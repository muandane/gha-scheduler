package ghclient

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

var (
	ErrNotFound     = errors.New("ghclient: not found")
	ErrUnauthorized = errors.New("ghclient: unauthorized")
	ErrBadRequest   = errors.New("ghclient: bad request")
	ErrRateLimited  = errors.New("ghclient: rate limited")
	ErrUnexpected   = errors.New("ghclient: unexpected status")
)

// APIError carries HTTP status and optional Retry-After for backoff.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration
	err        error
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("%v: status %d", e.err, e.Status)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	if e.RetryAfter > 0 {
		msg += fmt.Sprintf(" retry_after=%s", e.RetryAfter)
	}
	return msg
}

func (e *APIError) Unwrap() error { return e.err }

func parseStatusError(status int, body string, headers http.Header) error {
	retryAfter := parseRetryAfter(headers.Get("Retry-After"))
	switch status {
	case http.StatusNotFound:
		return &APIError{Status: status, Body: body, err: ErrNotFound}
	case http.StatusUnauthorized, http.StatusForbidden:
		return &APIError{Status: status, Body: body, err: ErrUnauthorized}
	case http.StatusBadRequest:
		return &APIError{Status: status, Body: body, err: ErrBadRequest}
	case http.StatusTooManyRequests:
		return &APIError{Status: status, Body: body, RetryAfter: retryAfter, err: ErrRateLimited}
	default:
		if status >= 500 {
			return &APIError{Status: status, Body: body, RetryAfter: retryAfter, err: ErrUnexpected}
		}
		return &APIError{Status: status, Body: body, err: ErrUnexpected}
	}
}

func NewRateLimitedError(retryAfter time.Duration) error {
	return &APIError{
		Status:     http.StatusTooManyRequests,
		RetryAfter: retryAfter,
		err:        ErrRateLimited,
	}
}

func parseRetryAfter(v string) time.Duration {
	if v == "" {
		return 0
	}
	if sec, err := strconv.Atoi(v); err == nil {
		return time.Duration(sec) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		d := time.Until(t)
		if d < 0 {
			return 0
		}
		return d
	}
	return 0
}
