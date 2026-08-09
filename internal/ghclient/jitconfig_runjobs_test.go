package ghclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestListRunJobs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/actions/runs/99/jobs" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jobs":[{"id":200,"run_id":99,"status":"queued","labels":["runs-on=99"]}]}`))
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))
	jobs, err := client.ListRunJobs(context.Background(), "org", "repo", 99)
	if err != nil {
		t.Fatalf("ListRunJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != 200 {
		t.Fatalf("jobs: %+v", jobs)
	}
}
