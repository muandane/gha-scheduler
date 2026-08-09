package s3backend

import (
	"context"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"sync"
)

// ObjectStore is the S3-compatible storage surface used by the cache backend.
type ObjectStore interface {
	Put(ctx context.Context, objectKey string, r io.Reader) (int64, error)
	Get(ctx context.Context, objectKey string) (io.ReadCloser, int64, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

// Backend stores cache archives scoped by owner/repo prefix.
type Backend struct {
	store  ObjectStore
	prefix string
}

// New creates a Backend with repo-scoped isolation.
func New(store ObjectStore, ownerRepo string) *Backend {
	return &Backend{store: store, prefix: ownerRepo}
}

func (b *Backend) objectKey(key, version string) string {
	return path.Join(b.prefix, version, key)
}

// Put stores a cache archive.
func (b *Backend) Put(ctx context.Context, key, version string, r io.Reader) error {
	_, err := b.store.Put(ctx, b.objectKey(key, version), r)
	return err
}

// Get retrieves a cache archive.
func (b *Backend) Get(ctx context.Context, key, version string) (io.ReadCloser, int64, error) {
	return b.store.Get(ctx, b.objectKey(key, version))
}

// FindKeys returns the first matching cache key using exact then prefix restore keys.
func (b *Backend) FindKeys(ctx context.Context, primaryKey, version string, restoreKeys []string) ([]string, error) {
	candidates := append([]string{primaryKey}, restoreKeys...)
	seen := make(map[string]struct{})
	var matches []string

	keys, err := b.store.List(ctx, path.Join(b.prefix, version)+"/")
	if err != nil {
		return nil, err
	}
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		short := strings.TrimPrefix(k, path.Join(b.prefix, version)+"/")
		keySet[short] = struct{}{}
	}

	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if _, ok := keySet[candidate]; ok {
			if _, done := seen[candidate]; !done {
				matches = append(matches, candidate)
				seen[candidate] = struct{}{}
			}
			continue
		}
		for short := range keySet {
			if strings.HasPrefix(short, candidate) {
				if _, done := seen[short]; !done {
					matches = append(matches, short)
					seen[short] = struct{}{}
				}
			}
		}
	}
	sort.Strings(matches)
	return matches, nil
}

// Memory is an in-memory ObjectStore for tests.
type Memory struct {
	mu   sync.RWMutex
	data map[string][]byte
}

// NewMemory creates an empty in-memory store.
func NewMemory() *Memory {
	return &Memory{data: make(map[string][]byte)}
}

// Put stores bytes under objectKey.
func (m *Memory) Put(ctx context.Context, objectKey string, r io.Reader) (int64, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[objectKey] = body
	return int64(len(body)), nil
}

// Get reads bytes for objectKey.
func (m *Memory) Get(ctx context.Context, objectKey string) (io.ReadCloser, int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	body, ok := m.data[objectKey]
	if !ok {
		return nil, 0, fmt.Errorf("s3backend: object not found: %s", objectKey)
	}
	cp := append([]byte(nil), body...)
	return io.NopCloser(strings.NewReader(string(cp))), int64(len(cp)), nil
}

// List returns object keys with the given prefix.
func (m *Memory) List(ctx context.Context, prefix string) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var keys []string
	for k := range m.data {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}
