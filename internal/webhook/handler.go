package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/muandane/gha-scheduler/internal/dispatch"
)

const maxWebhookBodyBytes = 1 << 20 // 1 MiB

// Dispatcher handles queued workflow jobs.
type Dispatcher interface {
	Dispatch(ctx context.Context, req dispatch.Request) error
}

// MetricsRecorder records webhook validity counters.
type MetricsRecorder interface {
	RecordWebhook(ctx context.Context, valid bool, reason string)
}

// Config configures the webhook handler.
type Config struct {
	Secret        string
	LabelDefaults dispatch.LabelDefaults
	OnQueued      func(context.Context, dispatch.Request)
	Metrics       MetricsRecorder
}

// Handler verifies GitHub webhooks and dispatches queued jobs asynchronously.
type Handler struct {
	cfg Config
	d   Dispatcher
	log *slog.Logger
	wg  sync.WaitGroup
}

// New creates a webhook Handler.
func New(cfg Config, d Dispatcher) *Handler {
	return &Handler{
		cfg: cfg,
		d:   d,
		log: slog.Default(),
	}
}

// Wait blocks until in-flight dispatches complete.
func (h *Handler) Wait() {
	h.wg.Wait()
}

type workflowJobEvent struct {
	Action string `json:"action"`
	Job    struct {
		ID     int64    `json:"id"`
		RunID  int64    `json:"run_id"`
		Labels []string `json:"labels"`
	} `json:"workflow_job"`
	Repository struct {
		FullName string `json:"full_name"`
		Owner    struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name string `json:"name"`
	} `json:"repository"`
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
	if err != nil {
		h.recordWebhook(r.Context(), false, "body_read")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if !verifySignature(h.cfg.Secret, body, sig) {
		h.recordWebhook(r.Context(), false, "signature")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	h.recordWebhook(r.Context(), true, "ok")

	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "workflow_job":
		h.handleWorkflowJob(body)
	default:
		h.log.Debug("ignored webhook event", "event", event)
	}

	w.WriteHeader(http.StatusOK)
}

func (h *Handler) recordWebhook(ctx context.Context, valid bool, reason string) {
	if h.cfg.Metrics != nil {
		h.cfg.Metrics.RecordWebhook(ctx, valid, reason)
	}
}

func (h *Handler) handleWorkflowJob(body []byte) {
	var evt workflowJobEvent
	if err := json.Unmarshal(body, &evt); err != nil {
		h.log.Error("decode workflow_job", "err", err)
		return
	}

	switch evt.Action {
	case "queued":
		owner := evt.Repository.Owner.Login
		repo := evt.Repository.Name
		if owner == "" || repo == "" {
			parts := strings.SplitN(evt.Repository.FullName, "/", 2)
			if len(parts) == 2 {
				owner, repo = parts[0], parts[1]
			}
		}
		req := dispatch.Request{
			Owner:         owner,
			Repo:          repo,
			RunID:         formatID(evt.Job.RunID),
			JobID:         formatID(evt.Job.ID),
			Labels:        evt.Job.Labels,
			LabelDefaults: h.cfg.LabelDefaults,
		}
		if h.cfg.OnQueued != nil {
			h.cfg.OnQueued(context.Background(), req)
		}
		h.wg.Go(func() {
			if err := h.d.Dispatch(context.Background(), req); err != nil {
				h.log.Error("dispatch failed", "err", err, "job_id", req.JobID, "labels", req.Labels)
			}
		})
	case "completed":
		h.log.Info("workflow_job completed", "job_id", evt.Job.ID)
	default:
		h.log.Debug("ignored workflow_job action", "action", evt.Action)
	}
}

func verifySignature(secret string, body []byte, header string) bool {
	if secret == "" || header == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	wantHex := strings.TrimPrefix(header, prefix)
	want, err := hex.DecodeString(wantHex)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
}

func formatID(id int64) string {
	return strconv.FormatInt(id, 10)
}
