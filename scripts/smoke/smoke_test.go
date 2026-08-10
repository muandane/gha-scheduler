// Package smoke runs offline smoke checks (fake webhook + fake k8s/GH).
package smoke_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/cleanup"
	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
	"github.com/muandane/gha-scheduler/internal/webhook"
)

const testSecret = "smoke-webhook-secret"

type fakeGH struct {
	resp ghclient.JITConfigResponse
}

func (f *fakeGH) GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error) {
	return f.resp, nil
}

func (f *fakeGH) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	return nil
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// TestSmokeWebhookCreatesJobAndSecret posts a signed queued webhook and asserts k8s Job + Secret.
func TestSmokeWebhookCreatesJobAndSecret(t *testing.T) {
	gh := &fakeGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit-smoke", RunnerName: "ghs-smoke-1-2"}}
	k8s := fake.NewSimpleClientset()

	d := dispatch.New(dispatch.Config{
		Namespace:   "gha-runners",
		RunnerImage: "ghcr.io/actions/runner:latest",
		CacheImage:  "ghcr.io/org/cache:latest",
		CachePort:   8080,
		MemPerCPU:   "2Gi",
	}, k8s, gh)

	h := webhook.New(webhook.Config{
		Secret:        testSecret,
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}, d)

	body := []byte(`{
		"action":"queued",
		"workflow_job":{
			"id":9001,
			"run_id":8001,
			"labels":["runs-on=100","cpu=2","arch=x64"],
			"runner_name":""
		},
		"repository":{"full_name":"org/smoke-repo","owner":{"login":"org"},"name":"smoke-repo"}
	}`)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sign(testSecret, body))

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status: %d body=%s", rr.Code, rr.Body.String())
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		secrets, _ := k8s.CoreV1().Secrets("gha-runners").List(context.Background(), metav1.ListOptions{})
		jobs, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
		if len(secrets.Items) >= 1 && len(jobs.Items) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	secrets, err := k8s.CoreV1().Secrets("gha-runners").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	if len(secrets.Items) != 1 {
		t.Fatalf("secrets: got %d want 1", len(secrets.Items))
	}
	if string(secrets.Items[0].Data["config"]) != "jit-smoke" {
		t.Fatalf("secret jit: %q", secrets.Items[0].Data["config"])
	}

	jobs, err := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs: got %d want 1", len(jobs.Items))
	}
	job := jobs.Items[0]
	if job.Labels[k8sjob.LabelGHJob] != "9001" {
		t.Fatalf("job label gh_job: %v", job.Labels)
	}
	if *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoffLimit: %d", *job.Spec.BackoffLimit)
	}
}

// TestSmokeWebhookCompletedDeletesJob posts completed after queued and asserts Job is removed.
func TestSmokeWebhookCompletedDeletesJob(t *testing.T) {
	gh := &fakeGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit-smoke", RunnerName: "ghs-smoke-1-2"}}
	k8s := fake.NewSimpleClientset()
	cleaner := cleanup.NewJobCleaner(cleanup.Config{Namespace: "gha-runners"}, k8s, nil)

	d := dispatch.New(dispatch.Config{
		Namespace:   "gha-runners",
		RunnerImage: "ghcr.io/actions/runner:latest",
		CacheImage:  "ghcr.io/org/cache:latest",
		CachePort:   8080,
		MemPerCPU:   "2Gi",
	}, k8s, gh)

	h := webhook.New(webhook.Config{
		Secret:        testSecret,
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
		Cleanup:       cleaner,
		CleanupGrace:  0,
	}, d)

	queued := []byte(`{
		"action":"queued",
		"workflow_job":{"id":9001,"run_id":8001,"labels":["runs-on=100","cpu=2","arch=x64"]},
		"repository":{"full_name":"org/smoke-repo","owner":{"login":"org"},"name":"smoke-repo"}
	}`)
	postWebhook(t, h, queued)

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
		if len(jobs.Items) >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	completed := []byte(`{
		"action":"completed",
		"workflow_job":{"id":9001,"run_id":8001,"conclusion":"cancelled"},
		"repository":{"full_name":"org/smoke-repo","owner":{"login":"org"},"name":"smoke-repo"}
	}`)
	postWebhook(t, h, completed)
	h.Wait()

	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		jobs, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
		if len(jobs.Items) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	jobs, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
	t.Fatalf("expected job deleted, still have %d", len(jobs.Items))
}

func postWebhook(t *testing.T, h *webhook.Handler, body []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "workflow_job")
	req.Header.Set("X-Hub-Signature-256", sign(testSecret, body))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("webhook status: %d body=%s", rr.Code, rr.Body.String())
	}
}
