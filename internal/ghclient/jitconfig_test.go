package ghclient_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestGenerateJITConfig(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"encoded_jit_config":"base64-blob","runner":{"id":1,"name":"ghs-1-2"}}`))
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))

	resp, err := client.GenerateJITConfig(context.Background(), "org", "repo", ghclient.JITConfigRequest{
		Name:   "ghs-123-456",
		Labels: []string{"self-hosted", "linux", "x64"},
	})
	if err != nil {
		t.Fatalf("GenerateJITConfig: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Fatalf("method: got %s want POST", gotMethod)
	}
	wantPath := "/repos/org/repo/actions/runners/generate-jit-config"
	if gotPath != wantPath {
		t.Fatalf("path: got %s want %s", gotPath, wantPath)
	}
	if gotBody["name"] != "ghs-123-456" {
		t.Fatalf("name: got %v", gotBody["name"])
	}
	labels, ok := gotBody["labels"].([]any)
	if !ok || len(labels) != 3 {
		t.Fatalf("labels: got %v", gotBody["labels"])
	}
	if resp.EncodedJITConfig != "base64-blob" {
		t.Fatalf("encoded_jit_config: got %q", resp.EncodedJITConfig)
	}
	if resp.RunnerName != "ghs-1-2" {
		t.Fatalf("runner name: got %q", resp.RunnerName)
	}
}

func TestGenerateJITConfigError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))
	_, err := client.GenerateJITConfig(context.Background(), "org", "repo", ghclient.JITConfigRequest{
		Name: "ghs-1-2",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("error should mention status: %v", err)
	}
}

func TestListQueuedRuns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method: %s", r.Method)
		}
		if r.URL.Path != "/repos/org/repo/actions/runs" {
			t.Fatalf("path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("status") != "queued" {
			t.Fatalf("status query: %s", r.URL.Query().Get("status"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"workflow_runs":[{"id":99,"status":"queued"}]}`))
	}))
	defer srv.Close()

	client := ghclient.New(srv.URL, ghclient.WithHTTPClient(srv.Client()))
	runs, err := client.ListQueuedRuns(context.Background(), "org", "repo")
	if err != nil {
		t.Fatalf("ListQueuedRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != 99 {
		t.Fatalf("runs: %+v", runs)
	}
}
