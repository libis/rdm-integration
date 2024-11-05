// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package types

import (
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
