package cacheproto_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/cacheproto"
	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

func TestFinalizeFailsWithoutUpload(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	body := []byte(`{"key":"missing","version":"v1","size_bytes":"8"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/twirp/github.actions.results.api.v1.CacheService/FinalizeCacheEntryUpload", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != false {
		t.Fatalf("expected ok=false: %v", got)
	}
}

func TestHealthz(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}
