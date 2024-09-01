// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package serviceaccount

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-cmp/cmp"
)

func TestLoadKey(t *testing.T) {
	keyJSON := `{
        "type": "service_account",
        "project_id": "test-project",
        "private_key_id": "test-key-id",
        "private_key": "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
        "client_email": "test@test-project.iam.gserviceaccount.com",
        "client_id": "test-client-id",
        "auth_uri": "https://accounts.google.com/o/oauth2/auth",
        "token_uri": "https://oauth2.googleapis.com/token"
    }`

	key, err := LoadKey([]byte(keyJSON))
	if err != nil {
		t.Fatal(err)
	}

	wantKey := &Key{
		Type:         "service_account",
		ProjectID:    "test-project",
		PrivateKeyID: "test-key-id",
		PrivateKey:   "-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n",
		ClientEmail:  "test@test-project.iam.gserviceaccount.com",
		ClientID:     "test-client-id",
		AuthURI:      "https://accounts.google.com/o/oauth2/auth",
		TokenURI:     "https://oauth2.googleapis.com/token",
	}

	if diff := cmp.Diff(key, wantKey); diff != "" {
		t.Errorf("LoadKey() mismatch (-want +got):\n%s", diff)
	}
}

func TestAccessToken(t *testing.T) {
	// Generate a new RSA private key for testing.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// Encode the private key as PEM.
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Create a mock HTTP server to handle token requests.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the request method and path.
		if r.Method != http.MethodPost || r.URL.Path != "/" {
			t.Errorf("Unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Verify the request parameters.
		if err := r.ParseForm(); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		grantType := r.FormValue("grant_type")
		assertion := r.FormValue("assertion")
		if grantType != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Errorf("Unexpected grant_type: %s", grantType)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Validate the JWT assertion.
		token, err := jwt.Parse(assertion, func(token *jwt.Token) (any, error) {
			// In a real scenario, you would verify the token's signature
			// using the public key corresponding to the service account.
			return privateKey.Public(), nil // Use the correct public key for testing
		})
		if err != nil {
			t.Errorf("Invalid JWT assertion: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			t.Error("Invalid JWT claims")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if claims["iss"] != "test@test-project.iam.gserviceaccount.com" {
			t.Errorf("Unexpected 'iss' claim: %s", claims["iss"])
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Return a mock access token.
		response := struct {
			AccessToken string `json:"access_token"`
		}{
			AccessToken: "mock-access-token",
		}
		if err := json.NewEncoder(w).Encode(response); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
	}))
	defer ts.Close()

	// Create a service account key with the mock token URI and generated private key.
	key := &Key{
		ClientEmail: "test@test-project.iam.gserviceaccount.com",
		TokenURI:    ts.URL,
		PrivateKey:  string(privateKeyPEM), // Use the generated PEM key
	}

	// Obtain an access token using the mock server.
	token, err := key.AccessToken(context.Background(), ts.Client(), "scope1", "scope2")
	if err != nil {
		t.Fatal(err)
	}

	if token != "mock-access-token" {
		t.Errorf("Unexpected access token: got %q, want %q", token, "mock-access-token")
	}
}

func TestAccessToken_HTTPError(t *testing.T) {
	// Generate a new RSA private key for testing.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	// Encode the private key as PEM.
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	// Create a service account key with an invalid token URI.
	key := &Key{
		ClientEmail: "test@test-project.iam.gserviceaccount.com",
		TokenURI:    "http://invalid-url",
		PrivateKey:  string(privateKeyPEM), // Use the generated PEM key
	}

	// Try to obtain an access token, expecting an error.
	_, err = key.AccessToken(context.Background(), http.DefaultClient, "scope1", "scope2")
	if err == nil {
		t.Fatal("Expected an error, but got nil")
	}
}
