package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

type releaseItem struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
}

type releasesTestServer struct {
	*httptest.Server
	reposQueries    []string
	releasesQueries map[string][]string
}

func newReleasesTestServer(t *testing.T, repos []repoItem, releasesByRepo map[string][][]releaseItem, status int) *releasesTestServer {
	t.Helper()

	s := &releasesTestServer{
		releasesQueries: make(map[string][]string),
	}

	mux := http.NewServeMux()

	// Handle /users/{u}/repos
	mux.HandleFunc("/users/octocat/repos", func(w http.ResponseWriter, r *http.Request) {
		s.reposQueries = append(s.reposQueries, r.URL.RawQuery)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		w.Header().Set("Content-Type", "application/json")
		if page != 1 {
			_ = json.NewEncoder(w).Encode([]repoItem{})
			return
		}
		_ = json.NewEncoder(w).Encode(repos)
	})

	// Handle /repos/{owner}/{repo}/releases
	mux.HandleFunc("/repos/octocat/", func(w http.ResponseWriter, r *http.Request) {
		repoName := r.URL.Path[len("/repos/octocat/"):]
		if len(repoName) > len("/releases") && repoName[len(repoName)-len("/releases"):] == "/releases" {
			repoName = repoName[:len(repoName)-len("/releases")]
			key := "octocat/" + repoName
			s.releasesQueries[key] = append(s.releasesQueries[key], r.URL.RawQuery)

			if status != 0 {
				w.WriteHeader(status)
				return
			}

			pages, ok := releasesByRepo[key]
			if !ok {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode([]releaseItem{})
				return
			}

			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				fmt.Sscanf(p, "%d", &page)
			}
			w.Header().Set("Content-Type", "application/json")
			if page < 1 || page > len(pages) {
				_ = json.NewEncoder(w).Encode([]releaseItem{})
				return
			}
			_ = json.NewEncoder(w).Encode(pages[page-1])
		}
	})

	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func releaseConfig(limit int) config.GitHub {
	return config.GitHub{Releases: &config.Section{Limit: limit}}
}

func TestReleasesNilSectionMakesNoRequest(t *testing.T) {
	srv := newReleasesTestServer(t, nil, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Releases(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.reposQueries) != 0 {
		t.Errorf("observed %d repos requests, want 0", len(srv.reposQueries))
	}
}

func TestReleasesZeroLimitMakesNoRequest(t *testing.T) {
	srv := newReleasesTestServer(t, nil, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Releases(context.Background(), c, "octocat", releaseConfig(0))
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.reposQueries) != 0 {
		t.Errorf("observed %d repos requests, want 0", len(srv.reposQueries))
	}
}

func TestReleasesPopulatesEveryModelField(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	published := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	repos := []repoItem{{
		Name:     "my-app",
		FullName: "octocat/my-app",
		PushedAt: pushed,
	}}
	releasesByRepo := map[string][][]releaseItem{
		"octocat/my-app": {{{
			TagName:     "v1.0.0",
			Name:        "Release 1.0.0",
			HTMLURL:     "https://github.com/octocat/my-app/releases/tag/v1.0.0",
			PublishedAt: published,
		}}},
	}
	srv := newReleasesTestServer(t, repos, releasesByRepo, 0)
	c := testClient(t, srv.URL)

	got, err := Releases(context.Background(), c, "octocat", releaseConfig(10))
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}

	want := model.Release{
		Repo:        "octocat/my-app",
		TagName:     "v1.0.0",
		Name:        "Release 1.0.0",
		URL:         "https://github.com/octocat/my-app/releases/tag/v1.0.0",
		PublishedAt: published,
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReleasesStopsAtLimit(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	repos := []repoItem{
		{Name: "app1", FullName: "octocat/app1", PushedAt: pushed},
		{Name: "app2", FullName: "octocat/app2", PushedAt: pushed},
	}
	releasesByRepo := map[string][][]releaseItem{
		"octocat/app1": {{{
			TagName:     "v1.0.0",
			Name:        "Release 1",
			HTMLURL:     "https://github.com/octocat/app1/releases/tag/v1.0.0",
			PublishedAt: published,
		}}},
		"octocat/app2": {{{
			TagName:     "v2.0.0",
			Name:        "Release 2",
			HTMLURL:     "https://github.com/octocat/app2/releases/tag/v2.0.0",
			PublishedAt: published,
		}}},
	}
	srv := newReleasesTestServer(t, repos, releasesByRepo, 0)
	c := testClient(t, srv.URL)

	got, err := Releases(context.Background(), c, "octocat", releaseConfig(1))
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d releases, want 1", len(got))
	}
	// Should have stopped after getting 1 release from app1, without checking app2
	if _, ok := srv.releasesQueries["octocat/app2"]; ok {
		t.Errorf("checked octocat/app2 releases, but should have stopped after reaching limit")
	}
}

func TestReleasesReturnsErrorFromReposList(t *testing.T) {
	srv := newReleasesTestServer(t, nil, nil, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := Releases(context.Background(), c, "octocat", releaseConfig(10))
	if err == nil {
		t.Fatal("Releases: want error on non-200 response from repos list, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

func TestReleasesBoundsScanToMaxRepos(t *testing.T) {
	// Create 30 repos with releases ONLY beyond maxReleasesReposScan.
	// This ensures the cap prevents finding the limit, not that the limit is reached first.
	// With maxReleasesReposScan = 10, we scan only repos 0-9, which have no releases.
	// Repos 15-20 have releases but are unreachable due to the cap, proving the cap is enforced.
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)

	// Create repos sorted by pushed_at descending (most recently active first).
	repos := make([]repoItem, 30)
	for i := 0; i < 30; i++ {
		repos[i] = repoItem{
			Name:     fmt.Sprintf("repo%d", i),
			FullName: fmt.Sprintf("octocat/repo%d", i),
			PushedAt: pushed.Add(time.Duration(30-i) * time.Hour), // Descending: repo0 most recent
		}
	}
	// Simulate GitHub's "pushed" sort (descending by pushed_at).
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].PushedAt.After(repos[j].PushedAt)
	})

	// Place releases ONLY on repos 15-20 (beyond the cap).
	// Repos 0-14 have no releases; repos 15-20 have releases but are unreachable due to maxReleasesReposScan.
	releasesByRepo := make(map[string][][]releaseItem)
	for i := 15; i <= 20; i++ {
		releasesByRepo[fmt.Sprintf("octocat/repo%d", i)] = [][]releaseItem{{{
			TagName:     fmt.Sprintf("v%d.0.0", i),
			Name:        fmt.Sprintf("Release %d", i),
			HTMLURL:     fmt.Sprintf("https://github.com/octocat/repo%d/releases/tag/v%d.0.0", i, i),
			PublishedAt: published,
		}}}
	}

	srv := newReleasesTestServer(t, repos, releasesByRepo, 0)
	c := testClient(t, srv.URL)

	// Request 3 releases with maxReleasesReposScan = 10.
	// Should find 0 releases because all scanned repos (0-9) have no releases.
	// Repos with releases (15-20) are beyond the cap and never queried.
	got, err := Releases(context.Background(), c, "octocat", releaseConfig(3))
	if err != nil {
		t.Fatalf("Releases: %v", err)
	}

	// Assert that we got fewer releases than requested (proves the cap stopped us before satisfying the limit).
	if len(got) >= 3 {
		t.Errorf("got %d releases, want fewer than 3 due to maxReleasesReposScan cap", len(got))
	}

	// Verify request count bound: 1 (list-repos) + at most maxReleasesReposScan (release requests).
	// We scan all 10 repos but find no releases, so expect 1 + 10 = 11 requests max.
	totalRequests := len(srv.reposQueries) + len(srv.releasesQueries)
	maxAllowedRequests := 1 + maxReleasesReposScan
	if totalRequests > maxAllowedRequests {
		t.Errorf("made %d requests, want at most %d (1 list-repos + %d release scans)",
			totalRequests, maxAllowedRequests, maxReleasesReposScan)
	}

	// Verify no repos beyond maxReleasesReposScan were scanned.
	for i := maxReleasesReposScan; i < 30; i++ {
		key := fmt.Sprintf("octocat/repo%d", i)
		if _, ok := srv.releasesQueries[key]; ok {
			t.Errorf("scanned repo %s, but should stop at maxReleasesReposScan=%d",
				key, maxReleasesReposScan)
		}
	}
}
