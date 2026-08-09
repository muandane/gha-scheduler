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

func withRetry(ctx context.Context, max int, wait waitFunc, fn func() error) error {
	if max < 1 {
		max = 1
	}
	if wait == nil {
		wait = defaultWait
	}
	var err error
	for attempt := 0; attempt < max; attempt++ {
		err = fn()
		if err == nil {
			return nil
		}
		if attempt == max-1 {
			break
		}
		delay := time.Duration(1<<attempt) * time.Second
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
	return err
}
