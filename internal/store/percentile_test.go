package store_test

import (
	"testing"

	"github.com/muandane/gha-scheduler/internal/store"
)

func TestPercentile(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50}
	if got := store.Percentile(vals, 0.5); got != 30 {
		t.Fatalf("p50 = %v, want 30", got)
	}
	if got := store.Percentile(vals, 0.95); got < 47 || got > 49 {
		t.Fatalf("p95 = %v, want ~48", got)
	}
	if got := store.Percentile(nil, 0.5); got != 0 {
		t.Fatalf("empty = %v", got)
	}
}
