package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

// server answers each request with the next status, recording what it saw.
type server struct {
	statuses []int
	codes    []string
	headers  []http.Header
	methods  []string
}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	i := len(s.headers)
	s.headers = append(s.headers, r.Header.Clone())
	s.methods = append(s.methods, r.Method)
	status := s.statuses[min(i, len(s.statuses)-1)]
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status >= 400 {
		code := "internal_error"
		if i < len(s.codes) && s.codes[i] != "" {
			code = s.codes[i]
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": "refused"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"id": "p1"})
}

func testClient(t *testing.T, s *server) *Client {
	t.Helper()
	ts := httptest.NewServer(s)
	t.Cleanup(ts.Close)
	c, err := New(ts.URL+"/v1", "moqpro_test", "test")
	if err != nil {
		t.Fatal(err)
	}
	c.baseWait, c.maxWait = 0, time.Second
	return c
}

func TestRetries(t *testing.T) {
	cases := []struct {
		name     string
		req      Request
		statuses []int
		codes    []string
		attempts int
		ok       bool
	}{
		{"get retries a 503", Request{Method: http.MethodGet}, []int{503, 200}, nil, 2, true},
		{"get gives up after the budget", Request{Method: http.MethodGet}, []int{503}, nil, 4, false},
		{"404 is permanent", Request{Method: http.MethodGet}, []int{404}, nil, 1, false},
		{"501 is permanent", Request{Method: http.MethodGet}, []int{501}, nil, 1, false},
		{"patch does not retry an ambiguous 503", Request{Method: http.MethodPatch, Body: map[string]any{}}, []int{503, 200}, nil, 1, false},
		{"patch retries a 429", Request{Method: http.MethodPatch, Body: map[string]any{}}, []int{429, 200}, nil, 2, true},
		{"keyed create retries a 503", Request{Method: http.MethodPost, Body: map[string]any{}, IdempotencyKey: "k"}, []int{503, 201}, nil, 2, true},
		{"unkeyed post does not retry a 503", Request{Method: http.MethodPost}, []int{503, 200}, nil, 1, false},
		{"keyed create retries 425 in flight", Request{Method: http.MethodPost, IdempotencyKey: "k"}, []int{425, 201}, nil, 2, true},
		{"keyed create retries 409 in progress", Request{Method: http.MethodPost, IdempotencyKey: "k"}, []int{409, 201}, []string{"idempotency_in_progress"}, 2, true},
		{"other 409 is permanent", Request{Method: http.MethodPost, IdempotencyKey: "k"}, []int{409, 201}, []string{"already_exists"}, 1, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &server{statuses: tc.statuses, codes: tc.codes}
			c := testClient(t, s)
			tc.req.Path = "/projects"
			err := c.Do(context.Background(), tc.req)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok %v", err, tc.ok)
			}
			if len(s.headers) != tc.attempts {
				t.Fatalf("attempts = %d, want %d", len(s.headers), tc.attempts)
			}
		})
	}
}

func TestIdempotencyKeyIsStableAndQuoted(t *testing.T) {
	s := &server{statuses: []int{503, 503, 201}}
	c := testClient(t, s)
	key := NewIdempotencyKey()
	if err := c.Do(context.Background(), Request{Method: http.MethodPost, Path: "/projects", Body: map[string]any{}, IdempotencyKey: key}); err != nil {
		t.Fatal(err)
	}
	for _, h := range s.headers {
		if got := h.Get("Idempotency-Key"); got != strconv.Quote(key) {
			t.Fatalf("Idempotency-Key = %s, want %q", got, key)
		}
	}
}

func TestRetryAfterBeyondBudgetIsReturned(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"code":"rate_limited","message":"slow down"}`))
	}))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL+"/v1", "moqpro_test", "test")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/projects"})
	if !HasCode(err, "rate_limited") {
		t.Fatalf("err = %v, want the rate_limited refusal without waiting", err)
	}
}

func TestErrorWithoutEnvelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("blocked by proxy"))
	}))
	t.Cleanup(ts.Close)
	c, err := New(ts.URL+"/v1", "moqpro_test", "test")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), Request{Method: http.MethodGet, Path: "/projects"})
	if err == nil || err.Error() != "403 unknown: blocked by proxy" {
		t.Fatalf("err = %v", err)
	}
}

func TestEndpointMustIncludeVersion(t *testing.T) {
	if _, err := New("https://api.moq.pro", "moqpro_test", "test"); err == nil {
		t.Fatal("accepted an endpoint without /v1")
	}
}

func TestLegacyPublicMigration(t *testing.T) {
	got := (&Public{V: 0, Publish: []string{"live"}, Subscribe: []string{""}}).V1()
	if got.V != 1 || got.Publish[0] != "live/**" || got.Subscribe[0] != "**" {
		t.Fatalf("migrated to %+v", got)
	}
}
