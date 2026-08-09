package ghclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestDeleteRunner(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))
	if err := client.DeleteRunner(context.Background(), "org", "repo", 42); err != nil {
		t.Fatalf("DeleteRunner: %v", err)
	}
	if gotMethod != http.MethodDelete {
		t.Fatalf("method: %s", gotMethod)
	}
	if gotPath != "/repos/org/repo/actions/runners/42" {
		t.Fatalf("path: %s", gotPath)
	}
}
