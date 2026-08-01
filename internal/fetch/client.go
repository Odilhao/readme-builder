package fetch

import (
	"fmt"
	"time"

	"github.com/google/go-github/v89/github"
)

// Identifies the tool to GitHub without naming anyone; a release version is
// injected by the caller through Options.UserAgent.
const defaultUserAgent = "readme-builder"

// DefaultTimeout bounds a single API request when Options.Timeout is zero.
const DefaultTimeout = 30 * time.Second

// Options configures the client. The zero value is valid and yields an
// unauthenticated client against the public GitHub API.
type Options struct {
	// Token only raises the rate limit. It is always supplied by the caller;
	// this package never reads the environment.
	Token string

	// BaseURL redirects the API, for tests and GitHub Enterprise. Empty means
	// api.github.com.
	BaseURL string

	UserAgent string
	Timeout   time.Duration
}

// Client is the GitHub REST client shared by every fetcher.
type Client struct {
	gh *github.Client
}

// New builds a client from opts. Without a token the client is unauthenticated
// and sends no Authorization header at all.
func New(opts Options) (*Client, error) {
	userAgent := opts.UserAgent
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	timeout := opts.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}

	clientOpts := []github.ClientOptionsFunc{
		github.WithUserAgent(userAgent),
		github.WithTimeout(timeout),
	}
	if opts.BaseURL != "" {
		clientOpts = append(clientOpts, github.WithURLs(&opts.BaseURL, nil))
	}
	// WithAuthToken rejects an empty token, and an absent token must leave the
	// Authorization header unset rather than send an empty one.
	if opts.Token != "" {
		clientOpts = append(clientOpts, github.WithAuthToken(opts.Token))
	}

	gh, err := github.NewClient(clientOpts...)
	if err != nil {
		return nil, fmt.Errorf("github client: %w", err)
	}

	return &Client{gh: gh}, nil
}
