package ghapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// TokenSource mints and caches GitHub App installation access tokens.
type TokenSource struct {
	appID          int64
	installationID int64
	privateKey     *rsa.PrivateKey
	apiBase        string
	httpClient     *http.Client

	mu      sync.Mutex
	token   string
	expires time.Time
}

// NewTokenSource creates a TokenSource for a GitHub App installation.
func NewTokenSource(appID, installationID int64, privateKeyPEM []byte, apiBase string, httpClient *http.Client) (*TokenSource, error) {
	key, err := parsePrivateKey(privateKeyPEM)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &TokenSource{
		appID:          appID,
		installationID: installationID,
		privateKey:     key,
		apiBase:        strings.TrimRight(apiBase, "/"),
		httpClient:     httpClient,
	}, nil
}

// Token returns a valid installation access token.
func (ts *TokenSource) Token(ctx context.Context) (string, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	if ts.token != "" && time.Now().Before(ts.expires.Add(-60*time.Second)) {
		return ts.token, nil
	}

	jwt, err := ts.createJWT()
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/app/installations/%d/access_tokens", ts.apiBase, ts.installationID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+jwt)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := ts.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("ghapp: access token: status %d: %s", resp.StatusCode, string(body))
	}

	var parsed struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("ghapp: decode token: %w", err)
	}
	ts.token = parsed.Token
	ts.expires = parsed.ExpiresAt
	return ts.token, nil
}

func (ts *TokenSource) createJWT() (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]int64{
		"iat": now.Unix() - 60,
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": ts.appID,
	})
	if err != nil {
		return "", err
	}
	payloadEnc := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := header + "." + payloadEnc
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, ts.privateKey, crypto.SHA256, hash[:])
	if err != nil {
		return "", err
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parsePrivateKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("ghapp: invalid PEM")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		parsed, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("ghapp: parse key: %w", err)
		}
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("ghapp: key is not RSA")
		}
		return rsaKey, nil
	}
	return key, nil
}
