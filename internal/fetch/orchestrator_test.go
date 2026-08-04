package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
)

// orchestratorServer mocks all GitHub and RSS endpoints for testing the
// orchestrator, recording which endpoints were actually called.
type orchestratorServer struct {
	*httptest.Server

	requestedPaths []string
}

func newOrchestratorServer(t *testing.T) *orchestratorServer {
	t.Helper()

	s := &orchestratorServer{}
	mux := http.NewServeMux()

	// GET /users/{u}/events/public for contributions
	mux.HandleFunc("/users/octocat/events/public", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		e := event{Type: "PushEvent", CreatedAt: base}
		e.Repo.Name = "example-org/example-repo"
		events := []event{e}
		_ = json.NewEncoder(w).Encode(events)
	})

	// GET /users/{u}/repos for repos
	mux.HandleFunc("/users/octocat/repos", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		repos := []repoItem{{
			Name:        "Hello-World",
			FullName:    "octocat/Hello-World",
			Fork:        false,
			PushedAt:    pushed,
			HTMLURL:     "https://github.com/octocat/Hello-World",
			Language:    "Go",
			Description: "my first repo",
		}}
		_ = json.NewEncoder(w).Encode(repos)
	})

	// GET /search/issues for pull requests
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				{
					"number":         42,
					"title":          "Fix bug",
					"html_url":       "https://github.com/example-org/example-repo/pull/42",
					"state":          "merged",
					"repository_url": "https://api.github.com/repos/example-org/example-repo",
					"pull_request": map[string]any{
						"url": "https://api.github.com/repos/example-org/example-repo/pulls/42",
					},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(result)
	})

	// GET /users/{u}/starred for starred repositories
	mux.HandleFunc("/users/octocat/starred", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		starred := []starItem{{
			StarredAt: base,
			Repo: repoItem{
				Name:     "starred-repo",
				FullName: "example-org/starred-repo",
				Fork:     false,
				PushedAt: pushed,
				HTMLURL:  "https://github.com/example-org/starred-repo",
			},
		}}
		_ = json.NewEncoder(w).Encode(starred)
	})

	// GET /users/{u}/followers for followers
	mux.HandleFunc("/users/octocat/followers", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		followers := []followerItem{{
			Login:     "example-user",
			AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4",
			HTMLURL:   "https://github.com/example-user",
		}}
		_ = json.NewEncoder(w).Encode(followers)
	})

	// GET /users/{u}/gists for gists
	mux.HandleFunc("/users/octocat/gists", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		gists := []gistItem{{
			ID:          "gist123",
			URL:         "https://gist.github.com/octocat/gist123",
			Description: "my first gist",
			CreatedAt:   base,
		}}
		_ = json.NewEncoder(w).Encode(gists)
	})

	// GET /repos/{owner}/{repo}/releases for releases
	mux.HandleFunc("/repos/octocat/Hello-World/releases", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
		releases := []releaseItem{{
			TagName:     "v1.0.0",
			Name:        "Release 1.0.0",
			HTMLURL:     "https://github.com/octocat/Hello-World/releases/tag/v1.0.0",
			PublishedAt: base,
		}}
		_ = json.NewEncoder(w).Encode(releases)
	})

	// GET /search/commits for top projects' commit activity
	mux.HandleFunc("/search/commits", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		result := map[string]any{
			"total_count": 1,
			"items": []map[string]any{
				{
					"sha":        "abc123",
					"repository": map[string]any{"full_name": "example-org/example-repo"},
				},
			},
		}
		_ = json.NewEncoder(w).Encode(result)
	})

	// Feed endpoint (simple RSS)
	mux.HandleFunc("/feed", func(w http.ResponseWriter, r *http.Request) {
		s.requestedPaths = append(s.requestedPaths, r.URL.Path)
		w.Header().Set("Content-Type", "application/rss+xml")
		base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
		body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Test Feed</title>
<item>
<title>Hello</title>
<link>https://example.com/hello</link>
<description>a post</description>
<pubDate>%s</pubDate>
</item>
</channel></rss>`, base.Format(time.RFC1123Z))
		_, _ = w.Write([]byte(body))
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// TestFetchAllSectionsPresent verifies the orchestrator calls all fetchers when
// all sections are configured and assembles the results into model.Data.
func TestFetchAllSectionsPresent(t *testing.T) {
	srv := newOrchestratorServer(t)
	c := testClient(t, srv.URL)

	cfg := &config.Config{
		Username: "octocat",
		GitHub: config.GitHub{
			Contributions: &config.Section{Limit: 10},
			Repos:         &config.Section{Limit: 10},
			Forks:         &config.Section{Limit: 10},
			PullRequests:  &config.Section{Limit: 10},
			Stars:         &config.Section{Limit: 10},
			Releases:      &config.Section{Limit: 10},
			Followers:     &config.Section{Limit: 10},
			Gists:         &config.Section{Limit: 10},
			TopProjects:   &config.Section{Limit: 10, TimeWindow: "30d"},
		},
		Feeds: map[string]config.Feed{
			"blog": {URL: srv.URL + "/feed", Limit: 5},
		},
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify result is not nil and has the expected username
	if got == nil {
		t.Fatal("got nil Data, want non-nil")
	}
	if got.User.Login != "octocat" {
		t.Errorf("User.Login = %q, want %q", got.User.Login, "octocat")
	}

	// Verify GitHub sections were populated
	if len(got.GitHub.Contributions) != 1 {
		t.Errorf("GitHub.Contributions length = %d, want 1", len(got.GitHub.Contributions))
	}
	if len(got.GitHub.Repos) != 1 {
		t.Errorf("GitHub.Repos length = %d, want 1", len(got.GitHub.Repos))
	}
	if len(got.GitHub.Stars) != 1 {
		t.Errorf("GitHub.Stars length = %d, want 1", len(got.GitHub.Stars))
	}
	if len(got.GitHub.Releases) != 1 {
		t.Errorf("GitHub.Releases length = %d, want 1", len(got.GitHub.Releases))
	}
	if len(got.GitHub.Followers) != 1 {
		t.Errorf("GitHub.Followers length = %d, want 1", len(got.GitHub.Followers))
	}
	if len(got.GitHub.Gists) != 1 {
		t.Errorf("GitHub.Gists length = %d, want 1", len(got.GitHub.Gists))
	}
	if len(got.GitHub.TopProjects) != 1 {
		t.Errorf("GitHub.TopProjects length = %d, want 1", len(got.GitHub.TopProjects))
	}

	// Verify feeds were populated
	if got.Feeds == nil {
		t.Error("got Feeds = nil, want non-nil map")
	} else if len(got.Feeds["blog"]) != 1 {
		t.Errorf("Feeds[blog] length = %d, want 1", len(got.Feeds["blog"]))
	}

	// Verify Now is approximately current
	if got.Now.IsZero() {
		t.Error("Now is zero, want current time")
	}
	if got.Now.After(time.Now().Add(time.Second)) {
		t.Error("Now is in the future")
	}
}

// TestFetchSkipsAbsentSections verifies the orchestrator only calls fetchers for
// configured sections and does not call fetchers for nil sections.
func TestFetchSkipsAbsentSections(t *testing.T) {
	srv := newOrchestratorServer(t)
	c := testClient(t, srv.URL)

	cfg := &config.Config{
		Username: "octocat",
		GitHub: config.GitHub{
			Contributions: &config.Section{Limit: 10},
			// Repos, Forks, PullRequests are nil (not configured)
		},
		// Feeds is nil
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	// Verify contributions were fetched
	if len(got.GitHub.Contributions) != 1 {
		t.Errorf("GitHub.Contributions length = %d, want 1", len(got.GitHub.Contributions))
	}

	// Verify other sections are empty/nil
	if got.GitHub.Repos != nil {
		t.Errorf("GitHub.Repos = %+v, want nil (not configured)", got.GitHub.Repos)
	}
	if got.GitHub.Forks != nil {
		t.Errorf("GitHub.Forks = %+v, want nil (not configured)", got.GitHub.Forks)
	}
	if got.GitHub.PullRequests != nil {
		t.Errorf("GitHub.PullRequests = %+v, want nil (not configured)", got.GitHub.PullRequests)
	}
	if got.GitHub.Stars != nil {
		t.Errorf("GitHub.Stars = %+v, want nil (not configured)", got.GitHub.Stars)
	}
	if got.GitHub.Releases != nil {
		t.Errorf("GitHub.Releases = %+v, want nil (not configured)", got.GitHub.Releases)
	}
	if got.GitHub.Followers != nil {
		t.Errorf("GitHub.Followers = %+v, want nil (not configured)", got.GitHub.Followers)
	}
	if got.GitHub.Gists != nil {
		t.Errorf("GitHub.Gists = %+v, want nil (not configured)", got.GitHub.Gists)
	}
	if got.GitHub.TopProjects != nil {
		t.Errorf("GitHub.TopProjects = %+v, want nil (not configured)", got.GitHub.TopProjects)
	}
	if got.Feeds != nil {
		t.Errorf("Feeds = %+v, want nil (not configured)", got.Feeds)
	}

	// Verify only the configured endpoint was called.
	// Only contributions should be fetched; repos, pull requests, and feeds should not be called.
	wantPaths := []string{"/users/octocat/events/public"}
	if len(srv.requestedPaths) != len(wantPaths) {
		t.Errorf("requestedPaths length = %d, want %d; got %v", len(srv.requestedPaths), len(wantPaths), srv.requestedPaths)
	} else {
		for i, got := range srv.requestedPaths {
			if got != wantPaths[i] {
				t.Errorf("requestedPaths[%d] = %q, want %q", i, got, wantPaths[i])
			}
		}
	}
}

// TestFetchNoSections verifies the orchestrator returns valid but empty data when
// no sections are configured.
func TestFetchNoSections(t *testing.T) {
	srv := newOrchestratorServer(t)
	c := testClient(t, srv.URL)

	cfg := &config.Config{
		Username: "octocat",
		// All sections nil
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if got == nil {
		t.Fatal("got nil Data, want non-nil")
	}
	if got.User.Login != "octocat" {
		t.Errorf("User.Login = %q, want %q", got.User.Login, "octocat")
	}
	if got.Feeds != nil {
		t.Errorf("Feeds = %+v, want nil", got.Feeds)
	}

	// Verify no endpoints were called when no sections are configured.
	if len(srv.requestedPaths) != 0 {
		t.Errorf("requestedPaths length = %d, want 0; got %v", len(srv.requestedPaths), srv.requestedPaths)
	}
}

// TestFetchWiresTopProjectsWhenConfiguredAlone verifies TopProjects is
// fetched on its own (Contributions absent), proving it reuses the events
// timeline via its own call rather than depending on Contributions having
// already run in the same Fetch.
func TestFetchWiresTopProjectsWhenConfiguredAlone(t *testing.T) {
	srv := newOrchestratorServer(t)
	c := testClient(t, srv.URL)

	cfg := &config.Config{
		Username: "octocat",
		GitHub: config.GitHub{
			TopProjects: &config.Section{Limit: 10, TimeWindow: "30d"},
		},
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got.GitHub.TopProjects) != 1 {
		t.Errorf("GitHub.TopProjects length = %d, want 1", len(got.GitHub.TopProjects))
	}

	wantAny := map[string]bool{"/search/commits": false, "/search/issues": false, "/users/octocat/events/public": false}
	for _, p := range srv.requestedPaths {
		if _, ok := wantAny[p]; ok {
			wantAny[p] = true
		}
	}
	for path, called := range wantAny {
		if !called {
			t.Errorf("%s was never called for a TopProjects-only config", path)
		}
	}
}

// TestFetchReturnsErrorFromContributions verifies that if any fetcher errors,
// the orchestrator returns that error and stops.
func TestFetchReturnsErrorFromContributions(t *testing.T) {
	s := &orchestratorServer{}
	mux := http.NewServeMux()
	// Error on contributions
	mux.HandleFunc("/users/octocat/events/public", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)

	c := testClient(t, s.URL)

	cfg := &config.Config{
		Username: "octocat",
		GitHub: config.GitHub{
			Contributions: &config.Section{Limit: 10},
		},
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err == nil {
		t.Fatal("Fetch: want error from contributions, got nil")
	}
	if got != nil {
		t.Errorf("got non-nil Data alongside error: %+v", got)
	}
}

// TestFetchReturnsFeedsNilWhenNoFeedsConfigured verifies that when no feeds are
// configured, the Feeds field is nil rather than an empty map.
func TestFetchReturnsFeedsNilWhenNoFeedsConfigured(t *testing.T) {
	srv := newOrchestratorServer(t)
	c := testClient(t, srv.URL)

	cfg := &config.Config{
		Username: "octocat",
		// Feeds is nil
	}

	got, err := Fetch(context.Background(), c, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if got.Feeds != nil {
		t.Errorf("Feeds = %+v, want nil (no feeds configured)", got.Feeds)
	}

	// Verify the feed endpoint was not called when no feeds are configured.
	for _, path := range srv.requestedPaths {
		if path == "/feed" {
			t.Errorf("feed endpoint %q was called when no feeds were configured", path)
		}
	}
}
