package dispatch

import (
	"context"
	"time"
)

const defaultMaxAttempts = 3

type waitFunc func(ctx context.Context, d time.Duration) error

func defaultWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func withRetry(ctx context.Context, max int, wait waitFunc, backoff *Backoff, key string, fn func() error) error {
	return withRetryBackoff(ctx, max, wait, backoff, key, fn)
}
