package console_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/internal/console"
)

func TestSPAServesIndex(t *testing.T) {
	fsys, err := console.Dist()
	if err != nil {
		t.Fatal(err)
	}
	h := console.NewSPA(fsys, "")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatalf("content-type %q", rec.Header().Get("Content-Type"))
	}
}
