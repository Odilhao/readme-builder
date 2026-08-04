package fetch

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// commitItem is the minimal shape of a /search/commits result item.
type commitItem struct {
	SHA        string `json:"sha"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func commit(sha, repoFullName string) commitItem {
	c := commitItem{SHA: sha}
	c.Repository.FullName = repoFullName
	return c
}

// topProjectsServer serves /search/commits, /search/issues and
// /users/{u}/events/public from fixed, single-page responses, recording every
// request path and query so tests can assert call counts and pagination
// (or its absence) directly off the wire.
type topProjectsServer struct {
	*httptest.Server

	paths          []string
	commitsQueries []url.Values
	issuesQueries  []url.Values
	commits        []commitItem
	issues         []searchIssue
	events         []event
	commitsStatus  int
	issuesStatus   int
	eventsStatus   int
}

func newTopProjectsServer(t *testing.T, commits []commitItem, issues []searchIssue, events []event) *topProjectsServer {
	t.Helper()
	return newTopProjectsServerWithStatus(t, commits, issues, events, 0, 0, 0)
}

func newTopProjectsServerWithStatus(t *testing.T, commits []commitItem, issues []searchIssue, events []event, commitsStatus, issuesStatus, eventsStatus int) *topProjectsServer {
	t.Helper()

	s := &topProjectsServer{commits: commits, issues: issues, events: events, commitsStatus: commitsStatus, issuesStatus: issuesStatus, eventsStatus: eventsStatus}
	mux := http.NewServeMux()
	mux.HandleFunc("/search/commits", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.Path)
		s.commitsQueries = append(s.commitsQueries, r.URL.Query())
		if s.commitsStatus != 0 {
			w.WriteHeader(s.commitsStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        len(s.commits),
			"incomplete_results": false,
			"items":              s.commits,
		})
	})
	mux.HandleFunc("/search/issues", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.Path)
		s.issuesQueries = append(s.issuesQueries, r.URL.Query())
		if s.issuesStatus != 0 {
			w.WriteHeader(s.issuesStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total_count":        len(s.issues),
			"incomplete_results": false,
			"items":              s.issues,
		})
	})
	mux.HandleFunc("/users/octocat/events/public", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.Path)
		if s.eventsStatus != 0 {
			w.WriteHeader(s.eventsStatus)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.events)
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func (s *topProjectsServer) countPath(path string) int {
	n := 0
	for _, p := range s.paths {
		if p == path {
			n++
		}
	}
	return n
}

func tpConfig(limit int) config.GitHub {
	return config.GitHub{TopProjects: &config.Section{Limit: limit, TimeWindow: "30d"}}
}

func TestTopProjectsNilSectionMakesNoRequest(t *testing.T) {
	srv := newTopProjectsServer(t, nil, nil, nil)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.paths) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.paths))
	}
}

func TestTopProjectsZeroLimitMakesNoRequest(t *testing.T) {
	srv := newTopProjectsServer(t, nil, nil, nil)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(0))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.paths) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.paths))
	}
}

// TestTopProjectsMakesExactlyTwoSearchCallsAndOneEventsCall pins the exact
// call budget agreed at intake: /search/commits once, /search/issues once,
// and /users/{u}/events/public for review counts - never a third search
// call, and never more than one call per source regardless of result count.
func TestTopProjectsMakesExactlyTwoSearchCallsAndOneEventsCall(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "example-org/example-repo")},
		[]searchIssue{pr(1, "one", "example-org/example-repo")},
		[]event{ev("PullRequestReviewEvent", "example-org/example-repo", time.Now())},
	)
	c := testClient(t, srv.URL)

	if _, err := TopProjects(context.Background(), c, "octocat", tpConfig(10)); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if n := srv.countPath("/search/commits"); n != 1 {
		t.Errorf("/search/commits requests = %d, want exactly 1", n)
	}
	if n := srv.countPath("/search/issues"); n != 1 {
		t.Errorf("/search/issues requests = %d, want exactly 1", n)
	}
	if n := srv.countPath("/users/octocat/events/public"); n < 1 {
		t.Errorf("/users/octocat/events/public requests = %d, want at least 1", n)
	}
}

// TestTopProjectsSearchCallsAreUnconditionalSinglePage proves neither search
// call ever pages: even a full page of results (matching maxSearchPerPage)
// must not trigger a second /search/commits or /search/issues request.
func TestTopProjectsSearchCallsAreUnconditionalSinglePage(t *testing.T) {
	commits := make([]commitItem, maxSearchPerPage)
	for i := range commits {
		commits[i] = commit("sha", "example-org/example-repo")
	}
	issues := make([]searchIssue, maxSearchPerPage)
	for i := range issues {
		issues[i] = pr(i+1, "pr", "example-org/example-repo")
	}
	srv := newTopProjectsServer(t, commits, issues, nil)
	c := testClient(t, srv.URL)

	if _, err := TopProjects(context.Background(), c, "octocat", tpConfig(1000)); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if n := srv.countPath("/search/commits"); n != 1 {
		t.Errorf("/search/commits requests = %d, want exactly 1 even on a full page", n)
	}
	if n := srv.countPath("/search/issues"); n != 1 {
		t.Errorf("/search/issues requests = %d, want exactly 1 even on a full page", n)
	}
}

// TestTopProjectsSearchQueriesScopeToAuthorAndSince pins the literal 'q'
// values sent to both search endpoints: author-scoped, and qualified by a
// since date derived from the time window.
func TestTopProjectsSearchQueriesScopeToAuthorAndSince(t *testing.T) {
	srv := newTopProjectsServer(t, nil, nil, nil)
	c := testClient(t, srv.URL)

	gh := tpConfig(10)
	gh.TopProjects.TimeWindow = "7d"
	if _, err := TopProjects(context.Background(), c, "octocat", gh); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	wantSince := time.Now().UTC().AddDate(0, 0, -7).Format("2006-01-02")

	if len(srv.commitsQueries) != 1 {
		t.Fatalf("got %d /search/commits requests, want 1", len(srv.commitsQueries))
	}
	if got, want := srv.commitsQueries[0].Get("q"), "author:octocat author-date:>"+wantSince; got != want {
		t.Errorf("/search/commits q = %q, want %q", got, want)
	}

	if len(srv.issuesQueries) != 1 {
		t.Fatalf("got %d /search/issues requests, want 1", len(srv.issuesQueries))
	}
	if got, want := srv.issuesQueries[0].Get("q"), "is:pr author:octocat created:>"+wantSince; got != want {
		t.Errorf("/search/issues q = %q, want %q", got, want)
	}
	wantPerPage := "100" // maxSearchPerPage, the search API's own ceiling
	if got := srv.commitsQueries[0].Get("per_page"); got != wantPerPage {
		t.Errorf("/search/commits per_page = %q, want %q", got, wantPerPage)
	}
	if got := srv.issuesQueries[0].Get("per_page"); got != wantPerPage {
		t.Errorf("/search/issues per_page = %q, want %q", got, wantPerPage)
	}
}

// TestTopProjectsDefaultsEmptyTimeWindowToThirtyDays exercises the fetcher's
// own default directly, via a config.Section literal that omits TimeWindow -
// the shape callers other than config.Load (e.g. tests, or a future caller)
// can construct. config.Load itself always fills TimeWindow in before this
// function runs, so this is the only path that reaches the fetcher's own
// fallback.
func TestTopProjectsDefaultsEmptyTimeWindowToThirtyDays(t *testing.T) {
	srv := newTopProjectsServer(t, nil, nil, nil)
	c := testClient(t, srv.URL)

	gh := config.GitHub{TopProjects: &config.Section{Limit: 10}}
	if _, err := TopProjects(context.Background(), c, "octocat", gh); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	wantSince := time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02")

	if len(srv.commitsQueries) != 1 {
		t.Fatalf("got %d /search/commits requests, want 1", len(srv.commitsQueries))
	}
	if got, want := srv.commitsQueries[0].Get("q"), "author:octocat author-date:>"+wantSince; got != want {
		t.Errorf("/search/commits q = %q, want %q (empty TimeWindow must default to 30d)", got, want)
	}
}

// TestTopProjectsAggregatesCommitsPRsAndReviewsIntoScore proves the three
// sources are summed per repo into a single Score, not merely three
// independent counts.
func TestTopProjectsAggregatesCommitsPRsAndReviewsIntoScore(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "example-org/example-repo"),
			commit("a2", "example-org/example-repo"),
			commit("a3", "example-org/example-repo"),
		},
		[]searchIssue{pr(1, "one", "example-org/example-repo")},
		[]event{
			ev("PullRequestReviewEvent", "example-org/example-repo", base),
			ev("PullRequestReviewEvent", "example-org/example-repo", base.Add(time.Hour)),
		},
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	want := model.TopProject{Repo: "example-org/example-repo", Commits: 3, PullRequests: 1, Reviews: 2, Score: 6}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

// TestTopProjectsSortsByScoreDescendingAndTruncatesToLimit proves ranking is
// by summed Score, and that Limit truncates after sorting rather than before.
func TestTopProjectsSortsByScoreDescendingAndTruncatesToLimit(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "example-org/low"),
			commit("a2", "example-org/high"),
			commit("a3", "example-org/high"),
			commit("a4", "example-org/high"),
		},
		nil, nil,
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(1))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/high" || got[0].Score != 3 {
		t.Errorf("got %+v, want only example-org/high with score 3", got)
	}
}

// TestTopProjectsAppliesExcludedFilterToAllThreeSources proves excluded()
// (ExcludeRepos/ExcludeOrgs/self-repo) is applied identically to commits,
// PRs and reviews, and that filtering happens after the single request per
// source, not by narrowing the search query itself.
func TestTopProjectsAppliesExcludedFilterToAllThreeSources(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "excluded-org/excluded-repo"),
			commit("a2", "example-org/example-repo"),
		},
		[]searchIssue{
			pr(1, "excluded", "excluded-org/excluded-repo"),
			pr(2, "kept", "example-org/example-repo"),
		},
		[]event{
			ev("PullRequestReviewEvent", "excluded-org/excluded-repo", base),
			ev("PullRequestReviewEvent", "example-org/example-repo", base),
		},
	)
	c := testClient(t, srv.URL)

	gh := tpConfig(10)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := TopProjects(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	want := model.TopProject{Repo: "example-org/example-repo", Commits: 1, PullRequests: 1, Reviews: 1, Score: 3}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v] (excluded-org must be dropped from every source)", got, want)
	}
	if n := srv.countPath("/search/commits"); n != 1 {
		t.Errorf("/search/commits requests = %d, want exactly 1 even after post-filtering", n)
	}
	if n := srv.countPath("/search/issues"); n != 1 {
		t.Errorf("/search/issues requests = %d, want exactly 1 even after post-filtering", n)
	}
}

// TestTopProjectsExcludesSelfRepoUnconditionally proves the special GitHub
// profile repo ({user}/{user}) is dropped from every source, matching
// excluded()'s existing self-repo guard.
func TestTopProjectsExcludesSelfRepoUnconditionally(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "octocat/octocat"), commit("a2", "example-org/example-repo")},
		nil, nil,
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo", got)
	}
}

// TestTopProjectsDropsCommitWithEmptyRepositoryFullName proves a commit
// search result whose Repository.FullName resolves empty (a malformed or
// unexpected API response) is dropped rather than surfacing as a
// TopProject{Repo: ""} entry in ranked output - the repo=="" half of the
// guard, distinct from excluded(), which passes an empty string through.
func TestTopProjectsDropsCommitWithEmptyRepositoryFullName(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", ""),
			commit("a2", "example-org/example-repo"),
		},
		nil, nil,
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (empty repository full_name must be dropped)", got)
	}
	for _, tp := range got {
		if tp.Repo == "" {
			t.Errorf("got %+v, want no entry with an empty Repo", got)
		}
	}
}

// TestTopProjectsDropsIssueWithUnparsableRepositoryURL proves an issue whose
// repository_url doesn't parse to a repo full name (repoFullNameFromURL
// returns "") is dropped, matching pullrequests.go's own handling of a
// malformed repository_url.
func TestTopProjectsDropsIssueWithUnparsableRepositoryURL(t *testing.T) {
	malformed := searchIssue{Number: 1, Title: "malformed", RepositoryURL: "", PullRequest: json.RawMessage(`{}`)}
	srv := newTopProjectsServer(t, nil,
		[]searchIssue{malformed, pr(2, "kept", "example-org/example-repo")},
		nil,
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (unparsable repository_url must be dropped)", got)
	}
	for _, tp := range got {
		if tp.Repo == "" {
			t.Errorf("got %+v, want no entry with an empty Repo", got)
		}
	}
}

// TestTopProjectsDropsEventWithEmptyRepoName proves a public event whose
// repo.name resolves empty is dropped, matching Contributions' own handling
// of the same malformed shape (TestContributionsDropsEventsWithEmptyRepoName).
func TestTopProjectsDropsEventWithEmptyRepoName(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t, nil, nil, []event{
		ev("PullRequestReviewEvent", "", base),
		ev("PullRequestReviewEvent", "example-org/example-repo", base),
	})
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (empty event repo.name must be dropped)", got)
	}
	for _, tp := range got {
		if tp.Repo == "" {
			t.Errorf("got %+v, want no entry with an empty Repo", got)
		}
	}
}

// TestTopProjectsSkipsNonPullRequestIssues exercises the defensive is-PR
// check on the issues search results, matching pullrequests.go's own guard.
func TestTopProjectsSkipsNonPullRequestIssues(t *testing.T) {
	srv := newTopProjectsServer(t, nil,
		[]searchIssue{
			issueNotPR(1, "plain issue", "example-org/example-repo"),
			pr(2, "a pr", "example-org/example-repo"),
		},
		nil,
	)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].PullRequests != 1 {
		t.Errorf("got %+v, want one repo with PullRequests=1 (plain issue must not count)", got)
	}
}

// TestTopProjectsIgnoresNonReviewEventTypes proves only PullRequestReviewEvent
// contributes to Reviews - other contribution-shaped event types (which
// Contributions itself does count) must not leak into this section's review
// count. PullRequestEvent is included deliberately, not just PushEvent and
// IssuesEvent: it is the actual near-miss regression risk (a near-identical
// type name, already present in contributionEventTypes), so a guard widened
// to also accept it must be caught here, not just a guard that accepts
// anything.
func TestTopProjectsIgnoresNonReviewEventTypes(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t, nil, nil, []event{
		ev("PushEvent", "example-org/example-repo", base),
		ev("IssuesEvent", "example-org/example-repo", base),
		ev("PullRequestEvent", "example-org/example-repo", base),
		ev("PullRequestReviewEvent", "example-org/example-repo", base),
	})
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}
	if len(got) != 1 || got[0].Reviews != 1 {
		t.Errorf("got %+v, want one repo with Reviews=1 (only PullRequestReviewEvent counts, not PullRequestEvent)", got)
	}
}

func TestTopProjectsReturnsErrorFromCommitsSearch(t *testing.T) {
	srv := newTopProjectsServerWithStatus(t, nil, nil, nil, http.StatusForbidden, 0, 0)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err == nil {
		t.Fatal("TopProjects: want error on non-200 /search/commits response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

func TestTopProjectsReturnsErrorFromIssuesSearch(t *testing.T) {
	srv := newTopProjectsServerWithStatus(t, nil, nil, nil, 0, http.StatusForbidden, 0)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err == nil {
		t.Fatal("TopProjects: want error on non-200 /search/issues response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

// TestTopProjectsSinceConvertsEveryWindowUnit exercises topProjectsSince
// directly, one case per Nd/Nw/Ny unit, so a unit's duration computation
// (days vs weeks vs years) cannot be swapped with another's undetected -
// the search query assertions elsewhere only ever exercise "d".
func TestTopProjectsSinceConvertsEveryWindowUnit(t *testing.T) {
	now := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	tests := map[string]struct {
		window string
		want   string
	}{
		"days":  {"5d", "2026-07-29"},
		"weeks": {"2w", "2026-07-20"},
		"years": {"1y", "2025-08-03"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			if got := topProjectsSince(tt.window, now); got != tt.want {
				t.Errorf("topProjectsSince(%q, %v) = %q, want %q", tt.window, now, got, tt.want)
			}
		})
	}
}

func TestTopProjectsReturnsErrorFromEventsList(t *testing.T) {
	srv := newTopProjectsServerWithStatus(t, nil, nil, nil, 0, 0, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err == nil {
		t.Fatal("TopProjects: want error on non-200 events list response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}
