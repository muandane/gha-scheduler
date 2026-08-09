package cacheproto_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/cacheproto"
	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

func TestV1GetCacheHit(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	_ = backend.Put(t.Context(), "my-key", "ver1", strings.NewReader("archive-bytes"))

	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	url := srv.URL + "/_apis/artifactcache/cache?keys=my-key&version=ver1"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer token")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status %d: %s", resp.StatusCode, body)
	}

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["cacheKey"] != "my-key" {
		t.Fatalf("cacheKey: %v", got["cacheKey"])
	}
	loc, _ := got["archiveLocation"].(string)
	if !strings.Contains(loc, "/_apis/artifactcache/blobs/") {
		t.Fatalf("archiveLocation: %q", loc)
	}

	blobReq, _ := http.NewRequest(http.MethodGet, loc, nil)
	blobResp, err := http.DefaultClient.Do(blobReq)
	if err != nil {
		t.Fatal(err)
	}
	defer blobResp.Body.Close()
	if blobResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(blobResp.Body)
		t.Fatalf("blob status %d: %s", blobResp.StatusCode, body)
	}
	blobBody, err := io.ReadAll(blobResp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(blobBody) != "archive-bytes" {
		t.Fatalf("blob body: %q", blobBody)
	}
}

func TestV1GetCacheMiss(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	url := srv.URL + "/_apis/artifactcache/cache?keys=missing&version=ver1"
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status: %d", resp.StatusCode)
	}
}

func TestV2GetCacheEntryDownloadURLHit(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	_ = backend.Put(t.Context(), "deps-cache", "ver2", strings.NewReader("blob"))

	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	body := []byte(`{"key":"deps-cache","version":"ver2","restore_keys":["deps-"]}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/twirp/github.actions.results.api.v1.CacheService/GetCacheEntryDownloadURL", bytes.NewReader(body))
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
	if got["ok"] != true {
		t.Fatalf("ok: %v msg=%v", got["ok"], got["message"])
	}
	if got["matched_key"] != "deps-cache" {
		t.Fatalf("matched_key: %v", got["matched_key"])
	}
}

func TestV2CreateAndFinalizeCacheEntry(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")
	srv := httptest.NewServer(cacheproto.NewHandler(backend, "org/repo"))
	defer srv.Close()

	createBody := []byte(`{"key":"build-cache","version":"v1"}`)
	createReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/twirp/github.actions.results.api.v1.CacheService/CreateCacheEntry", bytes.NewReader(createBody))
	createReq.Header.Set("Content-Type", "application/json")
	createResp, err := http.DefaultClient.Do(createReq)
	if err != nil {
		t.Fatal(err)
	}
	defer createResp.Body.Close()

	var create map[string]any
	if err := json.NewDecoder(createResp.Body).Decode(&create); err != nil {
		t.Fatal(err)
	}
	if create["ok"] != true {
		t.Fatalf("create ok: %v", create)
	}
	uploadURL, _ := create["signed_upload_url"].(string)
	if uploadURL == "" {
		t.Fatal("missing signed_upload_url")
	}

	uploadReq, _ := http.NewRequest(http.MethodPut, uploadURL, strings.NewReader("tar-data"))
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("upload status: %d", uploadResp.StatusCode)
	}

	finalizeBody := []byte(`{"key":"build-cache","version":"v1","size_bytes":"8"}`)
	finalizeReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/twirp/github.actions.results.api.v1.CacheService/FinalizeCacheEntryUpload", bytes.NewReader(finalizeBody))
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeResp, err := http.DefaultClient.Do(finalizeReq)
	if err != nil {
		t.Fatal(err)
	}
	defer finalizeResp.Body.Close()

	var finalize map[string]any
	if err := json.NewDecoder(finalizeResp.Body).Decode(&finalize); err != nil {
		t.Fatal(err)
	}
	if finalize["ok"] != true {
		t.Fatalf("finalize: %v", finalize)
	}

	// Verify stored via v1 lookup.
	getURL := srv.URL + "/_apis/artifactcache/cache?keys=build-cache&version=v1"
	getReq, _ := http.NewRequest(http.MethodGet, getURL, nil)
	getResp, err := http.DefaultClient.Do(getReq)
	if err != nil {
		t.Fatal(err)
	}
	defer getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get after finalize: %d", getResp.StatusCode)
	}
}
