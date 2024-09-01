// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package serviceaccount provides functions for working with Google service accounts.
//
// See https://developers.google.com/identity/protocols/oauth2/service-account.
package serviceaccount

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"go.astrophena.name/base/request"
	"go.astrophena.name/tools/internal/version"

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
func (k *Key) AccessToken(ctx context.Context, client *http.Client, scopes ...string) (string, error) {
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

	params := url.Values{}
	params.Add("grant_type", "urn:ietf:params:oauth:grant-type:jwt-bearer")
	params.Add("assertion", sig)

	type response struct {
		AccessToken string `json:"access_token"`
	}

	tok, err := request.Make[response](ctx, request.Params{
		Method: http.MethodPost,
		URL:    k.TokenURI,
		Body:   params,
		Headers: map[string]string{
			"User-Agent": version.UserAgent(),
		},
		HTTPClient: client,
	})
	if err != nil {
		return "", err
	}

	return tok.AccessToken, nil
}
