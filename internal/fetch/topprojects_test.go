package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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

// topProjectsServer serves /search/commits, /search/issues,
// /users/{u}/events/public, and /repos/{owner}/{repo}/commits from fixed
// responses, recording every request path and query so tests can assert call
// counts and pagination (or its absence) directly off the wire.
type topProjectsServer struct {
	*httptest.Server

	paths               []string
	commitsQueries      []url.Values
	issuesQueries       []url.Values
	reposCommits        map[string]int // "owner/repo" -> count of commits to return
	reposCommitsReqs    []string       // full request URLs to /repos/.../commits
	reposCommitsQueries []url.Values   // query parameters for each /repos/.../commits request
	commits             []commitItem
	issues              []searchIssue
	events              []event
	commitsStatus       int
	issuesStatus        int
	eventsStatus        int
}

func newTopProjectsServer(t *testing.T, commits []commitItem, issues []searchIssue, events []event) *topProjectsServer {
	t.Helper()
	return newTopProjectsServerWithStatus(t, commits, issues, events, 0, 0, 0)
}

func newTopProjectsServerWithStatus(t *testing.T, commits []commitItem, issues []searchIssue, events []event, commitsStatus, issuesStatus, eventsStatus int) *topProjectsServer {
	t.Helper()

	s := &topProjectsServer{
		commits:       commits,
		issues:        issues,
		events:        events,
		commitsStatus: commitsStatus,
		issuesStatus:  issuesStatus,
		eventsStatus:  eventsStatus,
		reposCommits:  make(map[string]int),
	}
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
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		s.reposCommitsReqs = append(s.reposCommitsReqs, r.URL.String())
		// Extract owner/repo from the path like "/repos/owner/repo/commits"
		parts := strings.Split(r.URL.Path, "/")
		// parts = ["", "repos", "owner", "repo", "commits"] (leading empty from split on "/")
		if len(parts) >= 5 && parts[4] == "commits" {
			ownerRepo := parts[2] + "/" + parts[3]
			// Record query parameters for validation
			s.reposCommitsQueries = append(s.reposCommitsQueries, r.URL.Query())
			count := s.reposCommits[ownerRepo]
			w.Header().Set("Content-Type", "application/json")
			// Build commit items for the response
			commitItems := make([]map[string]any, 0)
			for i := 0; i < count && i < 1; i++ { // per_page=1, only return 1 item if count > 0
				commitItems = append(commitItems, map[string]any{
					"sha": "fake-sha-" + ownerRepo,
				})
			}
			// If count > 1, set Link header with last page = count
			// Format: <URL?page=1>; rel="first", <URL?page=N>; rel="last"
			if count > 1 {
				firstURL := fmt.Sprintf("%s%s?page=1", s.URL, r.URL.Path)
				lastURL := fmt.Sprintf("%s%s?page=%d", s.URL, r.URL.Path, count)
				w.Header().Set("Link", fmt.Sprintf(`<%s>; rel="first", <%s>; rel="last"`, firstURL, lastURL))
			}
			_ = json.NewEncoder(w).Encode(commitItems)
		}
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

func (s *topProjectsServer) repoCommitsRequests() int {
	n := 0
	for _, r := range s.reposCommitsReqs {
		if strings.Contains(r, "/repos/") && strings.Contains(r, "/commits") {
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
	srv.reposCommits["example-org/example-repo"] = 3
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
	srv.reposCommits["example-org/low"] = 1
	srv.reposCommits["example-org/high"] = 3
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
	srv.reposCommits["example-org/example-repo"] = 1
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

// TestTopProjectsFetchesExactCommitCountsForTopCandidatesOnly tests that exact
// commit counts are fetched only for repos in the final top-Limit output,
// not for all candidates discovered.
func TestTopProjectsFetchesExactCommitCountsForTopCandidatesOnly(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "org-a/repo-1"),
			commit("a2", "org-a/repo-1"),
			commit("a3", "org-a/repo-1"),
			commit("b1", "org-b/repo-2"),
			commit("c1", "org-c/repo-3"),
		},
		nil, nil,
	)
	// Set up exact commit counts for repos
	srv.reposCommits["org-a/repo-1"] = 50
	srv.reposCommits["org-b/repo-2"] = 25
	srv.reposCommits["org-c/repo-3"] = 10

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(2))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Should have exactly 2 results (limit is 2)
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}

	// Should only have made 2 exact commit requests, not 3
	if n := srv.repoCommitsRequests(); n != 2 {
		t.Errorf("exact commit requests = %d, want 2 (only for top Limit repos)", n)
	}

	// Top 2 should be org-a/repo-1 and org-b/repo-2
	if got[0].Repo != "org-a/repo-1" || got[0].Commits != 50 {
		t.Errorf("got[0] = %+v, want org-a/repo-1 with 50 commits", got[0])
	}
	if got[1].Repo != "org-b/repo-2" || got[1].Commits != 25 {
		t.Errorf("got[1] = %+v, want org-b/repo-2 with 25 commits", got[1])
	}
}

// TestTopProjectsHandlesLinkHeaderWithMultiplePages tests that a Link header
// with a "last" rel and page number is parsed to get the exact count.
func TestTopProjectsHandlesLinkHeaderWithMultiplePages(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "org/repo-multi")},
		nil, nil,
	)
	srv.reposCommits["org/repo-multi"] = 42 // Sets Link header with page=42

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 1 || got[0].Commits != 42 {
		t.Errorf("got %+v, want 42 commits from Link header", got)
	}
}

// TestTopProjectsHandlesZeroCommits tests the edge case where a repo has
// zero commits: empty result list, no Link header.
func TestTopProjectsHandlesZeroCommits(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "org/repo-zero")},
		nil, nil,
	)
	srv.reposCommits["org/repo-zero"] = 0 // No commits

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 1 || got[0].Commits != 0 {
		t.Errorf("got %+v, want 0 commits", got)
	}
}

// TestTopProjectsHandlesSingleCommit tests the edge case where a repo has
// exactly one commit: single item in result, no Link header, LastPage = 0.
func TestTopProjectsHandlesSingleCommit(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "org/repo-one")},
		nil, nil,
	)
	srv.reposCommits["org/repo-one"] = 1 // Single commit

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 1 || got[0].Commits != 1 {
		t.Errorf("got %+v, want 1 commit", got)
	}
}

// TestTopProjectsRecomputesScoreAfterExactCommits tests that Score is
// recomputed as exact_commits + rough_prs + rough_reviews after the exact
// commit fetch and re-sorted.
func TestTopProjectsRecomputesScoreAfterExactCommits(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "org/repo-a"),
			commit("b1", "org/repo-b"),
			commit("b2", "org/repo-b"),
		},
		[]searchIssue{
			pr(1, "pr", "org/repo-b"), // 1 PR for repo-b
		},
		[]event{
			ev("PullRequestReviewEvent", "org/repo-a", base), // 1 review for repo-a
		},
	)
	// Exact counts: repo-a has 100 commits, repo-b has 20 commits
	srv.reposCommits["org/repo-a"] = 100
	srv.reposCommits["org/repo-b"] = 20

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Expected scores after exact count fetch:
	// repo-a: 100 (exact commits) + 0 (prs) + 1 (review) = 101
	// repo-b: 20 (exact commits) + 1 (pr) + 0 (reviews) = 21
	// So repo-a should be first
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}
	if got[0].Repo != "org/repo-a" || got[0].Score != 101 {
		t.Errorf("got[0] = %+v, want org/repo-a with score 101", got[0])
	}
	if got[1].Repo != "org/repo-b" || got[1].Score != 21 {
		t.Errorf("got[1] = %+v, want org/repo-b with score 21", got[1])
	}
}

// TestTopProjectsResortAfterExactCommits tests that repos are re-sorted by
// their new Score after exact commit counts are fetched, changing the order
// from the rough-score ranking.
func TestTopProjectsResortAfterExactCommits(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			// Rough search: repo-a appears 5 times, repo-b appears 3 times
			commit("a1", "org/repo-a"),
			commit("a2", "org/repo-a"),
			commit("a3", "org/repo-a"),
			commit("a4", "org/repo-a"),
			commit("a5", "org/repo-a"),
			commit("b1", "org/repo-b"),
			commit("b2", "org/repo-b"),
			commit("b3", "org/repo-b"),
		},
		nil, nil,
	)
	// Exact counts: repo-a has 5 commits, repo-b has 100 commits
	// So the order should flip after exact counts
	srv.reposCommits["org/repo-a"] = 5
	srv.reposCommits["org/repo-b"] = 100

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}
	// After exact counts, repo-b should be first (100 commits > 5 commits)
	if got[0].Repo != "org/repo-b" || got[0].Commits != 100 {
		t.Errorf("got[0] = %+v, want org/repo-b with 100 commits", got[0])
	}
	if got[1].Repo != "org/repo-a" || got[1].Commits != 5 {
		t.Errorf("got[1] = %+v, want org/repo-a with 5 commits", got[1])
	}
}

// TestTopProjectsAppliesExcludedBeforeExactCommits tests that excluded()
// filtering is still applied before the exact commit fetch, preventing
// excluded repos from even getting those requests.
func TestTopProjectsAppliesExcludedBeforeExactCommits(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "excluded-org/excluded-repo"),
			commit("a2", "excluded-org/excluded-repo"),
			commit("b1", "example-org/example-repo"),
		},
		nil, nil,
	)
	srv.reposCommits["excluded-org/excluded-repo"] = 50
	srv.reposCommits["example-org/example-repo"] = 1

	c := testClient(t, srv.URL)

	gh := tpConfig(10)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := TopProjects(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Only example-org/example-repo should be in results
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (excluded-org must be dropped)", got)
	}

	// No exact commit request should have been made for the excluded repo
	for _, req := range srv.reposCommitsReqs {
		if strings.Contains(req, "excluded-org") {
			t.Errorf("unexpected request to excluded repo: %s", req)
		}
	}
}

// TestTopProjectsNoNewSearchCallsForExactCommits verifies that the exact
// commit fetch does not introduce any new search API calls - it only uses
// core REST /repos/{owner}/{repo}/commits endpoints.
func TestTopProjectsNoNewSearchCallsForExactCommits(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{
			commit("a1", "org/repo-a"),
			commit("b1", "org/repo-b"),
		},
		[]searchIssue{
			pr(1, "pr", "org/repo-a"),
		},
		[]event{
			ev("PullRequestReviewEvent", "org/repo-b", time.Now()),
		},
	)
	srv.reposCommits["org/repo-a"] = 50
	srv.reposCommits["org/repo-b"] = 25

	c := testClient(t, srv.URL)

	if _, err := TopProjects(context.Background(), c, "octocat", tpConfig(10)); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Should still have exactly 1 /search/commits and 1 /search/issues call
	if n := srv.countPath("/search/commits"); n != 1 {
		t.Errorf("/search/commits requests = %d, want exactly 1", n)
	}
	if n := srv.countPath("/search/issues"); n != 1 {
		t.Errorf("/search/issues requests = %d, want exactly 1", n)
	}
}

// TestTopProjectsExactCountsViaZeroToken verifies that exact commit counts
// work unauthenticated (the credential-free guarantee from invariant 1).
func TestTopProjectsExactCountsViaZeroToken(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "org/repo")},
		nil, nil,
	)
	srv.reposCommits["org/repo"] = 75

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 1 || got[0].Commits != 75 {
		t.Errorf("got %+v, want 75 commits (unauthenticated request)", got)
	}
}

// TestTopProjectsExactCommitCountsPassesCorrectQueryParams asserts that the
// /repos/{owner}/{repo}/commits requests include the correct query parameters:
// author (filtered to user), since (from TimeWindow), and until (current time).
// This test would fail if these filters are removed or wrong, catching any
// regression where the exact commit count falls back to search results.
func TestTopProjectsExactCommitCountsPassesCorrectQueryParams(t *testing.T) {
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "org/repo-test")},
		nil, nil,
	)
	srv.reposCommits["org/repo-test"] = 15

	c := testClient(t, srv.URL)

	gh := tpConfig(10)
	gh.TopProjects.TimeWindow = "7d"
	if _, err := TopProjects(context.Background(), c, "octocat", gh); err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(srv.reposCommitsQueries) != 1 {
		t.Fatalf("got %d /repos/.../commits requests, want exactly 1", len(srv.reposCommitsQueries))
	}

	q := srv.reposCommitsQueries[0]

	// Verify author parameter
	if got, want := q.Get("author"), "octocat"; got != want {
		t.Errorf("author param = %q, want %q", got, want)
	}

	// Verify since parameter (should be 7 days ago, in RFC3339 format as serialized by go-github)
	wantSinceParsed := time.Now().UTC().AddDate(0, 0, -7)
	if got := q.Get("since"); got == "" {
		t.Error("since param is empty, want an RFC3339 timestamp")
	} else {
		// Parse the returned since value to verify it's in the right ballpark (same day)
		gotTime, err := time.Parse(time.RFC3339, got)
		if err != nil {
			t.Errorf("since param %q is not valid RFC3339: %v", got, err)
		} else if gotTime.Format("2006-01-02") != wantSinceParsed.Format("2006-01-02") {
			t.Errorf("since param date = %q, want date %q", gotTime.Format("2006-01-02"), wantSinceParsed.Format("2006-01-02"))
		}
	}

	// Verify until parameter is set (should be an RFC3339 timestamp)
	if got := q.Get("until"); got == "" {
		t.Error("until param is empty, want an RFC3339 timestamp")
	} else if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("until param %q is not valid RFC3339: %v", got, err)
	}

	// Verify per_page parameter is 1 (for pagination via Link header)
	if got, want := q.Get("per_page"), "1"; got != want {
		t.Errorf("per_page param = %q, want %q", got, want)
	}
}

// TestTopProjectsPushEventOnlyRepoBecomesCandidate tests that a repo appearing
// solely in PushEvent data (not on either search page) is included as a
// candidate and receives an exact commit count. This proves PushEvent mining
// widens the candidate pool beyond the two fixed search queries.
func TestTopProjectsPushEventOnlyRepoBecomesCandidate(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		[]commitItem{commit("a1", "example-org/search-repo")},
		[]searchIssue{pr(1, "pr", "example-org/search-repo")},
		[]event{
			ev("PullRequestReviewEvent", "example-org/search-repo", base),
			ev("PushEvent", "example-org/push-only-repo", base),
		},
	)
	// Set up exact commit counts for both repos
	srv.reposCommits["example-org/search-repo"] = 10
	srv.reposCommits["example-org/push-only-repo"] = 25

	c := testClient(t, srv.URL)

	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(10))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Should have 2 repos: one from search results, one from PushEvent only
	if len(got) != 2 {
		t.Fatalf("got %d repos, want 2", len(got))
	}

	// Find the push-only repo in results
	var pushOnlyRepo *model.TopProject
	for i := range got {
		if got[i].Repo == "example-org/push-only-repo" {
			pushOnlyRepo = &got[i]
			break
		}
	}

	if pushOnlyRepo == nil {
		t.Errorf("PushEvent-only repo not found in results: %+v", got)
	} else if pushOnlyRepo.Commits != 25 {
		t.Errorf("PushEvent-only repo has %d commits, want 25 (should get exact count)", pushOnlyRepo.Commits)
	}

	// Verify that we still made only 1 search/commits and 1 search/issues call (no new requests)
	if n := srv.countPath("/search/commits"); n != 1 {
		t.Errorf("/search/commits requests = %d, want exactly 1", n)
	}
	if n := srv.countPath("/search/issues"); n != 1 {
		t.Errorf("/search/issues requests = %d, want exactly 1", n)
	}
}

// TestTopProjectsAppliesExcludedFilterToPushEvents tests that excluded()
// filtering is applied to repos discovered via PushEvent, just as it is to
// commits, PRs, and review events - filtering happens in the event loop, not
// by narrowing the search query.
func TestTopProjectsAppliesExcludedFilterToPushEvents(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		nil, nil,
		[]event{
			ev("PushEvent", "excluded-org/excluded-repo", base),
			ev("PushEvent", "example-org/example-repo", base),
		},
	)
	srv.reposCommits["example-org/example-repo"] = 5

	c := testClient(t, srv.URL)

	gh := tpConfig(10)
	gh.ExcludeOrgs = []string{"excluded-org"}
	got, err := TopProjects(context.Background(), c, "octocat", gh)
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	// Should have only the non-excluded repo
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (excluded-org must be dropped from PushEvent)", got)
	}

	// Verify excluded repo did not get an exact commit request
	for _, req := range srv.reposCommitsReqs {
		if strings.Contains(req, "excluded-org") {
			t.Errorf("unexpected request to excluded repo: %s", req)
		}
	}
}

// TestTopProjectsPushEventOnlyRepoScoresToZeroPreCap tests that PushEvent-only
// repos contribute zero to pre-cap scoring. This test uses a scenario where
// pre-cap ranking determines which candidate survives the Limit cap: a
// commit-sourced candidate (highest score), a PushEvent-only repo (must be 0),
// and review-sourced candidates (score 1 each). With Limit=2, if the
// mutation (get(repo).commits++) were applied, the push-only repo would score 1
// pre-cap, rank equal to or above a review-sourced candidate, and wrongly
// displace it from the top-Limit window. The test verifies a review-sourced
// candidate is in the final results, proving it survived pre-cap truncation
// and was not wrongly displaced by the push-only repo scoring >0.
func TestTopProjectsPushEventOnlyRepoScoresToZeroPreCap(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newTopProjectsServer(t,
		[]commitItem{
			// commits-repo: 2 commits (highest pre-cap score = 2)
			commit("c1", "example-org/commits-repo"),
			commit("c2", "example-org/commits-repo"),
		},
		nil, // no PRs
		[]event{
			// push-only-repo: PushEvent only (pre-cap score = 0, must not increment)
			// Added first so it appears before review-sourced repos in insertion order.
			// With the mutation (get(repo).commits++), push-only would score 1 pre-cap.
			ev("PushEvent", "example-org/push-only-repo", base),
			// review-repo1: 1 review (pre-cap score = 1)
			ev("PullRequestReviewEvent", "example-org/review-repo1", base),
			// review-repo2: 1 review (pre-cap score = 1)
			ev("PullRequestReviewEvent", "example-org/review-repo2", base),
		},
	)

	srv.reposCommits["example-org/commits-repo"] = 50
	srv.reposCommits["example-org/push-only-repo"] = 5
	srv.reposCommits["example-org/review-repo1"] = 60
	srv.reposCommits["example-org/review-repo2"] = 40

	c := testClient(t, srv.URL)

	// Limit = 2: with correct code, insertion order is [commits-repo, push-only-repo,
	// review-repo1, review-repo2], pre-cap scores are [2, 0, 1, 1]. Sorted descending
	// with stable sort: commits-repo(2), review-repo1(1), review-repo2(1), push-only(0).
	// Top 2 = commits-repo and review-repo1.
	//
	// With mutation (push-only.commits++), pre-cap scores become [2, 1, 1, 1].
	// Sorted descending stable: commits-repo(2), push-only(1), review-repo1(1), review-repo2(1).
	// Top 2 = commits-repo and push-only (review-repo1 wrongly truncated).
	got, err := TopProjects(context.Background(), c, "octocat", tpConfig(2))
	if err != nil {
		t.Fatalf("TopProjects: %v", err)
	}

	if len(got) != 2 {
		t.Fatalf("got %d repos, want exactly 2", len(got))
	}

	// Verify review-repo1 is in the final results. If the mutation were applied,
	// review-repo1 would be wrongly excluded from the pre-cap top-Limit, would not
	// get exact counts, and would not appear here.
	var foundReviewRepo1 bool
	for _, tp := range got {
		if tp.Repo == "example-org/review-repo1" {
			foundReviewRepo1 = true
			// After exact count fetch: 60 (exact commits) + 0 (PRs) + 1 (review) = 61
			if tp.Commits != 60 {
				t.Errorf("review-repo1 commits = %d, want 60", tp.Commits)
			}
			if tp.Score != 61 {
				t.Errorf("review-repo1 score = %d, want 61", tp.Score)
			}
			break
		}
	}
	if !foundReviewRepo1 {
		t.Errorf("review-repo1 not found in top-2 results %+v; it was wrongly excluded by pre-cap truncation (mutation likely applied)", got)
	}

	// Verify push-only-repo is NOT in the top 2 results. It should rank 3rd after
	// exact counts are applied (scores: review-repo1=61, commits-repo=53, push-only=5).
	for _, tp := range got {
		if tp.Repo == "example-org/push-only-repo" {
			t.Errorf("push-only-repo unexpectedly in top 2 results: %+v (should rank below review-repo1)", tp)
		}
	}
}
