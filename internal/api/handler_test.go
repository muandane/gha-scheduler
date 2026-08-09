package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/muandane/gha-scheduler/internal/api"
	"github.com/muandane/gha-scheduler/internal/store"
	sqlitestore "github.com/muandane/gha-scheduler/internal/store/sqlite"
)

func TestAPIJobsAndStats(t *testing.T) {
	st := openAPIStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	_ = st.UpsertQueued(ctx, "org", "repo", "99", "42", now)
	_ = st.MarkDispatching(ctx, "42", store.RunnerSpecSnapshot{CPU: 2}, now)
	_ = st.MarkJobCreated(ctx, "42", now.Add(2*time.Second))

	h := api.NewHandler(st, "")
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/jobs/42", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get: %d", rec.Code)
	}
	var detail struct {
		Job struct {
			JobID string `json:"job_id"`
		} `json:"job"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Job.JobID != "42" {
		t.Fatalf("job %+v", detail)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/stats?since=24h", nil)
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stats: %d", rec.Code)
	}
}

func TestAPIAuth(t *testing.T) {
	st := openAPIStore(t)
	h := api.NewHandler(st, "secret")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs", nil)
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rec.Code)
	}
}

func openAPIStore(t *testing.T) *sqlitestore.Store {
	t.Helper()
	st, err := sqlitestore.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}
