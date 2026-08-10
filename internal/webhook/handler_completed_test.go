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

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ context.Context, _ dispatch.Request) error { return nil }

type recordingCleaner struct {
	mu      sync.Mutex
	jobIDs  []string
	reasons []string
}

func (r *recordingCleaner) CleanupByJobID(_ context.Context, jobID, reason string) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobIDs = append(r.jobIDs, jobID)
	r.reasons = append(r.reasons, reason)
	return 1, nil
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestHandlerCompletedTriggersCleanup(t *testing.T) {
	cleaner := &recordingCleaner{}
	h := webhook.New(webhook.Config{
		Secret:       "s3cr3t",
		Cleanup:      cleaner,
		CleanupGrace: 0,
	}, noopDispatcher{})

	body := []byte(`{
		"action":"completed",
		"workflow_job":{"id":9001,"run_id":8001,"conclusion":"cancelled"},
		"repository":{"full_name":"org/repo","owner":{"login":"org"},"name":"repo"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sign("s3cr3t", body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status: %d", rr.Code)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cleaner.mu.Lock()
		n := len(cleaner.jobIDs)
		cleaner.mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.Wait()

	cleaner.mu.Lock()
	defer cleaner.mu.Unlock()
	if len(cleaner.jobIDs) != 1 || cleaner.jobIDs[0] != "9001" {
		t.Fatalf("jobIDs: %v", cleaner.jobIDs)
	}
	if len(cleaner.reasons) != 1 || cleaner.reasons[0] != "webhook_completed" {
		t.Fatalf("reasons: %v", cleaner.reasons)
	}
}
