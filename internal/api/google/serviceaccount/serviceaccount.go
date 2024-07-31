// Package serviceaccount provides functions for working with Google service accounts.
//
// See https://developers.google.com/identity/protocols/oauth2/service-account.
package serviceaccount

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// LoadKey loads service account key from JSON byte slice.
func LoadKey(b []byte) (*Key, error) {
	var key Key
	if err := json.Unmarshal(b, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// Key represents a service account key.
type Key struct {
	Type         string `json:"type"`
	ProjectID    string `json:"project_id"`
	PrivateKeyID string `json:"private_key_id"`
	PrivateKey   string `json:"private_key"`
	ClientEmail  string `json:"client_email"`
	ClientID     string `json:"client_id"`
	AuthURI      string `json:"auth_uri"`
	TokenURI     string `json:"token_uri"`
}

// AccessToken obtains an access token for service account identified by this key that is valid for one hour.
func (k *Key) AccessToken(client *http.Client, scopes ...string) (string, error) {
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(k.PrivateKey))
	if err != nil {
		return "", err
	}

	now := time.Now()
	sig, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"iss":   k.ClientEmail,
		"sub":   k.ClientEmail,
		"aud":   k.TokenURI,
		"scope": strings.Join(scopes, " "),
		"iat":   now.Unix(),
		"exp":   now.Add(time.Hour).Unix(),
	}).SignedString(key)
	if err != nil {
		return "", err
	}

	params := &url.Values{}
	params.Add("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	params.Add("assertion", sig)

	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Post(k.TokenURI, "application/x-www-form-urlencoded", strings.NewReader(params.Encode()))
	if err != nil {
		return "", err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("got status code %d instead of 200: %s", resp.StatusCode, body)
	}

	var tokenResponse struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return "", err
	}

	return tokenResponse.AccessToken, nil
}
