package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/muandane/gha-scheduler/internal/store"
)

// Handler serves console JSON API.
type Handler struct {
	Store store.JobStore
	Token string
}

// NewHandler creates an API handler.
func NewHandler(st store.JobStore, token string) *Handler {
	return &Handler{Store: st, Token: token}
}

// ServeHTTP routes /api/v1/* requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.Token != "" && !h.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	switch {
	case path == "/health" && r.Method == http.MethodGet:
		h.writeJSON(w, map[string]string{"store": "ok"})
	case path == "/jobs" && r.Method == http.MethodGet:
		h.listJobs(w, r)
	case strings.HasPrefix(path, "/jobs/") && r.Method == http.MethodGet:
		h.getJob(w, r, strings.TrimPrefix(path, "/jobs/"))
	case path == "/stats" && r.Method == http.MethodGet:
		h.stats(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) authorized(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(auth, prefix) {
		return false
	}
	return strings.TrimPrefix(auth, prefix) == h.Token
}

func (h *Handler) listJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	res, err := h.Store.ListJobs(r.Context(), store.ListQuery{
		Repo:   q.Get("repo"),
		Status: store.Status(q.Get("status")),
		Limit:  limit,
		Cursor: q.Get("cursor"),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, map[string]any{
		"jobs":        jobResponses(res.Jobs),
		"next_cursor": res.NextCursor,
	})
}

func (h *Handler) getJob(w http.ResponseWriter, r *http.Request, jobID string) {
	job, err := h.Store.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if job == nil {
		http.NotFound(w, r)
		return
	}
	h.writeJSON(w, map[string]any{
		"job":      jobResponse(*job),
		"timeline": timelineFor(*job),
	})
}

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	since := time.Now().Add(-24 * time.Hour)
	if raw := r.URL.Query().Get("since"); raw != "" {
		if d, err := time.ParseDuration(raw); err == nil {
			since = time.Now().Add(-d)
		}
	}
	stats, err := h.Store.Stats(r.Context(), since)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.writeJSON(w, stats)
}

func (h *Handler) writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

type jobJSON struct {
	JobID              string  `json:"job_id"`
	RunID              string  `json:"run_id"`
	Owner              string  `json:"owner"`
	Repo               string  `json:"repo"`
	Status             string  `json:"status"`
	WebhookAt          string  `json:"webhook_at,omitempty"`
	DispatchAt         string  `json:"dispatch_at,omitempty"`
	JobCreatedAt       string  `json:"job_created_at,omitempty"`
	ScheduledAt        string  `json:"scheduled_at,omitempty"`
	RunningAt          string  `json:"running_at,omitempty"`
	CompletedAt        string  `json:"completed_at,omitempty"`
	DispatchLatencySec float64 `json:"dispatch_latency_sec,omitempty"`
	ScheduleLatencySec float64 `json:"schedule_latency_sec,omitempty"`
	JobDurationSec     float64 `json:"job_duration_sec,omitempty"`
	CPU                int     `json:"cpu,omitempty"`
	Arch               string  `json:"arch,omitempty"`
	Pool               string  `json:"pool,omitempty"`
	CacheEnabled       bool    `json:"cache_enabled"`
	ExitCode           *int    `json:"exit_code,omitempty"`
	PodName            string  `json:"pod_name,omitempty"`
	DispatchError      string  `json:"dispatch_error,omitempty"`
	GitHubURL          string  `json:"github_url"`
	UpdatedAt          string  `json:"updated_at"`
}

func jobResponses(jobs []store.Job) []jobJSON {
	out := make([]jobJSON, len(jobs))
	for i, j := range jobs {
		out[i] = jobResponse(j)
	}
	return out
}

func jobResponse(j store.Job) jobJSON {
	return jobJSON{
		JobID:              j.JobID,
		RunID:              j.RunID,
		Owner:              j.Owner,
		Repo:               j.Repo,
		Status:             string(j.Status),
		WebhookAt:          formatAPI(j.WebhookAt),
		DispatchAt:         formatAPI(j.DispatchAt),
		JobCreatedAt:       formatAPI(j.JobCreatedAt),
		ScheduledAt:        formatAPI(j.ScheduledAt),
		RunningAt:          formatAPI(j.RunningAt),
		CompletedAt:        formatAPI(j.CompletedAt),
		DispatchLatencySec: j.DispatchLatencySec,
		ScheduleLatencySec: j.ScheduleLatencySec,
		JobDurationSec:     j.JobDurationSec,
		CPU:                j.CPU,
		Arch:               j.Arch,
		Pool:               j.Pool,
		CacheEnabled:       j.CacheEnabled,
		ExitCode:           j.ExitCode,
		PodName:            j.PodName,
		DispatchError:      j.DispatchError,
		GitHubURL:          githubURL(j),
		UpdatedAt:          formatAPI(j.UpdatedAt),
	}
}

func timelineFor(j store.Job) []store.TimelinePhase {
	phases := []struct {
		name string
		at   time.Time
	}{
		{"webhook_received", j.WebhookAt},
		{"dispatch", j.DispatchAt},
		{"job_created", j.JobCreatedAt},
		{"pod_scheduled", j.ScheduledAt},
		{"pod_running", j.RunningAt},
		{"pod_completed", j.CompletedAt},
	}
	var out []store.TimelinePhase
	for _, p := range phases {
		if p.at.IsZero() {
			continue
		}
		out = append(out, store.TimelinePhase{Name: p.name, At: p.at})
	}
	return out
}

func githubURL(j store.Job) string {
	repo := j.Repo
	if !strings.Contains(repo, "/") {
		repo = j.Owner + "/" + repo
	}
	return "https://github.com/" + repo + "/actions/runs/" + j.RunID
}

func formatAPI(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
