// PostgREST client — the read side of the Supabase backend. Three
// queries feed the meter (tui/SPEC.md §2/§7): the 30-day usage_daily
// baseline, the today-slice of raw requests since local midnight, and
// the inserted_at cursor query the polling LiveSource walks. All are
// plain anon-key GETs against <supabase_url>/rest/v1; RLS makes the anon
// role read-only, so no elevated credentials ever touch this client.

package data

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/harjeet2020/burnbar/tui/internal/core"
)

// baselineDays is how far back the usage_daily baseline reaches — the
// month window plus the current UTC day, matching the backend's 30-day
// retention (root SPEC §1). A few KB of rows; no local database needed.
const baselineDays = 30

// restTimeout bounds a single PostgREST request. Generous enough for a
// cold baseline over a slow link, short enough that a black-holed
// connection surfaces as an error instead of hanging the startup fetch.
const restTimeout = 15 * time.Second

// RESTClient issues the anon-key PostgREST reads. It is safe for
// concurrent use (net/http.Client is), so the startup batch and the
// polling loop can share one.
type RESTClient struct {
	baseURL string
	anonKey string
	hc      *http.Client
}

// NewRESTClient builds a client from the validated config. The base URL
// is <supabase_url>/rest/v1 with any trailing slash on the configured
// URL trimmed so paths join cleanly.
func NewRESTClient(cfg Config) *RESTClient {
	return &RESTClient{
		baseURL: strings.TrimRight(cfg.SupabaseURL, "/") + "/rest/v1",
		anonKey: cfg.SupabaseAnonKey,
		hc:      &http.Client{Timeout: restTimeout},
	}
}

// RESTError is a non-2xx response from PostgREST, carrying enough to
// diagnose a misconfiguration (wrong URL, bad key, missing view) without
// leaking a huge body into the status row.
type RESTError struct {
	Op         string // which fetch failed, for the log/status line
	StatusCode int
	Body       string // truncated response body
}

// Error renders the failure compactly.
func (e *RESTError) Error() string {
	return fmt.Sprintf("%s: PostgREST returned %d: %s", e.Op, e.StatusCode, e.Body)
}

// FetchBaseline reads the usage_daily view for the last baselineDays UTC
// days, newest first. This is the week/month source and the re-baseline
// target on refresh and realtime rejoin.
func (c *RESTClient) FetchBaseline(ctx context.Context) ([]core.DailyRow, error) {
	cutoff := core.UTCDayStart(time.Now()).AddDate(0, 0, -(baselineDays - 1))
	q := url.Values{}
	q.Set("select", "*")
	q.Set("day", "gte."+cutoff.Format("2006-01-02"))
	q.Set("order", "day.desc")

	body, err := c.get(ctx, "baseline", "/usage_daily", q)
	if err != nil {
		return nil, err
	}
	return decodeDaily(body)
}

// FetchTodaySlice reads the raw requests rows since the given local
// midnight — the exact source for the local-today window, which the
// UTC-day view rows cannot be re-cut to (tui/SPEC.md §7). Rows come back
// as fetched (not live): they seed the store but never carry accents.
func (c *RESTClient) FetchTodaySlice(ctx context.Context, sinceLocalMidnight time.Time) ([]core.RequestRow, error) {
	q := url.Values{}
	q.Set("select", "*")
	q.Set("requested_at", "gte."+sinceLocalMidnight.UTC().Format(time.RFC3339))
	q.Set("order", "requested_at.asc")

	body, err := c.get(ctx, "today-slice", "/requests", q)
	if err != nil {
		return nil, err
	}
	return decodeRequests(body)
}

// FetchSince reads raw requests rows inserted after the given cursor —
// the polling LiveSource's per-tick query (tui/SPEC.md §7). The returned
// rows are stamped live with a ReceivedAt of now, so they participate in
// accents and meter lag exactly as realtime events would; ordering by
// inserted_at lets the caller advance its cursor to the last row.
func (c *RESTClient) FetchSince(ctx context.Context, sinceInsertedAt time.Time) ([]core.RequestRow, error) {
	q := url.Values{}
	q.Set("select", "*")
	q.Set("inserted_at", "gt."+sinceInsertedAt.UTC().Format(time.RFC3339Nano))
	q.Set("order", "inserted_at.asc")

	body, err := c.get(ctx, "poll", "/requests", q)
	if err != nil {
		return nil, err
	}
	rows, err := decodeRequests(body)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	for i := range rows {
		rows[i].Live = true
		rows[i].ReceivedAt = now
	}
	return rows, nil
}

// get performs one anon-key GET and returns the response body, mapping
// any non-2xx into a *RESTError tagged with the operation name.
func (c *RESTClient) get(ctx context.Context, op, path string, q url.Values) ([]byte, error) {
	u := c.baseURL + path + "?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", op, err)
	}
	req.Header.Set("apikey", c.anonKey)
	req.Header.Set("Authorization", "Bearer "+c.anonKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only GET

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("%s: read body: %w", op, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &RESTError{Op: op, StatusCode: resp.StatusCode, Body: truncate(string(body), 300)}
	}
	return body, nil
}

// truncate shortens a string for error display, appending an ellipsis
// when it had to cut.
func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
