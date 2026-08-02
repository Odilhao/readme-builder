package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// testClient builds a Client redirected at baseURL, shared by repos_test.go
// and pullrequests_test.go.
func testClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := New(Options{BaseURL: baseURL + "/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// repoItem is the minimal shape of a GET /users/{u}/repos entry, as fixtures
// need it.
type repoItem struct {
	Name            string    `json:"name"`
	FullName        string    `json:"full_name"`
	Description     string    `json:"description"`
	HTMLURL         string    `json:"html_url"`
	Language        string    `json:"language"`
	StargazersCount int       `json:"stargazers_count"`
	PushedAt        time.Time `json:"pushed_at"`
	Fork            bool      `json:"fork"`
}

// reposServer serves /users/{u}/repos from a fixed set of pages, recording
// every request's raw query string.
type reposServer struct {
	*httptest.Server

	queries []string
}

func newReposServer(t *testing.T, pages [][]repoItem, status int) *reposServer {
	t.Helper()
	return newReposServerForUser(t, "octocat", pages, status)
}

// newReposServerForUser is newReposServer with the path's username as a
// parameter, so tests can prove the caller-supplied user is threaded through
// rather than a fixed one: a server only listening on octocat's path would
// 404 a request sent to any other user.
func newReposServerForUser(t *testing.T, user string, pages [][]repoItem, status int) *reposServer {
	t.Helper()

	s := &reposServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+user+"/repos", func(w http.ResponseWriter, r *http.Request) {
		s.queries = append(s.queries, r.URL.RawQuery)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		w.Header().Set("Content-Type", "application/json")
		if page < 1 || page > len(pages) {
			_ = json.NewEncoder(w).Encode([]repoItem{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func repoConfig(limit int) config.GitHub {
	return config.GitHub{Repos: &config.Section{Limit: limit}}
}

func forkConfig(limit int) config.GitHub {
	return config.GitHub{Forks: &config.Section{Limit: limit}}
}

func TestReposNilSectionMakesNoRequest(t *testing.T) {
	srv := newReposServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestReposZeroLimitMakesNoRequest(t *testing.T) {
	srv := newReposServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(0))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestForksNilSectionMakesNoRequest(t *testing.T) {
	srv := newReposServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Forks(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Forks: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestReposFiltersOutForksAndKeepsOwnRepos(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{
		{Name: "Hello-World", FullName: "octocat/Hello-World", Fork: false, PushedAt: pushed},
		{Name: "a-fork", FullName: "octocat/a-fork", Fork: true, PushedAt: pushed},
	}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(10))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "octocat/Hello-World" {
		t.Errorf("got %+v, want only octocat/Hello-World", got)
	}
}

func TestForksFiltersToForksOnly(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{
		{Name: "Hello-World", FullName: "octocat/Hello-World", Fork: false, PushedAt: pushed},
		{Name: "a-fork", FullName: "octocat/a-fork", Fork: true, PushedAt: pushed},
	}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Forks(context.Background(), c, "octocat", forkConfig(10))
	if err != nil {
		t.Fatalf("Forks: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "octocat/a-fork" {
		t.Errorf("got %+v, want only octocat/a-fork", got)
	}
}

// TestReposPopulatesEveryModelField pins the full model.Repo shape, not just
// key presence: a wrong field mapping (e.g. Language into Description) would
// still leave every key present but fail this comparison.
func TestReposPopulatesEveryModelField(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	pages := [][]repoItem{{{
		Name:            "Hello-World",
		FullName:        "octocat/Hello-World",
		Description:     "my first repo",
		HTMLURL:         "https://github.com/octocat/Hello-World",
		Language:        "Go",
		StargazersCount: 42,
		PushedAt:        pushed,
		Fork:            false,
	}}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(10))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}

	want := model.Repo{
		Name:        "Hello-World",
		FullName:    "octocat/Hello-World",
		Description: "my first repo",
		URL:         "https://github.com/octocat/Hello-World",
		Language:    "Go",
		Stars:       42,
		PushedAt:    pushed,
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestReposAppliesNameExclusions(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{
		{Name: "excluded-repo", FullName: "excluded-org/excluded-repo", PushedAt: pushed},
		{Name: "example-repo", FullName: "example-org/example-repo", PushedAt: pushed},
	}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	gh := repoConfig(10)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := Repos(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo", got)
	}
}

// TestReposStopsAtLimitWithoutFetchingExtraPages uses a full-length first
// page so only the `len(out) < limit` condition, not a short-page break, can
// stop the loop: a fixture whose first page is already short would pass even
// if the limit check were relaxed to `<=`.
func TestReposStopsAtLimitWithoutFetchingExtraPages(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{
		genRepoItems(reposPerPage, "r", pushed),
		{{Name: "r-extra", FullName: "example-org/r-extra", PushedAt: pushed}},
	}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(1))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d repos, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (must stop once Limit is reached)", len(srv.queries))
	}
}

func TestReposStopsPaginatingOnShortPage(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{{Name: "only", FullName: "example-org/only", PushedAt: pushed}}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(10))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d repos, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (short page must stop pagination)", len(srv.queries))
	}
}

func genRepoItems(n int, namePrefix string, pushed time.Time) []repoItem {
	items := make([]repoItem, n)
	for i := range items {
		items[i] = repoItem{
			Name:     fmt.Sprintf("%s-%d", namePrefix, i),
			FullName: fmt.Sprintf("example-org/%s-%d", namePrefix, i),
			PushedAt: pushed,
		}
	}
	return items
}

// TestReposContinuesPastAFullFirstPage proves a full-length first page is not
// mistaken for the last one: only a short page may stop pagination.
func TestReposContinuesPastAFullFirstPage(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{
		genRepoItems(reposPerPage, "repo", pushed),
		{{Name: "last", FullName: "example-org/last", PushedAt: pushed}},
	}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(reposPerPage+1))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != reposPerPage+1 {
		t.Fatalf("got %d repos, want %d", len(got), reposPerPage+1)
	}
	if len(srv.queries) != 2 {
		t.Errorf("observed %d requests, want 2 (a full page must not be treated as the last one)", len(srv.queries))
	}
}

// TestReposRequestsUseDocumentedPerPage pins the literal per_page, type and
// sort values sent on the wire, not merely the request count: a wrong
// constant would still pass a count-only assertion. type=owner and
// sort=created are the literal query the issue names for
// GET /users/{u}/repos - type=all would return org repos the user merely
// belongs to, a real behavior change.
func TestReposRequestsUseDocumentedPerPage(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{{Name: "only", FullName: "example-org/only", PushedAt: pushed}}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	if _, err := Repos(context.Background(), c, "octocat", repoConfig(10)); err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(srv.queries) != 1 {
		t.Fatalf("observed %d requests, want 1", len(srv.queries))
	}
	q, err := url.ParseQuery(srv.queries[0])
	if err != nil {
		t.Fatalf("url.ParseQuery(%q): %v", srv.queries[0], err)
	}
	if got, want := q.Get("per_page"), "100"; got != want {
		t.Errorf("per_page = %q, want %q", got, want)
	}
	if got, want := q.Get("type"), "owner"; got != want {
		t.Errorf("type = %q, want %q", got, want)
	}
	if got, want := q.Get("sort"), "created"; got != want {
		t.Errorf("sort = %q, want %q", got, want)
	}
}

// TestReposRequestsAdvancePageAcrossCalls pins the literal page number sent
// on the wire across two paginated requests: mutating the initial Page to 0
// or dropping opts.Page++ both leave the request count unchanged, so only an
// assertion on the page value itself catches them.
func TestReposRequestsAdvancePageAcrossCalls(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{
		genRepoItems(reposPerPage, "repo", pushed),
		{{Name: "last", FullName: "example-org/last", PushedAt: pushed}},
	}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	if _, err := Repos(context.Background(), c, "octocat", repoConfig(reposPerPage+1)); err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(srv.queries) != 2 {
		t.Fatalf("observed %d requests, want 2", len(srv.queries))
	}
	for i, want := range []string{"1", "2"} {
		q, err := url.ParseQuery(srv.queries[i])
		if err != nil {
			t.Fatalf("url.ParseQuery(%q): %v", srv.queries[i], err)
		}
		if got := q.Get("page"); got != want {
			t.Errorf("request %d: page = %q, want %q", i, got, want)
		}
	}
}

// TestReposUsesCallerSuppliedUsername proves Repos threads the user argument
// through to GET /users/{u}/repos rather than a fixed one: a server that only
// answers for "example-user" would 404 (and yield zero results) if the call
// silently used a different username.
func TestReposUsesCallerSuppliedUsername(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{{Name: "only", FullName: "example-user/only", PushedAt: pushed}}}
	srv := newReposServerForUser(t, "example-user", pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "example-user", repoConfig(10))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "example-user/only" {
		t.Errorf("got %+v, want only example-user/only", got)
	}
}

// TestForksUsesCallerSuppliedUsername is TestReposUsesCallerSuppliedUsername
// for Forks.
func TestForksUsesCallerSuppliedUsername(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{{Name: "only", FullName: "example-user/only", Fork: true, PushedAt: pushed}}}
	srv := newReposServerForUser(t, "example-user", pages, 0)
	c := testClient(t, srv.URL)

	got, err := Forks(context.Background(), c, "example-user", forkConfig(10))
	if err != nil {
		t.Fatalf("Forks: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "example-user/only" {
		t.Errorf("got %+v, want only example-user/only", got)
	}
}

// TestReposExcludesSelfRepoUnconditionally proves the special GitHub profile
// repo ({user}/{user}) is always dropped from Repos - no config field
// controls it.
func TestReposExcludesSelfRepoUnconditionally(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{
		{Name: "octocat", FullName: "octocat/octocat", PushedAt: pushed},
		{Name: "Hello-World", FullName: "octocat/Hello-World", PushedAt: pushed},
	}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(10))
	if err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "octocat/Hello-World" {
		t.Errorf("got %+v, want only octocat/Hello-World (self-repo must be excluded, unconditionally, no config field)", got)
	}
}

// TestForksExcludesSelfRepoUnconditionally is
// TestReposExcludesSelfRepoUnconditionally for Forks - the self-repo is never
// a fork in practice, but the exclusion must not depend on that.
func TestForksExcludesSelfRepoUnconditionally(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]repoItem{{
		{Name: "octocat", FullName: "octocat/octocat", Fork: true, PushedAt: pushed},
		{Name: "a-fork", FullName: "octocat/a-fork", Fork: true, PushedAt: pushed},
	}}
	srv := newReposServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Forks(context.Background(), c, "octocat", forkConfig(10))
	if err != nil {
		t.Fatalf("Forks: %v", err)
	}
	if len(got) != 1 || got[0].FullName != "octocat/a-fork" {
		t.Errorf("got %+v, want only octocat/a-fork (self-repo must be excluded even among forks)", got)
	}
}

// TestReposMakesNoAuthorizationHeaderWithoutToken exercises a high-level
// fetcher end-to-end (not just the shared client) to prove the no-token
// guarantee holds all the way through Repos: no Authorization header is sent
// when Options.Token is empty.
func TestReposMakesNoAuthorizationHeaderWithoutToken(t *testing.T) {
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	seen := false
	var gotAuth string

	mux := http.NewServeMux()
	mux.HandleFunc("/users/octocat/repos", func(w http.ResponseWriter, r *http.Request) {
		seen = true
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]repoItem{
			{Name: "Hello-World", FullName: "octocat/Hello-World", PushedAt: pushed},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := New(Options{BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if _, err := Repos(context.Background(), c, "octocat", repoConfig(10)); err != nil {
		t.Fatalf("Repos: %v", err)
	}
	if !seen {
		t.Fatal("request never reached the server")
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (no token configured)", gotAuth)
	}
}

func TestReposReturnsErrorFromList(t *testing.T) {
	srv := newReposServer(t, nil, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := Repos(context.Background(), c, "octocat", repoConfig(10))
	if err == nil {
		t.Fatal("Repos: want error on non-200 response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}
