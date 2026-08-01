package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// recorder serves a minimal user payload and captures the request it observed.
type recorder struct {
	*httptest.Server

	requests []*http.Request
}

func newRecorder(t *testing.T) *recorder {
	t.Helper()

	r := &recorder{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.requests = append(r.requests, req.Clone(context.Background()))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"login":"octocat"}`))
	}))
	t.Cleanup(r.Close)

	return r
}

func (r *recorder) only(t *testing.T) *http.Request {
	t.Helper()

	if len(r.requests) != 1 {
		t.Fatalf("observed %d requests, want exactly 1", len(r.requests))
	}

	return r.requests[0]
}

// get drives one request through the client so the wire behaviour is observable.
func get(t *testing.T, c *Client) {
	t.Helper()

	if _, _, err := c.gh.Users.Get(context.Background(), "octocat"); err != nil {
		t.Fatalf("Users.Get: %v", err)
	}
}

func TestNewWithoutTokenSendsNoAuthorizationHeader(t *testing.T) {
	srv := newRecorder(t)

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get(t, c)

	for name, values := range srv.only(t).Header {
		if strings.Contains(strings.ToLower(name), "authorization") {
			t.Errorf("unauthenticated request carries %q: %q", name, values)
		}
	}
}

func TestNewWithTokenSendsBearerAuthorization(t *testing.T) {
	srv := newRecorder(t)

	c, err := New(Options{BaseURL: srv.URL, Token: "not-a-real-token"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get(t, c)

	if got, want := srv.only(t).Header.Get("Authorization"), "Bearer not-a-real-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestNewSendsConfiguredUserAgent(t *testing.T) {
	srv := newRecorder(t)

	c, err := New(Options{BaseURL: srv.URL, UserAgent: "readme-builder/1.2.3"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get(t, c)

	if got, want := srv.only(t).Header.Get("User-Agent"), "readme-builder/1.2.3"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

func TestNewSendsDefaultUserAgentWhenUnset(t *testing.T) {
	srv := newRecorder(t)

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get(t, c)

	if got, want := srv.only(t).Header.Get("User-Agent"), "readme-builder"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
}

func TestNewSendsRequestsToConfiguredBaseURL(t *testing.T) {
	srv := newRecorder(t)

	c, err := New(Options{BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	get(t, c)

	if got, want := srv.only(t).URL.Path, "/users/octocat"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
}

func TestNewDefaultsToPublicAPIWhenBaseURLUnset(t *testing.T) {
	c, err := New(Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got, want := c.gh.BaseURL(), "https://api.github.com/"; got != want {
		t.Errorf("BaseURL = %q, want %q", got, want)
	}
}

func TestNewRejectsInvalidBaseURL(t *testing.T) {
	if _, err := New(Options{BaseURL: "http://example.com/\x7f"}); err == nil {
		t.Fatal("New accepted an invalid base URL, want error")
	}
}

func TestNewRejectsNegativeTimeout(t *testing.T) {
	if _, err := New(Options{Timeout: -time.Second}); err == nil {
		t.Fatal("New accepted a negative timeout, want error")
	}
}

func TestNewAppliesTimeout(t *testing.T) {
	tests := map[string]struct {
		timeout time.Duration
		want    time.Duration
	}{
		"configured": {timeout: 5 * time.Second, want: 5 * time.Second},
		"default":    {timeout: 0, want: 30 * time.Second},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			c, err := New(Options{Timeout: tt.timeout})
			if err != nil {
				t.Fatalf("New: %v", err)
			}

			if got := c.gh.Client().Timeout; got != tt.want {
				t.Errorf("http timeout = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewTimeoutAbortsSlowRequest(t *testing.T) {
	blocked := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		<-blocked
	}))
	t.Cleanup(func() {
		close(blocked)
		srv.Close()
	})

	c, err := New(Options{BaseURL: srv.URL, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, _, err = c.gh.Users.Get(context.Background(), "octocat")
	if err == nil {
		t.Fatal("slow request succeeded, want a timeout error")
	}
	if !strings.Contains(err.Error(), "deadline exceeded") && !strings.Contains(err.Error(), "timeout") {
		t.Errorf("error = %v, want a timeout", err)
	}
}
