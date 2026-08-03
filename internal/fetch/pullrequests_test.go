package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// searchIssue is the minimal shape of a /search/issues result item, as
// fixtures need it. PullRequest is non-nil to mark the item as a pull
// request, matching go-github's Issue.IsPullRequest check.
type searchIssue struct {
	Number        int             `json:"number"`
	Title         string          `json:"title"`
	HTMLURL       string          `json:"html_url"`
	State         string          `json:"state"`
	RepositoryURL string          `json:"repository_url"`
	PullRequest   json.RawMessage `json:"pull_request,omitempty"`
}

func pr(number int, title, repoFullName string) searchIssue {
	return prWithState(number, title, repoFullName, "open")
}

// prWithState builds a pull-request search result item with an explicit
// state, so tests can pin State to something other than the "open" every
// other fixture defaults to.
func prWithState(number int, title, repoFullName, state string) searchIssue {
	return searchIssue{
		Number:        number,
		Title:         title,
		HTMLURL:       fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, number),
		State:         state,
		RepositoryURL: "https://api.github.com/repos/" + repoFullName,
		PullRequest:   json.RawMessage(`{}`),
	}
}

// issueNotPR builds a search result item that is a plain issue, not a pull
// request (no "pull_request" key at all), to exercise the defensive filter.
func issueNotPR(number int, title, repoFullName string) searchIssue {
	return searchIssue{
		Number:        number,
		Title:         title,
		HTMLURL:       fmt.Sprintf("https://github.com/%s/issues/%d", repoFullName, number),
		State:         "open",
		RepositoryURL: "https://api.github.com/repos/" + repoFullName,
	}
}

// searchServer serves /search/issues from a set of pages,
// recording every request's raw query.
type searchServer struct {
	*httptest.Server

	queries []url.Values
	status  int
	pages   [][]searchIssue
}

func newSearchServer(t *testing.T, items []searchIssue, status int) *searchServer {
	t.Helper()
	return newSearchServerPages(t, [][]searchIssue{items}, status)
}

// newSearchServerPages serves /search/issues from multiple pages, supporting pagination tests.
func newSearchServerPages(t *testing.T, pages [][]searchIssue, status int) *searchServer {
	t.Helper()

	s := &searchServer{status: status, pages: pages}
	mux := http.NewServeMux()
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		s.queries = append(s.queries, r.URL.Query())
		if s.status != 0 {
			w.WriteHeader(s.status)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		w.Header().Set("Content-Type", "application/json")
		if page < 1 || page > len(s.pages) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"total_count":        0,
				"incomplete_results": false,
				"items":              []searchIssue{},
			})
			return
		}
		items := s.pages[page-1]
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        len(items),
			"incomplete_results": false,
			"items":              items,
		})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func prConfig(limit int) config.GitHub {
	return config.GitHub{PullRequests: &config.Section{Limit: limit}}
}

func TestPullRequestsNilSectionMakesNoRequest(t *testing.T) {
	srv := newSearchServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestPullRequestsZeroLimitMakesNoRequest(t *testing.T) {
	srv := newSearchServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(0))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestPullRequestsIssuesExactlyOneSearchCall(t *testing.T) {
	items := []searchIssue{
		pr(1, "one", "example-org/example-repo"),
		pr(2, "two", "example-org/example-repo"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	if _, err := PullRequests(context.Background(), c, "octocat", prConfig(10)); err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d /search/issues requests, want exactly 1", len(srv.queries))
	}
}

// TestPullRequestsSearchQueryIsAuthorScoped pins the literal query and sort
// values sent to /search/issues.
func TestPullRequestsSearchQueryIsAuthorScoped(t *testing.T) {
	srv := newSearchServer(t, nil, 0)
	c := testClient(t, srv.URL)

	if _, err := PullRequests(context.Background(), c, "octocat", prConfig(5)); err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(srv.queries) != 1 {
		t.Fatalf("observed %d requests, want 1", len(srv.queries))
	}
	q := srv.queries[0]
	if got, want := q.Get("q"), "is:pr author:octocat"; got != want {
		t.Errorf("q = %q, want %q", got, want)
	}
	if got, want := q.Get("sort"), "created"; got != want {
		t.Errorf("sort = %q, want %q", got, want)
	}
	if got, want := q.Get("order"), "desc"; got != want {
		t.Errorf("order = %q, want %q", got, want)
	}
}

// TestPullRequestsSearchQueryUsesCallerSuppliedUsername proves the search
// query's author scope comes from the caller-supplied user argument rather
// than a fixed one: calling with a second, different username must produce a
// different "author:" term.
func TestPullRequestsSearchQueryUsesCallerSuppliedUsername(t *testing.T) {
	srv := newSearchServer(t, nil, 0)
	c := testClient(t, srv.URL)

	if _, err := PullRequests(context.Background(), c, "example-user", prConfig(5)); err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(srv.queries) != 1 {
		t.Fatalf("observed %d requests, want 1", len(srv.queries))
	}
	if got, want := srv.queries[0].Get("q"), "is:pr author:example-user"; got != want {
		t.Errorf("q = %q, want %q", got, want)
	}
}

// TestPullRequestsOverfetchesPerPageByHardcodedMultiplier pins per_page to
// exactly Limit*2 (the hardcoded, non-config-driven multiplier), capped at
// the search API's own 100 ceiling.
func TestPullRequestsOverfetchesPerPageByHardcodedMultiplier(t *testing.T) {
	tests := map[string]struct {
		limit int
		want  string
	}{
		"under cap": {limit: 5, want: "10"},
		"at cap":    {limit: 60, want: "100"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			srv := newSearchServer(t, nil, 0)
			c := testClient(t, srv.URL)

			if _, err := PullRequests(context.Background(), c, "octocat", prConfig(tt.limit)); err != nil {
				t.Fatalf("PullRequests: %v", err)
			}
			if len(srv.queries) != 1 {
				t.Fatalf("observed %d requests, want 1", len(srv.queries))
			}
			if got := srv.queries[0].Get("per_page"); got != tt.want {
				t.Errorf("per_page = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestPullRequestsNeverCallsRepoDetailEndpoint verifies the repository full
// name is parsed from the search result's own repository_url, never fetched
// via a second GET /repos call - the exact per-item budget spend the issue
// rules out.
func TestPullRequestsNeverCallsRepoDetailEndpoint(t *testing.T) {
	items := []searchIssue{pr(1, "one", "example-org/example-repo")}

	mux := http.NewServeMux()
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        len(items),
			"incomplete_results": false,
			"items":              items,
		})
	})
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s: pullrequests.go must never call GET /repos", r.URL.Path)
		w.WriteHeader(http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := testClient(t, srv.URL)
	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want one PR for example-org/example-repo", got)
	}
}

// TestPullRequestsFiltersExcludedRepoAfterSearch proves exclusion is applied
// to the single search result rather than by issuing a second, narrower
// search: the request count must stay at 1 even though the excluded repo's
// PR is dropped from the output.
func TestPullRequestsFiltersExcludedRepoAfterSearch(t *testing.T) {
	items := []searchIssue{
		pr(1, "excluded", "excluded-org/excluded-repo"),
		pr(2, "kept", "example-org/example-repo"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	gh := prConfig(10)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := PullRequests(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo", got)
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d /search/issues requests, want exactly 1 even after post-filtering", len(srv.queries))
	}
}

// TestPullRequestsSkipsNonPullRequestIssues exercises the defensive
// is-PR check: /search/issues can return plain issues alongside PRs.
func TestPullRequestsSkipsNonPullRequestIssues(t *testing.T) {
	items := []searchIssue{
		issueNotPR(1, "plain issue", "example-org/example-repo"),
		pr(2, "a pr", "example-org/example-repo"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 1 || got[0].Number != 2 {
		t.Errorf("got %+v, want only the pull request (number 2)", got)
	}
}

// TestPullRequestsTruncatesToLimitWithoutResorting proves truncation happens
// in search order (already sort=created&order=desc) rather than by
// re-sorting locally: a result set that arrives out of chronological order
// (as far as this fetcher can tell, since it never re-sorts) is trusted as-is
// and only cut to Limit.
func TestPullRequestsTruncatesToLimitWithoutResorting(t *testing.T) {
	items := []searchIssue{
		pr(1, "first", "example-org/repo-a"),
		pr(2, "second", "example-org/repo-b"),
		pr(3, "third", "example-org/repo-c"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(2))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 2 || got[0].Number != 1 || got[1].Number != 2 {
		t.Errorf("got %+v, want the first two results in search order", got)
	}
}

// TestPullRequestsPopulatesEveryModelFieldExceptMergedAt pins the full
// model.PullRequest shape, including a closed PR alongside the open one so
// State is read from the wire rather than indistinguishable from a
// hardcoded "open" literal. MergedAt is deliberately left at its zero value:
// /search/issues doesn't carry it, and a second per-PR call to fetch it would
// spend exactly the budget the issue rules out.
func TestPullRequestsPopulatesEveryModelFieldExceptMergedAt(t *testing.T) {
	items := []searchIssue{
		pr(7, "add feature", "example-org/example-repo"),
		prWithState(8, "fix bug", "example-org/example-repo", "closed"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}

	want := []model.PullRequest{
		{
			Repo:   "example-org/example-repo",
			Number: 7,
			Title:  "add feature",
			URL:    "https://github.com/example-org/example-repo/pull/7",
			State:  "open",
		},
		{
			Repo:   "example-org/example-repo",
			Number: 8,
			Title:  "fix bug",
			URL:    "https://github.com/example-org/example-repo/pull/8",
			State:  "closed",
		},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !got[0].MergedAt.IsZero() || !got[1].MergedAt.IsZero() {
		t.Errorf("MergedAt = %v / %v, want zero values", got[0].MergedAt, got[1].MergedAt)
	}
}

// TestRepoFullNameFromURLReturnsEmptyOnMalformedURL exercises the guard
// directly: a repository_url with no "/repos/" segment has no owner/name to
// extract, and must report "" rather than the URL itself.
func TestRepoFullNameFromURLReturnsEmptyOnMalformedURL(t *testing.T) {
	tests := map[string]string{
		"no repos segment": "https://api.github.com/orgs/example-org",
		"empty string":     "",
	}
	for name, url := range tests {
		t.Run(name, func(t *testing.T) {
			if got := repoFullNameFromURL(url); got != "" {
				t.Errorf("repoFullNameFromURL(%q) = %q, want \"\"", url, got)
			}
		})
	}
}

// TestPullRequestsExcludesSelfRepoUnconditionally proves the special GitHub
// profile repo ({user}/{user}) is always dropped from PullRequests - no
// config field controls it, and no second search request is spent to filter
// it.
func TestPullRequestsExcludesSelfRepoUnconditionally(t *testing.T) {
	items := []searchIssue{
		pr(1, "self", "octocat/octocat"),
		pr(2, "kept", "example-org/example-repo"),
	}
	srv := newSearchServer(t, items, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (self-repo PRs must be excluded)", got)
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d /search/issues requests, want exactly 1", len(srv.queries))
	}
}

func TestPullRequestsReturnsErrorFromSearch(t *testing.T) {
	srv := newSearchServer(t, nil, http.StatusForbidden)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err == nil {
		t.Fatal("PullRequests: want error on non-200 search response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

// TestPullRequestsPagesWhenExcludedReposReduceFirstPage demonstrates the bug
// fix: when enough items on the first search page are excluded, pagination
// continues to subsequent pages until Limit is reached or results are exhausted.
// Without pagination, this test would return fewer than Limit PRs even though
// plenty of non-excluded PRs exist on the next page.
func TestPullRequestsPagesWhenExcludedReposReduceFirstPage(t *testing.T) {
	// First page: 10 results, but 7 are excluded (excluded-org)
	// So only 3 non-excluded items from first page.
	// With limit=5, we need to continue to the next page to get 2 more.
	page1 := []searchIssue{
		pr(1, "excluded1", "excluded-org/example-repo"),
		pr(2, "excluded2", "excluded-org/example-repo"),
		pr(3, "excluded3", "excluded-org/example-repo"),
		pr(4, "excluded4", "excluded-org/example-repo"),
		pr(5, "excluded5", "excluded-org/example-repo"),
		pr(6, "excluded6", "excluded-org/example-repo"),
		pr(7, "excluded7", "excluded-org/example-repo"),
		pr(8, "kept1", "example-org/example-repo"),
		pr(9, "kept2", "example-org/example-repo"),
		pr(10, "kept3", "example-org/example-repo"),
	}
	// Second page: 3 more non-excluded results
	page2 := []searchIssue{
		pr(11, "kept4", "example-org/example-repo"),
		pr(12, "kept5", "example-org/example-repo"),
		pr(13, "kept6", "example-org/example-repo"),
	}
	srv := newSearchServerPages(t, [][]searchIssue{page1, page2}, 0)
	c := testClient(t, srv.URL)

	gh := prConfig(5)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := PullRequests(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	// Should have exactly 5 results from both pages
	if len(got) != 5 {
		t.Errorf("got %d PRs, want 5 (from first and second pages after filtering)", len(got))
	}
	// Should have paginated: at least 2 requests (one per page)
	if len(srv.queries) < 2 {
		t.Errorf("observed %d /search/issues requests, want at least 2 (must page when first page is mostly excluded)", len(srv.queries))
	}
	// Verify we got the right PRs
	want := []int{8, 9, 10, 11, 12}
	for i, pr := range got {
		if pr.Number != want[i] {
			t.Errorf("got PR #%d at position %d, want #%d", pr.Number, i, want[i])
		}
	}
}

// TestPullRequestsStopsAtLimitWithoutFetchingExtraPages verifies that once Limit
// is reached, no further pages are requested. Uses a full-length first page so
// only the limit check, not a short-page break, stops the loop.
func TestPullRequestsStopsAtLimitWithoutFetchingExtraPages(t *testing.T) {
	// Generate a full page (10 items at limit=5 means perPage=10)
	page1 := []searchIssue{
		pr(1, "one", "example-org/example-repo"),
		pr(2, "two", "example-org/example-repo"),
		pr(3, "three", "example-org/example-repo"),
		pr(4, "four", "example-org/example-repo"),
		pr(5, "five", "example-org/example-repo"),
		pr(6, "six", "example-org/example-repo"),
		pr(7, "seven", "example-org/example-repo"),
		pr(8, "eight", "example-org/example-repo"),
		pr(9, "nine", "example-org/example-repo"),
		pr(10, "ten", "example-org/example-repo"),
	}
	page2 := []searchIssue{
		pr(11, "extra", "example-org/example-repo"),
	}
	srv := newSearchServerPages(t, [][]searchIssue{page1, page2}, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(5))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 5 {
		t.Fatalf("got %d PRs, want 5", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (must stop once Limit is reached)", len(srv.queries))
	}
}

// TestPullRequestsStopsPaginatingOnShortPage verifies that pagination stops
// when a short page is received, even if Limit has not been reached yet.
func TestPullRequestsStopsPaginatingOnShortPage(t *testing.T) {
	page1 := []searchIssue{
		pr(1, "only", "example-org/example-repo"),
	}
	srv := newSearchServerPages(t, [][]searchIssue{page1}, 0)
	c := testClient(t, srv.URL)

	got, err := PullRequests(context.Background(), c, "octocat", prConfig(10))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("got %d PRs, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (short page must stop pagination)", len(srv.queries))
	}
}

// TestPullRequestsBoundedByMaxSearchPages verifies that pagination is bounded by
// maxSearchPages, never requesting more than that many pages even if Limit is not
// yet reached. This test isolates the maxSearchPages bound: limit is set high
// enough (1000) that it cannot be satisfied by 3 pages of 100 items (300 total).
// If the function stops after exactly 3 requests with 300 results, it must be due
// to the maxSearchPages bound, not due to reaching limit. This test would fail if
// maxSearchPages were removed or set much higher.
func TestPullRequestsBoundedByMaxSearchPages(t *testing.T) {
	// Create many full pages of 100 items each (matching maxSearchPerPage),
	// well beyond maxSearchPages.
	genPage := func(pageNum int) []searchIssue {
		items := make([]searchIssue, 100)
		for i := range items {
			prNum := pageNum*100 + i + 1
			items[i] = pr(prNum, fmt.Sprintf("pr%d", prNum), "example-org/example-repo")
		}
		return items
	}
	pages := [][]searchIssue{
		genPage(0),
		genPage(1),
		genPage(2),
		genPage(3),
		genPage(4),
	}
	srv := newSearchServerPages(t, pages, 0)
	c := testClient(t, srv.URL)

	// Request limit=1000: with perPage=100, we would need 10 full pages to satisfy
	// this limit. maxSearchPages=3, so the function should stop after 3 pages due to
	// the bound, returning 300 items (3*100) which is less than the requested 1000.
	// This proves we stopped due to the page bound, not because limit happened to be
	// satisfied. If the maxSearchPages constant were removed/increased, the function
	// would continue paging past 3 requests, failing both assertions below.
	got, err := PullRequests(context.Background(), c, "octocat", prConfig(1000))
	if err != nil {
		t.Fatalf("PullRequests: %v", err)
	}
	// Should have made exactly 3 requests (limited by maxSearchPages), not more.
	if len(srv.queries) != 3 {
		t.Errorf("observed %d requests, want exactly 3 (maxSearchPages bound must stop pagination, not limit)", len(srv.queries))
	}
	// Should have collected 300 items (3 full pages of 100 each), which is less than
	// the requested limit of 1000. This proves we stopped due to the page bound.
	wantResults := 300 // 3 pages * 100 items/page
	if len(got) != wantResults {
		t.Errorf("got %d PRs, want %d (stopped by maxSearchPages bound, not by reaching limit)", len(got), wantResults)
	}
}
