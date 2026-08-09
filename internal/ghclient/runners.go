package ghclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Runner is a self-hosted runner registration on GitHub.
type Runner struct {
	ID        int64
	Name      string
	Status    string
	Busy      bool
	CreatedAt time.Time
}

type runnersAPIResponse struct {
	TotalCount int `json:"total_count"`
	Runners    []struct {
		ID        int64     `json:"id"`
		Name      string    `json:"name"`
		Status    string    `json:"status"`
		Busy      bool      `json:"busy"`
		CreatedAt time.Time `json:"created_at"`
	} `json:"runners"`
}

// ListRunners returns self-hosted runners for a repo (paginated).
func (c *Client) ListRunners(ctx context.Context, owner, repo string) ([]Runner, error) {
	var all []Runner
	page := 1
	for {
		q := url.Values{}
		q.Set("per_page", "100")
		q.Set("page", fmt.Sprintf("%d", page))
		path := fmt.Sprintf("/repos/%s/%s/actions/runners?%s", owner, repo, q.Encode())
		respBody, status, hdr, err := c.do(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, parseStatusError(status, string(respBody), hdr)
		}
		var apiResp runnersAPIResponse
		if err := json.Unmarshal(respBody, &apiResp); err != nil {
			return nil, fmt.Errorf("ghclient: decode runners: %w", err)
		}
		if len(apiResp.Runners) == 0 {
			break
		}
		for _, r := range apiResp.Runners {
			all = append(all, Runner{
				ID:        r.ID,
				Name:      r.Name,
				Status:    r.Status,
				Busy:      r.Busy,
				CreatedAt: r.CreatedAt,
			})
		}
		if len(apiResp.Runners) < 100 {
			break
		}
		page++
	}
	return all, nil
}
