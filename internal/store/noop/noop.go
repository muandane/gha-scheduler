package noop

import (
	"context"
	"time"

	"github.com/muandane/gha-scheduler/internal/store"
)

// Store is a no-op JobStore used when persistence is disabled.
type Store struct{}

func (Store) UpsertQueued(context.Context, string, string, string, string, time.Time) error {
	return nil
}
func (Store) MarkDispatching(context.Context, string, store.RunnerSpecSnapshot, time.Time) error {
	return nil
}
func (Store) MarkJobCreated(context.Context, string, time.Time) error { return nil }
func (Store) MarkScheduled(context.Context, string, string, time.Time) error {
	return nil
}
func (Store) MarkRunning(context.Context, string, time.Time) error { return nil }
func (Store) MarkCompleted(context.Context, string, int, time.Time) error {
	return nil
}
func (Store) MarkDispatchError(context.Context, string, string, time.Time) error {
	return nil
}
func (Store) GetJob(context.Context, string) (*store.Job, error) { return nil, nil }
func (Store) ListJobs(context.Context, store.ListQuery) (store.ListResult, error) {
	return store.ListResult{}, nil
}
func (Store) Stats(context.Context, time.Time) (store.Stats, error) {
	return store.Stats{}, nil
}
func (Store) Prune(context.Context, time.Time) (int64, error) { return 0, nil }
func (Store) Close() error                                     { return nil }
