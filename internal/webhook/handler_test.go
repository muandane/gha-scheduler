package webhook_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/webhook"
)

const testSecret = "test-webhook-secret"

type recordingDispatcher struct {
	mu    sync.Mutex
	calls []dispatch.Request
}

func (r *recordingDispatcher) Dispatch(ctx context.Context, req dispatch.Request) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, req)
	return nil
}

func (r *recordingDispatcher) last() dispatch.Request {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls[len(r.calls)-1]
}

func (r *recordingDispatcher) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestWebhookQueuedJobDispatchesAsync(t *testing.T) {
	rec := &recordingDispatcher{}
	h := webhook.New(webhook.Config{
		Secret: testSecret,
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}, rec)

	body := []byte(`{
		"action":"queued",
		"workflow_job":{
			"id":42,
			"run_id":100,
			"labels":["runs-on=100","cpu=4"],
			"runner_name":""
		},
		"repository":{"full_name":"org/repo","owner":{"login":"org"},"name":"repo"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sign(testSecret, body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d body=%s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(2 * time.Second)
	for rec.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if rec.count() != 1 {
		t.Fatalf("dispatch calls: %d", rec.count())
	}
	got := rec.last()
	if got.JobID != "42" || got.RunID != "100" || got.Owner != "org" || got.Repo != "repo" {
		t.Fatalf("dispatch req: %+v", got)
	}
}

func TestWebhookInvalidSignatureRejected(t *testing.T) {
	rec := &recordingDispatcher{}
	h := webhook.New(webhook.Config{Secret: testSecret}, rec)

	body := []byte(`{"action":"queued"}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status: %d", rr.Code)
	}
	if rec.count() != 0 {
		t.Fatal("dispatcher should not be called")
	}
}

func TestWebhookCompletedIsNoOp(t *testing.T) {
	rec := &recordingDispatcher{}
	h := webhook.New(webhook.Config{Secret: testSecret}, rec)

	body := []byte(`{"action":"completed","workflow_job":{"id":1}}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sign(testSecret, body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
	time.Sleep(50 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatal("completed should not dispatch")
	}
}

func TestWebhookUnknownEventIgnored(t *testing.T) {
	rec := &recordingDispatcher{}
	h := webhook.New(webhook.Config{Secret: testSecret}, rec)

	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "push")
	req.Header.Set("X-Hub-Signature-256", sign(testSecret, body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}
}
