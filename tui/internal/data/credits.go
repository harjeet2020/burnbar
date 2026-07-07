// OpenRouter credits client — the remaining-balance figure in the header
// (tui/SPEC.md §7). It talks straight to OpenRouter with the user's local
// inference key; the value never touches the Supabase backend and is
// shown as-is with no local decrement (subtracting broadcast costs from a
// ~60s-stale cached balance causes bounce-up artifacts, root SPEC §2).

package data

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// creditsURL is the OpenRouter credits endpoint.
const creditsURL = "https://openrouter.ai/api/v1/credits"

// creditsTimeout bounds one credits request.
const creditsTimeout = 10 * time.Second

// ErrCreditsUnauthorized is returned on a 401 so the UI can show the
// specific "check openrouter_api_key" hint rather than a generic error
// (tui/SPEC.md §8).
var ErrCreditsUnauthorized = errors.New("openrouter credits: unauthorized (check openrouter_api_key)")

// CreditsClient polls the OpenRouter credits endpoint. Zero-value-unsafe:
// build it with NewCreditsClient.
type CreditsClient struct {
	apiKey string
	hc     *http.Client
}

// NewCreditsClient builds a client from config. The caller is expected to
// gate calls on Config.HasCredentialsForCredits — an empty key here would
// simply 401.
func NewCreditsClient(cfg Config) *CreditsClient {
	return &CreditsClient{
		apiKey: cfg.OpenRouterAPIKey,
		hc:     &http.Client{Timeout: creditsTimeout},
	}
}

// creditsResponse is the OpenRouter envelope: the balance fields nest
// under a top-level "data" object.
type creditsResponse struct {
	Data struct {
		TotalCredits float64 `json:"total_credits"`
		TotalUsage   float64 `json:"total_usage"`
	} `json:"data"`
}

// Fetch returns the remaining balance: total_credits − total_usage. A 401
// maps to ErrCreditsUnauthorized; other non-2xx responses return a
// generic error. On any failure the caller keeps showing the last value
// with a growing age (tui/SPEC.md §7).
func (c *CreditsClient) Fetch(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, creditsURL, nil)
	if err != nil {
		return 0, fmt.Errorf("credits: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return 0, fmt.Errorf("credits: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only GET

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return 0, fmt.Errorf("credits: read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return 0, ErrCreditsUnauthorized
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("credits: OpenRouter returned %d: %s", resp.StatusCode, truncate(string(body), 200))
	}

	var cr creditsResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return 0, fmt.Errorf("credits: decode: %w", err)
	}
	return cr.Data.TotalCredits - cr.Data.TotalUsage, nil
}
