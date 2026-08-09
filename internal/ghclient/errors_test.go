package ghclient

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestParseStatusError(t *testing.T) {
	h := http.Header{}
	h.Set("Retry-After", "60")
	err := parseStatusError(http.StatusTooManyRequests, "slow down", h)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("expected ErrRateLimited, got %v", err)
	}
	var api *APIError
	if !errors.As(err, &api) || api.RetryAfter != 60*time.Second {
		t.Fatalf("retry-after not parsed: %+v", api)
	}
}
