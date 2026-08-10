package ghclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TokenFunc returns a bearer token for GitHub API calls.
type TokenFunc func(ctx context.Context) (string, error)

// Client wraps GitHub REST calls used by the scheduler.
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      TokenFunc
	limitsMu   sync.RWMutex
	limits     RateLimits
}

// RateLimits holds the last observed GitHub API rate limit headers.
type RateLimits struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// Option configures a Client.
type Option func(*Client)

// WithHTTPClient sets a custom HTTP client (for tests).
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) {
		cl.httpClient = c
	}
}

// WithTokenFunc sets a function that supplies installation access tokens.
func WithTokenFunc(fn TokenFunc) Option {
	return func(cl *Client) {
		cl.token = fn
	}
}

// New creates a Client with the given API base URL (e.g. https://api.github.com).
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// JITConfigRequest is the body for generate-jitconfig.
type JITConfigRequest struct {
	Name          string   `json:"name"`
	RunnerGroupID int64    `json:"runner_group_id"`
	Labels        []string `json:"labels,omitempty"`
}

// JITConfigResponse is the generate-jit-config response.
type JITConfigResponse struct {
	EncodedJITConfig string
	RunnerName       string
	RunnerID         int64
}

type jitConfigAPIResponse struct {
	EncodedJITConfig string `json:"encoded_jit_config"`
	Runner           struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runner"`
}

// GenerateJITConfig calls POST /repos/{owner}/{repo}/actions/runners/generate-jitconfig.
func (c *Client) GenerateJITConfig(ctx context.Context, owner, repo string, req JITConfigRequest) (JITConfigResponse, error) {
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/generate-jitconfig", owner, repo)
	body, err := json.Marshal(req)
	if err != nil {
		return JITConfigResponse{}, err
	}

	respBody, status, hdr, err := c.do(ctx, http.MethodPost, path, body)
	if err != nil {
		return JITConfigResponse{}, err
	}
	if status < 200 || status >= 300 {
		return JITConfigResponse{}, parseStatusError(status, string(respBody), hdr)
	}

	var apiResp jitConfigAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return JITConfigResponse{}, fmt.Errorf("ghclient: decode jit config: %w", err)
	}
	return JITConfigResponse{
		EncodedJITConfig: apiResp.EncodedJITConfig,
		RunnerName:       apiResp.Runner.Name,
		RunnerID:         apiResp.Runner.ID,
	}, nil
}

// DeleteRunner removes an ephemeral runner registration (JIT orphan cleanup).
func (c *Client) DeleteRunner(ctx context.Context, owner, repo string, runnerID int64) error {
	if runnerID == 0 {
		return nil
	}
	path := fmt.Sprintf("/repos/%s/%s/actions/runners/%d", owner, repo, runnerID)
	_, status, hdr, err := c.do(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return parseStatusError(status, "", hdr)
	}
	return nil
}

// WorkflowRun is a minimal queued-run record for the reconciler.
type WorkflowRun struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

type workflowRunsResponse struct {
	WorkflowRuns []WorkflowRun `json:"workflow_runs"`
}

// ListQueuedRuns returns workflow runs with status=queued.
func (c *Client) ListQueuedRuns(ctx context.Context, owner, repo string) ([]WorkflowRun, error) {
	return c.ListRuns(ctx, owner, repo, []string{"queued"})
}

// ListRuns returns workflow runs matching any of the given statuses (paginated).
func (c *Client) ListRuns(ctx context.Context, owner, repo string, statuses []string) ([]WorkflowRun, error) {
	seen := make(map[int64]struct{})
	var all []WorkflowRun
	for _, status := range statuses {
		page := 1
		for {
			q := url.Values{}
			q.Set("status", status)
			q.Set("per_page", "100")
			q.Set("page", fmt.Sprintf("%d", page))
			path := fmt.Sprintf("/repos/%s/%s/actions/runs?%s", owner, repo, q.Encode())
			respBody, code, hdr, err := c.do(ctx, http.MethodGet, path, nil)
			if err != nil {
				return nil, err
			}
			if code < 200 || code >= 300 {
				return nil, parseStatusError(code, string(respBody), hdr)
			}
			var apiResp workflowRunsResponse
			if err := json.Unmarshal(respBody, &apiResp); err != nil {
				return nil, fmt.Errorf("ghclient: decode runs: %w", err)
			}
			if len(apiResp.WorkflowRuns) == 0 {
				break
			}
			for _, run := range apiResp.WorkflowRuns {
				if _, ok := seen[run.ID]; ok {
					continue
				}
				seen[run.ID] = struct{}{}
				all = append(all, run)
			}
			if len(apiResp.WorkflowRuns) < 100 {
				break
			}
			page++
		}
	}
	return all, nil
}

// WorkflowJob is a queued workflow job for reconciler dispatch.
type WorkflowJob struct {
	ID        int64     `json:"id"`
	RunID     int64     `json:"run_id"`
	Status    string    `json:"status"`
	Labels    []string  `json:"labels"`
	CreatedAt time.Time `json:"created_at"`
}

type workflowJobsResponse struct {
	Jobs []WorkflowJob `json:"jobs"`
}

// ListRunJobs returns jobs for a workflow run (paginated).
func (c *Client) ListRunJobs(ctx context.Context, owner, repo string, runID int64) ([]WorkflowJob, error) {
	var all []WorkflowJob
	page := 1
	for {
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprintf("%d", page))
		path := fmt.Sprintf("/repos/%s/%s/actions/runs/%d/jobs?%s", owner, repo, runID, q.Encode())
		respBody, status, hdr, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, parseStatusError(status, string(respBody), hdr)
		}
		var apiResp workflowJobsResponse
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, fmt.Errorf("ghclient: decode jobs: %w", err)
		}
		if len(apiResp.Jobs) == 0 {
			break
		}
		all = append(all, apiResp.Jobs...)
		if len(apiResp.Jobs) < 100 {
			break
		}
		page++
	}
	return all, nil
}

func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, int, http.Header, error) {
	var bodyReader io.Reader
	if body != nil {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/vnd.github+json")

	if c.token != nil {
		tok, err := c.token(ctx)
		if err != nil {
			return nil, 0, nil, err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, 0, nil, err
	}
	defer resp.Body.Close()

	c.recordRateLimits(resp.Header)

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, resp.Header, err
	}
	return respBody, resp.StatusCode, resp.Header, nil
}

func (c *Client) recordRateLimits(h http.Header) {
	if h == nil {
		return
	}
	lim, _ := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	rem, _ := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	var reset time.Time
	if v := h.Get("X-RateLimit-Reset"); v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			reset = time.Unix(sec, 0)
		}
	}
	if lim == 0 && rem == 0 && reset.IsZero() {
		return
	}
	c.limitsMu.Lock()
	c.limits = RateLimits{Limit: lim, Remaining: rem, Reset: reset}
	c.limitsMu.Unlock()
	slog.Debug("github rate limit", "limit", lim, "remaining", rem, "reset", reset)
}

// LastRateLimits returns the most recently observed rate limit headers.
func (c *Client) LastRateLimits() RateLimits {
	c.limitsMu.RLock()
	defer c.limitsMu.RUnlock()
	return c.limits
}
