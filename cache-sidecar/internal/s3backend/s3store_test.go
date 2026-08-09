package s3backend_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

func TestS3StorePutGetList(t *testing.T) {
	store := make(map[string][]byte)
	bucket := "cache"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		bucketPrefix := bucket + "/"
		switch r.Method {
		case http.MethodPut:
			key := strings.TrimPrefix(path, bucketPrefix)
			body, _ := io.ReadAll(r.Body)
			store[key] = body
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				prefix := r.URL.Query().Get("prefix")
				w.Header().Set("Content-Type", "application/xml")
				var keys []string
				for k := range store {
					if strings.HasPrefix(k, prefix) {
						keys = append(keys, k)
					}
				}
				var contents string
				for _, k := range keys {
					contents += "<Contents><Key>" + k + "</Key></Contents>"
				}
				_, _ = w.Write([]byte("<ListBucketResult>" + contents + "</ListBucketResult>"))
				return
			}
			key := strings.TrimPrefix(path, bucketPrefix)
			body, ok := store[key]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(body)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()

	s, err := s3backend.NewS3Store(s3backend.S3Config{
		Endpoint: srv.URL,
		Bucket:   bucket,
		Client:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("NewS3Store: %v", err)
	}

	n, err := s.Put(t.Context(), "org/repo/v1/key-a", strings.NewReader("payload"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if n != 7 {
		t.Fatalf("size: %d", n)
	}

	rc, size, err := s.Get(t.Context(), "org/repo/v1/key-a")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "payload" {
		t.Fatalf("body: %q", body)
	}
	if size != 7 {
		t.Fatalf("content length: %d", size)
	}

	keys, err := s.List(t.Context(), "org/repo/v1/")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 || keys[0] != "org/repo/v1/key-a" {
		t.Fatalf("keys: %v", keys)
	}
}
