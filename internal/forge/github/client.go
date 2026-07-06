// Package github implements the forge.Forge and forge.Repository interfaces
// against the GitHub REST API using only the standard library.
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the public GitHub REST API root. For GitHub Enterprise the
// caller supplies a different base (e.g. https://ghe.example.com/api/v3).
const DefaultBaseURL = "https://api.github.com"

// apiVersion pins the REST API version header GitHub recommends sending.
const apiVersion = "2022-11-28"

// maxRateLimitWait bounds how long the client will block on a secondary/primary
// rate-limit reset before giving up.
const maxRateLimitWait = 60 * time.Second

// client is a minimal authenticated GitHub REST client.
type client struct {
	http    *http.Client
	baseURL string
	token   string
	// now and sleep are indirections to make rate-limit handling testable.
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
}

// Option configures a client.
type Option func(*client)

// WithBaseURL overrides the API root (for GitHub Enterprise).
func WithBaseURL(base string) Option {
	return func(c *client) { c.baseURL = strings.TrimRight(base, "/") }
}

// WithHTTPClient overrides the underlying *http.Client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *client) { c.http = h }
}

// newClient builds a GitHub client authenticated with token.
func newClient(token string, opts ...Option) *client {
	c := &client{
		http:    &http.Client{Timeout: 30 * time.Second},
		baseURL: DefaultBaseURL,
		token:   token,
		now:     time.Now,
		sleep:   sleepCtx,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError is a typed error carrying the HTTP status and GitHub's message.
type APIError struct {
	StatusCode int
	Message    string
	Method     string
	Path       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: %s %s: %d %s", e.Method, e.Path, e.StatusCode, e.Message)
}

// do issues a request against path (relative to baseURL) and decodes a JSON
// response body into out (which may be nil). It transparently retries once when
// GitHub reports the rate limit is exhausted and the reset is within
// maxRateLimitWait. It returns the raw *http.Response header set for callers
// that need pagination Link headers.
func (c *client) do(ctx context.Context, method, path string, body io.Reader, out any) (http.Header, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}

		if wait, ok := rateLimited(resp, c.now()); ok && attempt == 0 && wait <= maxRateLimitWait {
			io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if err := c.sleep(ctx, wait); err != nil {
				return nil, err
			}
			continue
		}

		hdr, err := c.decode(resp, method, path, out)
		return hdr, err
	}
}

// decode consumes resp, returning the header set on success or an *APIError.
func (c *client) decode(resp *http.Response, method, path string, out any) (http.Header, error) {
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := decodeErrorMessage(resp.Body)
		return resp.Header, &APIError{
			StatusCode: resp.StatusCode,
			Message:    msg,
			Method:     method,
			Path:       path,
		}
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return resp.Header, fmt.Errorf("github: decode %s %s: %w", method, path, err)
		}
	} else {
		io.Copy(io.Discard, resp.Body)
	}
	return resp.Header, nil
}

// ghErrorBody is GitHub's standard error envelope.
type ghErrorBody struct {
	Message string `json:"message"`
}

func decodeErrorMessage(r io.Reader) string {
	var b ghErrorBody
	if err := json.NewDecoder(r).Decode(&b); err == nil && b.Message != "" {
		return b.Message
	}
	return "request failed"
}

// rateLimited reports whether resp indicates the rate limit is exhausted and,
// if so, how long to wait until the reset.
func rateLimited(resp *http.Response, now time.Time) (time.Duration, bool) {
	if resp.StatusCode != http.StatusForbidden && resp.StatusCode != http.StatusTooManyRequests {
		return 0, false
	}
	if resp.Header.Get("X-RateLimit-Remaining") != "0" {
		return 0, false
	}
	reset, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64)
	if err != nil {
		return 0, false
	}
	wait := time.Until(time.Unix(reset, 0))
	// Recompute against the injected clock for testability.
	wait = time.Unix(reset, 0).Sub(now)
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

// sleepCtx sleeps for d or returns early if ctx is cancelled.
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// nextPageURL extracts the rel="next" URL from a Link header, returning the
// path+query relative to baseURL, or "" when there is no next page.
func (c *client) nextPageURL(h http.Header) string {
	for _, part := range strings.Split(h.Get("Link"), ",") {
		segs := strings.Split(strings.TrimSpace(part), ";")
		if len(segs) < 2 {
			continue
		}
		rawURL := strings.Trim(strings.TrimSpace(segs[0]), "<>")
		for _, p := range segs[1:] {
			if strings.TrimSpace(p) == `rel="next"` {
				return c.relativePath(rawURL)
			}
		}
	}
	return ""
}

// relativePath converts an absolute API URL into the path+query used by do.
func (c *client) relativePath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.RawQuery != "" {
		return u.Path + "?" + u.RawQuery
	}
	return u.Path
}

// asAPIError returns the *APIError in err's chain, if any.
func asAPIError(err error) (*APIError, bool) {
	var e *APIError
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
