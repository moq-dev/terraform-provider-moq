// Package client is a minimal MoQ Pro /v1 client: bearer auth, an
// Idempotency-Key on every create, and the fleet's retry policy.
package client

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultEndpoint is production; staging is https://api.moq.wtf/v1.
const DefaultEndpoint = "https://api.moq.pro/v1"

type Client struct {
	endpoint  string
	token     string
	userAgent string
	http      *http.Client

	// Retry budget: 4 attempts, waiting 200 ms doubling to 5 s, with jitter.
	attempts int
	baseWait time.Duration
	maxWait  time.Duration
}

func New(endpoint, token, userAgent string) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("endpoint %q is not an http(s) URL", endpoint)
	}
	if !strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/v1") {
		return nil, fmt.Errorf("endpoint %q must include the /v1 path, for example %s", endpoint, DefaultEndpoint)
	}
	if token == "" {
		return nil, errors.New("an account API token is required")
	}
	return &Client{
		endpoint:  strings.TrimRight(endpoint, "/"),
		token:     token,
		userAgent: userAgent,
		http:      &http.Client{Timeout: 60 * time.Second},
		attempts:  4,
		baseWait:  200 * time.Millisecond,
		maxWait:   5 * time.Second,
	}, nil
}

// Error is the API's refusal envelope.
type Error struct {
	Status      int          `json:"-"`
	Code        string       `json:"code"`
	Message     string       `json:"message"`
	Errors      []FieldError `json:"errors,omitempty"`
	LockedUntil string       `json:"lockedUntil,omitempty"`
}

type FieldError struct {
	Path    []any  `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s: %s", e.Status, e.Code, e.Message)
	for _, f := range e.Errors {
		path := make([]string, len(f.Path))
		for i, p := range f.Path {
			path[i] = fmt.Sprint(p)
		}
		fmt.Fprintf(&b, "\n  %s: %s", strings.Join(path, "."), f.Message)
	}
	if e.LockedUntil != "" {
		fmt.Fprintf(&b, " (locked until %s)", e.LockedUntil)
	}
	return b.String()
}

func IsNotFound(err error) bool {
	var e *Error
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

func HasCode(err error, code string) bool {
	var e *Error
	return errors.As(err, &e) && e.Code == code
}

// NewIdempotencyKey returns a fresh key for one logical create. Reuse it, with
// identical bytes, for every attempt of that create.
func NewIdempotencyKey() string {
	b := make([]byte, 16)
	_, _ = crand.Read(b)
	return "terraform-" + hex.EncodeToString(b)
}

// Request is one API call. Path is relative to the endpoint, such as
// "/projects". Body is JSON encoded when non-nil; Out is decoded when non-nil.
type Request struct {
	Method         string
	Path           string
	Query          url.Values
	Body           any
	IdempotencyKey string
	Out            any
}

func (c *Client) Do(ctx context.Context, r Request) error {
	var body []byte
	if r.Body != nil {
		var err error
		if body, err = json.Marshal(r.Body); err != nil {
			return err
		}
	}
	target := c.endpoint + r.Path
	if len(r.Query) > 0 {
		target += "?" + r.Query.Encode()
	}

	// Only a request that is safe to repeat retries an ambiguous failure: a
	// non-idempotent PATCH or POST might have taken effect before it surfaced.
	safe := r.Method == http.MethodGet || r.Method == http.MethodDelete || r.IdempotencyKey != ""

	for attempt := 1; ; attempt++ {
		last := attempt >= c.attempts
		res, err := c.send(ctx, r, target, body)
		if err != nil {
			if ctx.Err() != nil || !safe || last {
				return err
			}
			if err := c.wait(ctx, attempt, 0); err != nil {
				return err
			}
			continue
		}
		data, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil {
			return err
		}
		if res.StatusCode < 300 {
			if r.Out == nil || len(data) == 0 {
				return nil
			}
			if err := json.Unmarshal(data, r.Out); err != nil {
				return fmt.Errorf("%s %s: decoding response: %w", r.Method, r.Path, err)
			}
			return nil
		}

		apiErr := &Error{Status: res.StatusCode}
		if json.Unmarshal(data, apiErr) != nil || apiErr.Code == "" {
			apiErr.Code = "unknown"
			apiErr.Message = strings.TrimSpace(string(data))
		}
		if last || !retryable(res.StatusCode, apiErr.Code, safe) {
			return apiErr
		}
		// Retry-After is a floor on the backoff, never a replacement. An ask
		// longer than this call can wait is returned rather than ignored.
		asked := retryAfter(res.Header)
		if asked > c.maxWait {
			return apiErr
		}
		if err := c.wait(ctx, attempt, asked); err != nil {
			return err
		}
	}
}

func (c *Client) send(ctx context.Context, r Request, target string, body []byte) (*http.Response, error) {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		if r.Method == http.MethodPatch {
			req.Header.Set("Content-Type", "application/merge-patch+json")
		} else {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	if r.IdempotencyKey != "" {
		// A Structured Field string: the key is quoted.
		req.Header.Set("Idempotency-Key", strconv.Quote(r.IdempotencyKey))
	}
	return c.http.Do(req)
}

// retryable reports whether a refusal is transient. A 429 was refused before
// any work, so it always retries. Otherwise only a safe request retries, and
// only on a transient status or an in-flight idempotent create. Every other
// refusal, including an unrecognized one, is permanent.
func retryable(status int, code string, safe bool) bool {
	if status == http.StatusTooManyRequests {
		return true
	}
	if !safe {
		return false
	}
	switch status {
	case http.StatusConflict:
		return code == "idempotency_in_progress"
	case http.StatusRequestTimeout, http.StatusTooEarly:
		return true
	case http.StatusNotImplemented, http.StatusHTTPVersionNotSupported:
		return false
	default:
		return status >= 500
	}
}

func retryAfter(h http.Header) time.Duration {
	raw := strings.TrimSpace(h.Get("Retry-After"))
	if raw == "" {
		return 0
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs < 0 {
		return 0
	}
	return time.Duration(secs * float64(time.Second))
}

// wait sleeps an exponential, equal-jittered backoff of at least floor.
func (c *Client) wait(ctx context.Context, attempt int, floor time.Duration) error {
	delay := min(c.maxWait, c.baseWait<<(attempt-1))
	delay = delay/2 + rand.N(delay/2+1)
	delay = max(delay, floor)
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"nextCursor"`
}

// List reads every page of a paginated collection.
func List[T any](ctx context.Context, c *Client, path string, query url.Values) ([]T, error) {
	var all []T
	err := each(ctx, c, path, query, func(item T) bool {
		all = append(all, item)
		return false
	})
	return all, err
}

// Find pages through a collection until match accepts an item. The API has
// no single-item GET for keys, rules, or sync targets.
func Find[T any](ctx context.Context, c *Client, path string, match func(T) bool) (*T, error) {
	var found *T
	err := each(ctx, c, path, nil, func(item T) bool {
		if match(item) {
			found = &item
		}
		return found != nil
	})
	return found, err
}

// each calls visit for every item, in order, until it returns true.
func each[T any](ctx context.Context, c *Client, path string, query url.Values, visit func(T) bool) error {
	q := url.Values{}
	for k, v := range query {
		q[k] = v
	}
	q.Set("limit", "200")
	for {
		var p page[T]
		if err := c.Do(ctx, Request{Method: http.MethodGet, Path: path, Query: q, Out: &p}); err != nil {
			return err
		}
		for _, item := range p.Items {
			if visit(item) {
				return nil
			}
		}
		if p.NextCursor == nil {
			return nil
		}
		q.Set("cursor", *p.NextCursor)
	}
}

func Escape(segment string) string {
	return url.PathEscape(segment)
}
