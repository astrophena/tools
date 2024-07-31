package serviceaccount

import (
	"context"
	"net/http"
	"os"
	"testing"
)

func TestAccessToken(t *testing.T) {
	key := os.Getenv("SERVICE_ACCOUNT_KEY")
	if key == "" {
		t.Skip("set SERVICE_ACCOUNT_KEY environment variable to run this test")
	}

	k, err := LoadKey([]byte(key))
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v", k)

	tok, err := k.AccessToken(context.Background(), http.DefaultClient, "https://www.googleapis.com/auth/spreadsheets")
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%s", tok)
}
