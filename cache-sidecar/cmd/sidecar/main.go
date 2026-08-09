package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/muandane/gha-scheduler/cache-sidecar/internal/cacheproto"
	"github.com/muandane/gha-scheduler/cache-sidecar/internal/s3backend"
)

func main() {
	port := envInt("CACHE_PORT", 8080)
	prefix := os.Getenv("CACHE_PREFIX")
	if prefix == "" {
		log.Fatal("CACHE_PREFIX is required (owner/repo)")
	}

	store, err := newObjectStore()
	if err != nil {
		log.Fatal(err)
	}

	backend := s3backend.New(store, prefix)
	handler := cacheproto.NewHandler(backend, prefix)

	addr := ":" + strconv.Itoa(port)
	log.Printf("cache sidecar listening on %s prefix=%s backend=%s", addr, prefix, env("CACHE_BACKEND", "memory"))
	log.Fatal(http.ListenAndServe(addr, handler))
}

func newObjectStore() (s3backend.ObjectStore, error) {
	backend := env("CACHE_BACKEND", "memory")
	switch backend {
	case "memory":
		return s3backend.NewMemory(), nil
	case "s3":
		return s3backend.NewS3Store(s3backend.S3Config{
			Endpoint:  os.Getenv("S3_ENDPOINT"),
			Bucket:    os.Getenv("S3_BUCKET"),
			Region:    env("S3_REGION", "us-east-1"),
			AccessKey: os.Getenv("S3_ACCESS_KEY"),
			SecretKey: os.Getenv("S3_SECRET_KEY"),
		})
	default:
		return nil, fmt.Errorf("unknown CACHE_BACKEND %q (use memory or s3)", backend)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
