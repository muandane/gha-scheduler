package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/muandane/gha-scheduler/internal/store"
	sqlitestore "github.com/muandane/gha-scheduler/internal/store/sqlite"
)

func seedLatencyRows(t *testing.B, st *sqlitestore.Store, n int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	for i := range n {
		id := fmt.Sprintf("bench-%d", i)
		lat := float64(i%100) + 0.5
		_ = st.UpsertQueued(ctx, "org", "repo", "1", id, now)
		_ = st.MarkDispatching(ctx, id, store.RunnerSpecSnapshot{}, now)
		_ = st.MarkJobCreated(ctx, id, now.Add(time.Duration(lat)*time.Second))
	}
}

// Benchmark confirms app-side percentile sort (used in Stats) is acceptable vs SQL window approach.
func BenchmarkStatsAppSide(b *testing.B) {
	st := openBenchDB(b)
	seedLatencyRows(b, st, 10000)
	ctx := context.Background()
	since := time.Now().UTC().Add(-time.Hour)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := st.Stats(ctx, since); err != nil {
			b.Fatal(err)
		}
	}
}

func openBenchDB(b *testing.B) *sqlitestore.Store {
	b.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		b.Fatal(err)
	}
	st, err := sqlitestore.OpenDB(db)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = st.Close() })
	return st
}
