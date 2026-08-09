package ghclient_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestListRunsPaginates(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("status") != "queued" {
			t.Fatalf("status: %s", r.URL.Query().Get("status"))
		}
		page++
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			var runs string
			for i := 0; i < 100; i++ {
				if i > 0 {
					runs += ","
				}
				runs += fmt.Sprintf(`{"id":%d,"status":"queued"}`, i+1)
			}
			_, _ = w.Write([]byte(`{"workflow_runs":[` + runs + `]}`))
		case "2":
			_, _ = w.Write([]byte(`{"workflow_runs":[{"id":101,"status":"queued"}]}`))
		default:
			_, _ = w.Write([]byte(`{"workflow_runs":[]}`))
		}
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))
	runs, err := client.ListRuns(context.Background(), "org", "repo", []string{"queued"})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 101 {
		t.Fatalf("runs: got %d want 101", len(runs))
	}
	if page < 2 {
		t.Fatalf("expected at least 2 pages, got %d", page)
	}
}
