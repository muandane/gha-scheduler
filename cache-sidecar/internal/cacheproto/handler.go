package cacheproto

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

const twirpService = "github.actions.results.api.v1.CacheService"

// Handler serves actions/cache v1 REST and v2 Twirp endpoints.
type Handler struct {
	backend    *s3backend.Backend
	ownerRepo  string
	baseURL    string
	mu         sync.Mutex
	pending    map[string]pendingUpload
	nextUpload int
}

type pendingUpload struct {
	key     string
	version string
	size    int64
}

// NewHandler creates an HTTP handler for cache protocol endpoints.
func NewHandler(backend *s3backend.Backend, ownerRepo string) http.Handler {
	h := &Handler{
		backend:   backend,
		ownerRepo: ownerRepo,
		pending:   make(map[string]pendingUpload),
	}
	return http.HandlerFunc(h.serve)
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	switch {
	case strings.HasPrefix(r.URL.Path, "/twirp/"):
		h.handleTwirp(w, r)
	case strings.HasPrefix(r.URL.Path, "/_apis/artifactcache/blobs/"):
		h.handleBlobDownload(w, r)
	case strings.HasPrefix(r.URL.Path, "/_apis/artifactcache/"):
		h.handleV1(w, r)
	case strings.HasPrefix(r.URL.Path, "/_apis/cache-upload/"):
		h.handleUpload(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleTwirp(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/twirp/"), "/")
	if len(parts) != 2 || parts[0] != twirpService {
		http.NotFound(w, r)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	switch parts[1] {
	case "GetCacheEntryDownloadURL":
		h.twirpGetDownloadURL(w, r, body)
	case "CreateCacheEntry":
		h.twirpCreateEntry(w, r, body)
	case "FinalizeCacheEntryUpload":
		h.twirpFinalizeUpload(w, r, body)
	default:
		http.NotFound(w, r)
	}
}

type twirpGetReq struct {
	Key         string   `json:"key"`
	Version     string   `json:"version"`
	RestoreKeys []string `json:"restore_keys"`
}

func (h *Handler) twirpGetDownloadURL(w http.ResponseWriter, r *http.Request, body []byte) {
	var req twirpGetReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid request"})
		return
	}
	h.ensureBaseURL(r)
	matches, err := h.backend.FindKeys(r.Context(), req.Key, req.Version, req.RestoreKeys)
	if err != nil || len(matches) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "cache miss"})
		return
	}
	matched := matches[0]
	downloadURL := h.blobURL(matched, req.Version)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                  true,
		"signed_download_url": downloadURL,
		"matched_key":         matched,
	})
}

type twirpCreateReq struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

func (h *Handler) twirpCreateEntry(w http.ResponseWriter, r *http.Request, body []byte) {
	var req twirpCreateReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid request"})
		return
	}
	h.mu.Lock()
	h.nextUpload++
	id := fmt.Sprintf("%d", h.nextUpload)
	h.pending[id] = pendingUpload{key: req.Key, version: req.Version}
	h.mu.Unlock()

	uploadURL := h.uploadURL(r, id)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                true,
		"signed_upload_url": uploadURL,
	})
}

type twirpFinalizeReq struct {
	Key       string `json:"key"`
	Version   string `json:"version"`
	SizeBytes string `json:"size_bytes"`
}

func (h *Handler) twirpFinalizeUpload(w http.ResponseWriter, r *http.Request, body []byte) {
	var req twirpFinalizeReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid request"})
		return
	}
	wantSize, err := parseSizeBytes(req.SizeBytes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "message": "invalid size_bytes"})
		return
	}
	rc, gotSize, err := h.backend.Get(r.Context(), req.Key, req.Version)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "cache entry not found"})
		return
	}
	defer rc.Close()
	if wantSize > 0 && gotSize != wantSize {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "message": "size mismatch"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "entry_id": "1"})
}

func (h *Handler) handleV1(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.HasSuffix(r.URL.Path, "/cache") {
		http.NotFound(w, r)
		return
	}
	h.ensureBaseURL(r)
	keys := strings.Split(r.URL.Query().Get("keys"), ",")
	version := r.URL.Query().Get("version")
	if len(keys) == 0 || keys[0] == "" || version == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	restore := keys[1:]
	matches, err := h.backend.FindKeys(r.Context(), keys[0], version, restore)
	if err != nil || len(matches) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	matched := matches[0]
	var cacheSize int64
	if rc, size, err := h.backend.Get(r.Context(), matched, version); err == nil {
		cacheSize = size
		_ = rc.Close()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cacheKey":        matched,
		"cacheVersion":    version,
		"archiveLocation": h.blobURL(matched, version),
		"cacheSize":       cacheSize,
		"creationTime":    "1970-01-01T00:00:00Z",
	})
}

func (h *Handler) handleUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/_apis/cache-upload/")
	h.mu.Lock()
	p, ok := h.pending[id]
	h.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := h.backend.Put(r.Context(), p.key, p.version, r.Body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if r.ContentLength > 0 {
		h.mu.Lock()
		if pending, ok := h.pending[id]; ok {
			pending.size = r.ContentLength
			h.pending[id] = pending
		}
		h.mu.Unlock()
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) handleBlobDownload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/_apis/artifactcache/blobs/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	key, version := parts[0], parts[1]
	rc, _, err := h.backend.Get(r.Context(), key, version)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.Copy(w, rc)
}

func (h *Handler) blobURL(key, version string) string {
	base := strings.TrimRight(h.baseURL, "/")
	if base == "" {
		base = "http://127.0.0.1"
	}
	return fmt.Sprintf("%s/_apis/artifactcache/blobs/%s/%s", base, url.PathEscape(key), url.PathEscape(version))
}

func (h *Handler) uploadURL(r *http.Request, id string) string {
	h.ensureBaseURL(r)
	return fmt.Sprintf("%s/_apis/cache-upload/%s", h.baseURL, id)
}

func (h *Handler) ensureBaseURL(r *http.Request) {
	if h.baseURL != "" {
		return
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1"
	}
	h.baseURL = fmt.Sprintf("%s://%s", scheme, host)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func parseSizeBytes(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	var n int64
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid size")
		}
		n = n*10 + int64(c-'0')
	}
	return n, nil
}
