package dispatch_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/muandane/gha-scheduler/internal/dispatch"
	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

type fakeGH struct {
	calls int
	err   error
	resp  ghclient.JITConfigResponse
}

func (f *fakeGH) GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error) {
	f.calls++
	if f.err != nil {
		return ghclient.JITConfigResponse{}, f.err
	}
	return f.resp, nil
}

func (f *fakeGH) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	return nil
}

func TestDispatchCreatesSecretAndJob(t *testing.T) {
	gh := &fakeGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit-blob", RunnerName: "ghs-1-2"}}
	k8s := fake.NewSimpleClientset()

	d := dispatch.New(dispatch.Config{
		Namespace:   "gha-runners",
		RunnerImage: "ghcr.io/actions/runner:latest",
		CacheImage:  "ghcr.io/org/cache:latest",
		CachePort:   8080,
		MemPerCPU:   "2Gi",
	}, k8s, gh)

	req := dispatch.Request{
		Owner:         "org",
		Repo:          "repo",
		RunID:         "100",
		JobID:         "200",
		Labels:        []string{"runs-on=100", "cpu=2", "arch=x64"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}

	if err := d.Dispatch(context.Background(), req); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if gh.calls != 1 {
		t.Fatalf("gh calls: got %d want 1", gh.calls)
	}

	secrets, err := k8s.CoreV1().Secrets("gha-runners").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list secrets: %v", err)
	}
	if len(secrets.Items) != 1 {
		t.Fatalf("secrets: got %d want 1", len(secrets.Items))
	}
	if len(secrets.Items[0].OwnerReferences) == 0 {
		t.Fatal("expected owner reference on secret")
	}
	if string(secrets.Items[0].Data["config"]) != "jit-blob" {
		t.Fatalf("secret data: %q", secrets.Items[0].Data["config"])
	}

	jobs, err := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("list jobs: %v", err)
	}
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs: got %d want 1", len(jobs.Items))
	}
	job := jobs.Items[0]
	if *job.Spec.BackoffLimit != 0 {
		t.Fatalf("backoffLimit: %d", *job.Spec.BackoffLimit)
	}
	if job.Labels[k8sjob.LabelGHJob] != "200" {
		t.Fatalf("job label: %v", job.Labels)
	}
	podLabels := job.Spec.Template.Labels
	if podLabels[k8sjob.LabelGHJob] != "200" {
		t.Fatalf("pod template labels: %v", podLabels)
	}
}

func TestDispatchDedupSkipsExistingJob(t *testing.T) {
	gh := &fakeGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit", RunnerName: "ghs-1-2"}}
	existing := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing",
			Namespace: "gha-runners",
			Labels: map[string]string{
				k8sjob.LabelGHJob: "200",
			},
		},
	}
	k8s := fake.NewSimpleClientset(existing)

	d := dispatch.New(dispatch.Config{Namespace: "gha-runners"}, k8s, gh)
	err := d.Dispatch(context.Background(), dispatch.Request{
		Owner:         "org",
		Repo:          "repo",
		RunID:         "100",
		JobID:         "200",
		Labels:        []string{"runs-on=100"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	})
	if err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if gh.calls != 0 {
		t.Fatalf("gh should not be called on dedup, calls=%d", gh.calls)
	}
	jobs, _ := k8s.BatchV1().Jobs("gha-runners").List(context.Background(), metav1.ListOptions{})
	if len(jobs.Items) != 1 {
		t.Fatalf("jobs after dedup: %d", len(jobs.Items))
	}
}

func TestDispatchInvalidLabelsFails(t *testing.T) {
	gh := &fakeGH{}
	k8s := fake.NewSimpleClientset()
	d := dispatch.New(dispatch.Config{Namespace: "gha-runners"}, k8s, gh)

	err := d.Dispatch(context.Background(), dispatch.Request{
		Owner:         "org",
		Repo:          "repo",
		RunID:         "100",
		JobID:         "200",
		Labels:        []string{"cpu=banana"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	})
	if err == nil {
		t.Fatal("expected error for invalid labels")
	}
	if gh.calls != 0 {
		t.Fatal("gh should not be called")
	}
}

func TestDispatchSecretOwnerReference(t *testing.T) {
	gh := &fakeGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit", RunnerName: "ghs-1-2"}}
	k8s := fake.NewSimpleClientset()

	d := dispatch.New(dispatch.Config{Namespace: "gha-runners", RunnerImage: "img"}, k8s, gh)
	if err := d.Dispatch(context.Background(), dispatch.Request{
		Owner: "org", Repo: "repo", RunID: "1", JobID: "2",
		Labels:        []string{"runs-on=1"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}

	secrets, _ := k8s.CoreV1().Secrets("gha-runners").List(context.Background(), metav1.ListOptions{})
	if len(secrets.Items[0].OwnerReferences) == 0 {
		t.Fatal("expected owner reference on secret")
	}
	if secrets.Items[0].OwnerReferences[0].Kind != "Job" {
		t.Fatalf("owner kind: %s", secrets.Items[0].OwnerReferences[0].Kind)
	}
}

func TestDispatchRetriesGHClient(t *testing.T) {
	k8s := fake.NewSimpleClientset()
	failGH := &failThenSucceedGH{failures: 2, resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit", RunnerName: "ghs-1-2"}}
	d := dispatch.New(dispatch.Config{Namespace: "gha-runners", RunnerImage: "img", MaxAttempts: 3}, k8s, failGH)
	d.SetWait(func(ctx context.Context, d time.Duration) error { return nil })

	if err := d.Dispatch(context.Background(), dispatch.Request{
		Owner: "org", Repo: "repo", RunID: "1", JobID: "3",
		Labels:        []string{"runs-on=1"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	}); err != nil {
		t.Fatalf("Dispatch: %v", err)
	}
	if failGH.calls != 3 {
		t.Fatalf("calls: got %d want 3", failGH.calls)
	}
}

type failThenSucceedGH struct {
	failures int
	calls    int
	resp     ghclient.JITConfigResponse
}

func (f *failThenSucceedGH) GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error) {
	f.calls++
	if f.calls <= f.failures {
		return ghclient.JITConfigResponse{}, fmt.Errorf("transient")
	}
	return f.resp, nil
}

func (f *failThenSucceedGH) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	return nil
}

func TestDispatchCleansUpSecretOnJobFailure(t *testing.T) {
	gh := &recordingGH{resp: ghclient.JITConfigResponse{EncodedJITConfig: "jit", RunnerName: "ghs-1-2", RunnerID: 99}}
	k8s := fake.NewSimpleClientset()
	k8s.PrependReactor("create", "jobs", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("job create failed")
	})

	d := dispatch.New(dispatch.Config{Namespace: "gha-runners", RunnerImage: "img", MaxAttempts: 1}, k8s, gh)
	err := d.Dispatch(context.Background(), dispatch.Request{
		Owner: "org", Repo: "repo", RunID: "1", JobID: "4",
		Labels:        []string{"runs-on=1"},
		LabelDefaults: dispatch.LabelDefaults{CPU: 2, Arch: "x64"},
	})
	if err == nil {
		t.Fatal("expected error")
	}

	secrets, _ := k8s.CoreV1().Secrets("gha-runners").List(context.Background(), metav1.ListOptions{})
	if len(secrets.Items) != 0 {
		t.Fatalf("secret should be deleted, got %d", len(secrets.Items))
	}
	if gh.deletedRunnerID != 99 {
		t.Fatalf("delete runner: got %d want 99", gh.deletedRunnerID)
	}
}

type recordingGH struct {
	resp            ghclient.JITConfigResponse
	deletedRunnerID int64
}

func (r *recordingGH) GenerateJITConfig(ctx context.Context, owner, repo string, req ghclient.JITConfigRequest) (ghclient.JITConfigResponse, error) {
	return r.resp, nil
}

func (r *recordingGH) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	r.deletedRunnerID = runnerID
	return nil
}

// Ensure fake k8s tracks owner refs (compile-time sanity).
var _ runtime.Object = &corev1.Secret{}
