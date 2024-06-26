package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.astrophena.name/tools/internal/request"
)

func TestConns(t *testing.T) {
	mux := http.NewServeMux()
	s := httptest.NewUnstartedServer(mux)
	mux.Handle("/", Conns(t.Logf, s.Config))

	s.Start()
	defer s.Close()

	_, err := http.Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
}

func TestConns_JSON(t *testing.T) {
	mux := http.NewServeMux()
	s := httptest.NewUnstartedServer(mux)
	mux.Handle("/", Conns(t.Logf, s.Config))

	s.Start()
	defer s.Close()

	conns, err := request.MakeJSON[ConnMap](context.Background(), request.Params{
		Method: http.MethodGet,
		URL:    s.URL + "?format=json",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(conns) != 1 {
		t.Fatalf("conns should have only one connection, got %d", len(conns))
	}

	var conn *Conn
	// Grab a sole connection from the map.
	for _, c := range conns {
		conn = c
		break
	}

	if conn.Network != "tcp" {
		t.Errorf("connection's network must be tcp, got %s", conn.Network)
	}
	if !strings.HasPrefix(conn.Addr, "127.0.0.1") {
		t.Errorf("connection's address must begin from 127.0.0.1, got %s", conn.Addr)
	}
}
