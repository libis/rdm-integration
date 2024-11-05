// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/core/types"
	"integration/app/logging"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var PluginConfig = map[string]config.RepoPlugin{}
var RedirectUri string

func GetOauthToken(ctx context.Context, pluginId, code, refreshToken, sessionId string) (types.TokenResponse, error) {
	res := types.TokenResponse{SessionId: sessionId}
	clientId := PluginConfig[pluginId].TokenGetter.OauthClientId
	redirectUri := RedirectUri
	clientSecret, resource, postUrl, exchange, err := config.ClientSecret(clientId)
	if err != nil {
		return res, err
	}
	grantType := "authorization_code"
	if code == "" && refreshToken != "" {
		grantType = "refresh_token"
	}
	req := types.OauthTokenRequest{ClientId: clientId, ClientSecret: clientSecret, Code: code, RefreshToken: refreshToken, RedirectUri: redirectUri, GrantType: grantType, Resource: resource}
	request, _ := http.NewRequestWithContext(ctx, "POST", postUrl, encode(req))
	request.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Add("Accept", "application/json")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return res, fmt.Errorf("getting API token failed: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return res, fmt.Errorf("getting API token failed: %d - %s", r.StatusCode, string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return res, fmt.Errorf("getting token response failed: %v", err)
	}
	result := types.OauthTokenResponse{}
	err = json.Unmarshal(b, &result)
	if err != nil {
		resultStrings := types.OauthTokenResponseStrings{}
		err = json.Unmarshal(b, &resultStrings)
		exp, _ := strconv.Atoi(resultStrings.ExpiresIn)
		exp2, _ := strconv.Atoi(resultStrings.RefreshTokenExpiresIn)
		result = types.OauthTokenResponse{
			AccessToken:           resultStrings.AccessToken,
			ExpiresIn:             exp,
			RefreshToken:          resultStrings.RefreshToken,
			RefreshTokenExpiresIn: exp2,
			Scope:                 resultStrings.Scope,
			TokenType:             resultStrings.TokenType,
		}
	}
	if err != nil {
		str := string(b)
		if str == "" {
			return res, fmt.Errorf("getting API token failed: response is empty")
		}
		params, err := url.ParseQuery(str)
		if err != nil {
			return res, fmt.Errorf("getting API token failed: %v", err)
		}
		exp, _ := strconv.Atoi(params.Get("expires_in"))
		exp2, _ := strconv.Atoi(params.Get("refresh_token_expires_in"))
		result = types.OauthTokenResponse{
			AccessToken:           params.Get("access_token"),
			ExpiresIn:             exp,
			RefreshToken:          params.Get("refresh_token"),
			RefreshTokenExpiresIn: exp2,
			Scope:                 params.Get("scope"),
			TokenType:             params.Get("token_type"),
		}
	}
	if exchange != "" {
		result, err = doExchange(ctx, result, exchange)
		if err != nil {
			return res, err
		}
	}
	result.Issued = time.Now()
	tokenBytes, err := json.Marshal(result)
	if err != nil {
		return res, err
	}
	config.GetRedis().Set(ctx, fmt.Sprintf("%v-%v", pluginId, sessionId), string(tokenBytes), config.LockMaxDuration)
	return res, nil
}

func GetTokenFromCache(ctx context.Context, token, sessionId, pluginId string) string {
	res, ok := getTokenFromCache(ctx, pluginId, sessionId)
	if !ok {
		return token
	}
	expired := time.Now().After(res.Issued.Add(time.Duration((res.ExpiresIn - 5*60)) * time.Second))
	ok = true
	if expired {
		_, err := GetOauthToken(ctx, pluginId, "", res.RefreshToken, sessionId)
		if err != nil {
			logging.Logger.Println("token refresh failed:", err)
			return res.AccessToken
		}
		res, ok = getTokenFromCache(ctx, pluginId, sessionId)
		if !ok {
			logging.Logger.Println("token not in cache after refresh for plugin id:", pluginId)
			return token
		}
	}
	for _, t := range res.OtherTokens {
		if t.ResourceServer == "transfer.api.globus.org" {
			res = t
			break
		}
	}
	return res.AccessToken
}

func getTokenFromCache(ctx context.Context, pluginId, sessionId string) (types.OauthTokenResponse, bool) {
	cached := config.GetRedis().Get(ctx, fmt.Sprintf("%v-%v", pluginId, sessionId))
	jsonString := cached.Val()
	if jsonString == "" {
		return types.OauthTokenResponse{}, false
	}
	res := types.OauthTokenResponse{}
	json.Unmarshal([]byte(jsonString), &res)
	return res, true
}

func encode(req types.OauthTokenRequest) *bytes.Buffer {
	codeOrRefreshToken := req.Code
	codeOrRefreshTokenName := "code"
	if req.Code == "" && req.RefreshToken != "" {
		codeOrRefreshToken = req.RefreshToken
		codeOrRefreshTokenName = "refresh_token"
	}
	s := fmt.Sprintf("%s=%s&client_id=%s&client_secret=%s&redirect_uri=%s&grant_type=%s",
		codeOrRefreshTokenName,
		url.QueryEscape(codeOrRefreshToken),
		url.QueryEscape(req.ClientId),
		url.QueryEscape(req.ClientSecret),
		url.QueryEscape(req.RedirectUri),
		url.QueryEscape(req.GrantType),
	)
	if req.Resource != "" {
		s = s + "&resource=" + url.QueryEscape(req.Resource)
	}
	return bytes.NewBuffer([]byte(s))
}

func doExchange(ctx context.Context, in types.OauthTokenResponse, url string) (types.OauthTokenResponse, error) {
	res := in
	req := types.ExchangeRequest{DropPermissions: true, IdToken: in.JwtToken}
	data, _ := json.Marshal(req)
	body := bytes.NewBuffer(data)
	request, _ := http.NewRequestWithContext(ctx, "POST", url, body)
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Accept", "application/json")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return res, fmt.Errorf("exchanging API token failed: %v", err)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		b, _ := io.ReadAll(r.Body)
		return res, fmt.Errorf("exchanging API token failed: %d - %s", r.StatusCode, string(b))
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return res, fmt.Errorf("exchanging token response failed: %v", err)
	}
	result := types.ExchangeResponse{}
	err = json.Unmarshal(b, &result)
	res.AccessToken = result.Token
	if result.Message != "" {
		return res, fmt.Errorf("exchanging token failed with message: %v", result.Message)
	}
	return res, err
}
