package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/muandane/gha-scheduler/internal/dispatch"
)

type noopDispatcher struct{}

func (noopDispatcher) Dispatch(_ context.Context, _ dispatch.Request) error { return nil }

func TestHandlerRejectsOversizedBody(t *testing.T) {
	h := New(Config{Secret: "s3cr3t"}, noopDispatcher{})
	body := bytes.Repeat([]byte("x"), maxWebhookBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", signBody("s3cr3t", body))
	req.Header.Set("X-GitHub-Event", "workflow_job")

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status: %d", rr.Code)
	}
}

func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
