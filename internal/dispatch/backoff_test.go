package dispatch

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

func TestBackoffHonorsRetryAfter(t *testing.T) {
	b := newBackoff()
	var waits []time.Duration
	wait := func(_ context.Context, d time.Duration) error {
		waits = append(waits, d)
		return nil
	}
	rateErr := ghclient.NewRateLimitedError(30 * time.Second)
	err := withRetryBackoff(context.Background(), 2, wait, b, "k", func() error {
		return rateErr
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if len(waits) != 1 {
		t.Fatalf("expected 1 wait, got %d", len(waits))
	}
	if waits[0] < 30*time.Second {
		t.Fatalf("expected retry-after delay >= 30s, got %v", waits[0])
	}
}

func TestBackoffResetsOnSuccess(t *testing.T) {
	b := newBackoff()
	calls := 0
	err := withRetryBackoff(context.Background(), 3, defaultWait, b, "k", func() error {
		calls++
		if calls == 1 {
			return errors.New("transient")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls, got %d", calls)
	}
}
