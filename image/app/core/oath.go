// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type OauthTokenRequest struct {
	ClientId     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Code         string `json:"code,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	RedirectUri  string `json:"redirect_uri"`
	GrantType    string `json:"grant_type"`
	Resource     string `json:"resource,omitempty"`
}

type OauthTokenResponse struct {
	AccessToken           string `json:"access_token"`
	JwtToken              string `json:"id_token"`
	ExpiresIn             int    `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int    `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
	TokenType             string `json:"token_type"`
	Error                 string `json:"error"`
	Error_description     string `json:"error_description"`
	Error_uri             string `json:"error_uri"`
	ResourceServer        string `json:"resource_server"`
	State                 string `json:"state"`
	Issued                time.Time
	OtherTokens           []OauthTokenResponse `json:"other_tokens"`
}

type OauthTokenResponseStrings struct {
	AccessToken           string `json:"access_token"`
	JwtToken              string `json:"id_token"`
	ExpiresIn             string `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn string `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
	TokenType             string `json:"token_type"`
	Error                 string `json:"error"`
	Error_description     string `json:"error_description"`
	Error_uri             string `json:"error_uri"`
}

type TokenResponse struct {
	SessionId string `json:"session_id"`
}

type ExchangeRequest struct {
	DropPermissions bool   `json:"drop_permissions"`
	IdToken         string `json:"id_token"`
}

type ExchangeResponse struct {
	Message     string   `json:"message"`
	Expiration  string   `json:"expiration"`
	Permissions []string `json:"permissions"`
	Token       string   `json:"token"`
}

var PluginConfig = map[string]config.RepoPlugin{}
var RedirectUri string

func GetOauthToken(ctx context.Context, pluginId, code, refreshToken, sessionId string) (TokenResponse, error) {
	res := TokenResponse{sessionId}
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
	req := OauthTokenRequest{clientId, clientSecret, code, refreshToken, redirectUri, grantType, resource}
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
	result := OauthTokenResponse{}
	err = json.Unmarshal(b, &result)
	if err != nil {
		resultStrings := OauthTokenResponseStrings{}
		err = json.Unmarshal(b, &resultStrings)
		exp, _ := strconv.Atoi(resultStrings.ExpiresIn)
		exp2, _ := strconv.Atoi(resultStrings.RefreshTokenExpiresIn)
		result = OauthTokenResponse{
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
		result = OauthTokenResponse{
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
	for _, t := range result.OtherTokens {
		if t.ResourceServer == "transfer.api.globus.org" {
			result = t
			break
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
	return res.AccessToken
}

func getTokenFromCache(ctx context.Context, pluginId, sessionId string) (OauthTokenResponse, bool) {
	cached := config.GetRedis().Get(ctx, fmt.Sprintf("%v-%v", pluginId, sessionId))
	jsonString := cached.Val()
	if jsonString == "" {
		return OauthTokenResponse{}, false
	}
	res := OauthTokenResponse{}
	json.Unmarshal([]byte(jsonString), &res)
	return res, true
}

func encode(req OauthTokenRequest) *bytes.Buffer {
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

func doExchange(ctx context.Context, in OauthTokenResponse, url string) (OauthTokenResponse, error) {
	res := in
	req := ExchangeRequest{true, in.JwtToken}
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
	result := ExchangeResponse{}
	err = json.Unmarshal(b, &result)
	res.AccessToken = result.Token
	if result.Message != "" {
		return res, fmt.Errorf("exchanging token failed with message: %v", result.Message)
	}
	return res, err
}
