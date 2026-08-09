package dispatch

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"sync"
	"time"

	"github.com/muandane/gha-scheduler/internal/ghclient"
)

// BackoffConfig configures exponential backoff with jitter.
type BackoffConfig struct {
	Initial    time.Duration
	Multiplier float64
	Max        time.Duration
	JitterFrac float64
}

func defaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		Initial:    10 * time.Second,
		Multiplier: 1.5,
		Max:        5 * time.Minute,
		JitterFrac: 0.2,
	}
}

type backoffEntry struct {
	backoff time.Duration
	mux     sync.Mutex
}

// Backoff tracks per-key retry delays.
type Backoff struct {
	cfg     BackoffConfig
	entries sync.Map
}

func newBackoff() *Backoff {
	return &Backoff{cfg: defaultBackoffConfig()}
}

func (b *Backoff) delayFor(key string) time.Duration {
	v, _ := b.entries.LoadOrStore(key, &backoffEntry{backoff: b.cfg.Initial})
	entry := v.(*backoffEntry)
	entry.mux.Lock()
	defer entry.mux.Unlock()
	d := entry.backoff
	entry.backoff = min(time.Duration(float64(entry.backoff)*b.cfg.Multiplier), b.cfg.Max)
	return b.jitter(d)
}

func (b *Backoff) reset(key string) {
	b.entries.Delete(key)
}

func (b *Backoff) jitter(d time.Duration) time.Duration {
	if b.cfg.JitterFrac <= 0 {
		return d
	}
	maxJitter := int64(float64(d) * b.cfg.JitterFrac)
	if maxJitter <= 0 {
		return d
	}
	n, err := rand.Int(rand.Reader, big.NewInt(maxJitter*2+1))
	if err != nil {
		return d
	}
	return d + time.Duration(n.Int64()-maxJitter)
}

func retryAfterFromErr(err error) time.Duration {
	var apiErr *ghclient.APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return apiErr.RetryAfter
	}
	return 0
}

func withRetryBackoff(ctx context.Context, max int, wait waitFunc, backoff *Backoff, key string, fn func() error) error {
	if max < 1 {
		max = 1
	}
	if wait == nil {
		wait = defaultWait
	}
	if backoff == nil {
		backoff = newBackoff()
	}
	var err error
	for attempt := 0; attempt < max; attempt++ {
		err = fn()
		if err == nil {
			backoff.reset(key)
			return nil
		}
		if attempt == max-1 {
			break
		}
		delay := backoff.delayFor(key)
		if ra := retryAfterFromErr(err); ra > delay {
			delay = ra
		}
		if err := wait(ctx, delay); err != nil {
			return err
		}
	}
	return err
}
