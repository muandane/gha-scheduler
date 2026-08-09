package reconciler

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/muandane/gha-scheduler/internal/ghclient"
	"github.com/muandane/gha-scheduler/internal/k8sjob"
)

type fakeRunnerGH struct {
	runners []ghclient.Runner
	deleted []int64
}

func (f *fakeRunnerGH) ListRunners(_ context.Context, _, _ string) ([]ghclient.Runner, error) {
	return f.runners, nil
}

func (f *fakeRunnerGH) DeleteRunner(_ context.Context, _, _ string, runnerID int64) error {
	f.deleted = append(f.deleted, runnerID)
	return nil
}

type fakeLeaseChecker struct {
	locked map[string]bool
}

func (f *fakeLeaseChecker) IsDispatchLocked(_ context.Context, jobID string) (bool, error) {
	return f.locked[jobID], nil
}

type fakeOrphanMetrics struct {
	deleted int
	skipped map[string]int
}

func (m *fakeOrphanMetrics) RecordOrphanRunnerDeleted(_ context.Context) { m.deleted++ }
func (m *fakeOrphanMetrics) RecordOrphanRunnerSkipped(_ context.Context, reason string) {
	if m.skipped == nil {
		m.skipped = make(map[string]int)
	}
	m.skipped[reason]++
}

func TestOrphanRunnerSweep_youngRunnerNotDeleted(t *testing.T) {
	gh := &fakeRunnerGH{runners: []ghclient.Runner{{
		ID: 1, Name: "ghs-100-200", CreatedAt: time.Now().Add(-30 * time.Second),
	}}}
	metrics := &fakeOrphanMetrics{}
	sweep := NewOrphanRunnerSweep(OrphanSweepConfig{
		Namespace: "gha-runners",
		Repos:     []string{"org/repo"},
		Grace:     2 * time.Minute,
		Metrics:   metrics,
	}, gh, fake.NewSimpleClientset(), &fakeLeaseChecker{})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gh.deleted) != 0 {
		t.Fatalf("expected no deletes, got %v", gh.deleted)
	}
	if metrics.skipped["young"] != 1 {
		t.Fatalf("expected young skip, got %v", metrics.skipped)
	}
}

func TestOrphanRunnerSweep_oldRunnerWithoutJobDeleted(t *testing.T) {
	gh := &fakeRunnerGH{runners: []ghclient.Runner{{
		ID: 42, Name: "ghs-100-200", CreatedAt: time.Now().Add(-5 * time.Minute),
	}}}
	metrics := &fakeOrphanMetrics{}
	sweep := NewOrphanRunnerSweep(OrphanSweepConfig{
		Namespace: "gha-runners",
		Repos:     []string{"org/repo"},
		Grace:     2 * time.Minute,
		Metrics:   metrics,
	}, gh, fake.NewSimpleClientset(), &fakeLeaseChecker{})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gh.deleted) != 1 || gh.deleted[0] != 42 {
		t.Fatalf("expected runner 42 deleted, got %v", gh.deleted)
	}
	if metrics.deleted != 1 {
		t.Fatalf("expected deleted metric, got %d", metrics.deleted)
	}
}

func TestOrphanRunnerSweep_matchingJobNotDeleted(t *testing.T) {
	gh := &fakeRunnerGH{runners: []ghclient.Runner{{
		ID: 7, Name: "ghs-100-200", CreatedAt: time.Now().Add(-5 * time.Minute),
	}}}
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ghs-job-100-200",
			Namespace: "gha-runners",
			Labels:    map[string]string{k8sjob.LabelGHJob: "200"},
		},
	}
	k8s := fake.NewSimpleClientset(job)
	metrics := &fakeOrphanMetrics{}
	sweep := NewOrphanRunnerSweep(OrphanSweepConfig{
		Namespace: "gha-runners",
		Repos:     []string{"org/repo"},
		Grace:     2 * time.Minute,
		Metrics:   metrics,
	}, gh, k8s, &fakeLeaseChecker{})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gh.deleted) != 0 {
		t.Fatalf("expected no deletes, got %v", gh.deleted)
	}
	if metrics.skipped["has_job"] != 1 {
		t.Fatalf("expected has_job skip, got %v", metrics.skipped)
	}
}

func TestOrphanRunnerSweep_wrongPrefixNeverTouched(t *testing.T) {
	gh := &fakeRunnerGH{runners: []ghclient.Runner{{
		ID: 9, Name: "arc-runner-200", CreatedAt: time.Now().Add(-5 * time.Minute),
	}}}
	metrics := &fakeOrphanMetrics{}
	sweep := NewOrphanRunnerSweep(OrphanSweepConfig{
		Namespace: "gha-runners",
		Repos:     []string{"org/repo"},
		Grace:     2 * time.Minute,
		Metrics:   metrics,
	}, gh, fake.NewSimpleClientset(), &fakeLeaseChecker{})

	if err := sweep.SweepOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(gh.deleted) != 0 {
		t.Fatalf("expected no deletes, got %v", gh.deleted)
	}
	if metrics.skipped["wrong_prefix"] != 1 {
		t.Fatalf("expected wrong_prefix skip, got %v", metrics.skipped)
	}
}

func TestParseJobIDFromRunnerName(t *testing.T) {
	jobID, ok := parseJobIDFromRunnerName("ghs-12345-67890")
	if !ok || jobID != "67890" {
		t.Fatalf("got %q %v", jobID, ok)
	}
	_, ok = parseJobIDFromRunnerName("not-ours")
	if ok {
		t.Fatal("expected false for wrong prefix")
	}
}
