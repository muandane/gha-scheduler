package s3backend_test

import (
	"io"
	"strings"
	"testing"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

func TestMemoryPutGetList(t *testing.T) {
	store := s3backend.NewMemory()
	backend := s3backend.New(store, "org/repo")

	if err := backend.Put(t.Context(), "key-a", "v1", strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}

	rc, size, err := backend.Get(t.Context(), "key-a", "v1")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	body, _ := io.ReadAll(rc)
	if string(body) != "data" || size != 4 {
		t.Fatalf("get: %q size=%d", body, size)
	}

	keys, err := backend.FindKeys(t.Context(), "key", "v1", []string{"key-a", "key-prefix"})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != "key-a" {
		t.Fatalf("find: %v", keys)
	}
}

func TestPrefixIsolation(t *testing.T) {
	store := s3backend.NewMemory()
	a := s3backend.New(store, "org/a")
	b := s3backend.New(store, "org/b")

	_ = a.Put(t.Context(), "shared", "v1", strings.NewReader("a"))
	_, _, err := b.Get(t.Context(), "shared", "v1")
	if err == nil {
		t.Fatal("expected isolation between repo prefixes")
	}
}
