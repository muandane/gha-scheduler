package ghapp_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/muandane/gha-scheduler/internal/ghapp"
)

func TestTokenSource(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/app/installations/42/access_tokens" {
			t.Errorf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":      "ghs_install_token",
			"expires_at": time.Now().Add(1 * time.Hour).Format(time.RFC3339),
		})
	}))
	defer srv.Close()

	ts, err := ghapp.NewTokenSource(123, 42, pemBytes, srv.URL, srv.Client())
	if err != nil {
		t.Fatalf("NewTokenSource: %v", err)
	}

	token, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if token != "ghs_install_token" {
		t.Fatalf("token: %q", token)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Fatalf("auth header: %q", gotAuth)
	}

	// Cached token path
	token2, err := ts.Token(context.Background())
	if err != nil {
		t.Fatalf("Token cached: %v", err)
	}
	if token2 != token {
		t.Fatalf("cached token mismatch")
	}
}
