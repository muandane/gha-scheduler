package ghclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestGetWorkflowJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/org/repo/actions/jobs/42" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": 42,
			"run_id": 10,
			"status": "completed",
			"conclusion": "success",
			"completed_at": "2026-01-01T12:00:00Z"
		}`))
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL)
	job, err := client.GetWorkflowJob(context.Background(), "org", "repo", 42)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != 42 || job.Status != "completed" || job.Conclusion != "success" {
		t.Fatalf("job: %+v", job)
	}
	if job.CompletedAt.IsZero() {
		t.Fatal("expected completed_at")
	}
	if job.CompletedAt.UTC() != time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC) {
		t.Fatalf("completed_at: %v", job.CompletedAt)
	}
}
