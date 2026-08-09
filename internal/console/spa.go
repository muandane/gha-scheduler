package console

import (
	"io/fs"
	"net/http"
	"strings"
)

// SPA serves embedded static assets with index.html fallback.
type SPA struct {
	fs     fs.FS
	prefix string
}

// NewSPA creates a handler for embedded frontend assets.
func NewSPA(fsys fs.FS, prefix string) *SPA {
	return &SPA{fs: fsys, prefix: prefix}
}

// ServeHTTP implements http.Handler.
func (s *SPA) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}

	data, err := fs.ReadFile(s.fs, s.prefix+path)
	if err != nil {
		data, err = fs.ReadFile(s.fs, s.prefix+"index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
		return
	}

	if strings.HasSuffix(path, ".html") {
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	setContentType(w, path)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func setContentType(w http.ResponseWriter, path string) {
	switch {
	case strings.HasSuffix(path, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(path, ".js"):
		w.Header().Set("Content-Type", "application/javascript")
	case strings.HasSuffix(path, ".css"):
		w.Header().Set("Content-Type", "text/css")
	case strings.HasSuffix(path, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	default:
		w.Header().Set("Content-Type", "application/octet-stream")
	}
}
