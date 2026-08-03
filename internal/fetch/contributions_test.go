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

// event is the minimal shape of a public event, as the fixtures need it.
type event struct {
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	Repo      struct {
		Name string `json:"name"`
	} `json:"repo"`
}

// contribServer serves /users/{u}/events/public from a fixed page of events
// and /repos/{owner}/{repo} from a fork map, recording every request path.
type contribServer struct {
	*httptest.Server

	paths []string
	forks map[string]bool
}

// eventsStatus and reposStatus force a non-200 on the respective endpoint
// when set, for exercising the error paths.
type contribServerOpts struct {
	eventsStatus int
	reposStatus  int
}

func newContribServer(t *testing.T, pages [][]event, forks map[string]bool) *contribServer {
	t.Helper()
	return newContribServerWithOpts(t, pages, forks, contribServerOpts{})
}

func newContribServerWithOpts(t *testing.T, pages [][]event, forks map[string]bool, opts contribServerOpts) *contribServer {
	t.Helper()

	s := &contribServer{forks: forks}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/octocat/events/public", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.String())
		if opts.eventsStatus != 0 {
			w.WriteHeader(opts.eventsStatus)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		w.Header().Set("Content-Type", "application/json")
		if page < 1 || page > len(pages) {
			_ = json.NewEncoder(w).Encode([]event{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	})
	mux.HandleFunc("/repos/", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.String())
		if opts.reposStatus != 0 {
			w.WriteHeader(opts.reposStatus)
			return
		}
		fork := s.forks[r.URL.Path]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"fork": fork})
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

// newUserContribServer serves /users/{user}/events/public for an
// arbitrary, caller-chosen username, unlike newContribServer which is fixed
// to "octocat". It exists to prove the user argument is actually threaded
// into the request rather than a username hardcoded somewhere in the call
// path: every other fixture in this file only ever exercises "octocat".
func newUserContribServer(t *testing.T, user string, pages [][]event) *contribServer {
	t.Helper()
	s := &contribServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+user+"/events/public", func(w http.ResponseWriter, r *http.Request) {
		s.paths = append(s.paths, r.URL.String())
		w.Header().Set("Content-Type", "application/json")
		if len(pages) == 0 {
			_ = json.NewEncoder(w).Encode([]event{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[0])
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func newTestClient(t *testing.T, srv *contribServer) *Client {
	t.Helper()
	c, err := New(Options{BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// repoDetailRequests counts requests to /repos/{owner}/{repo} (excludes the
// events list request).
func (s *contribServer) repoDetailRequests() int {
	n := 0
	for _, p := range s.paths {
		if len(p) >= 7 && p[:7] == "/repos/" {
			n++
		}
	}
	return n
}

// eventsListRequests counts requests to /users/{u}/events/public.
func (s *contribServer) eventsListRequests() int {
	n := 0
	for _, p := range s.paths {
		if len(p) >= 20 && p[:20] == "/users/octocat/event" {
			n++
		}
	}
	return n
}

// eventsListQueries returns the parsed query of every request to
// /users/{u}/events/public, in request order.
func (s *contribServer) eventsListQueries(t *testing.T) []url.Values {
	t.Helper()
	var qs []url.Values
	for _, p := range s.paths {
		if len(p) < 20 || p[:20] != "/users/octocat/event" {
			continue
		}
		u, err := url.Parse(p)
		if err != nil {
			t.Fatalf("url.Parse(%q): %v", p, err)
		}
		qs = append(qs, u.Query())
	}
	return qs
}

func ev(typ, repo string, t time.Time) event {
	e := event{Type: typ, CreatedAt: t}
	e.Repo.Name = repo
	return e
}

func ghConfig(limit int, excludeForks bool, excludeRepos, excludeOrgs []string) config.GitHub {
	return config.GitHub{
		ExcludeForks:  excludeForks,
		ExcludeRepos:  excludeRepos,
		ExcludeOrgs:   excludeOrgs,
		Contributions: &config.Section{Limit: limit, TimeWindow: "30d"},
	}
}

// contrib builds a Contribution with URLs constructed from repo name and login.
func contrib(repo string, events int, login string) model.Contribution {
	return model.Contribution{
		Repo:        repo,
		Events:      events,
		CommitsURL:  "https://github.com/" + repo + "/commits?author=" + login,
		ActivityURL: "https://github.com/" + repo + "/issues?q=updated:30d+author:" + login,
	}
}

func TestContributionsNilSectionMakesNoRequest(t *testing.T) {
	srv := newContribServer(t, nil, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.paths) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.paths))
	}
}

// TestContributionsThreadsUserArgumentIntoEventsRequestPath proves the user
// argument reaches the request URL rather than a hardcoded literal: every
// other test in this file calls Contributions with "octocat", so a call site
// that silently substitutes that literal for the user parameter would pass
// them all and only show up on a different username actually reaching the
// wire.
func TestContributionsThreadsUserArgumentIntoEventsRequestPath(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{ev("PushEvent", "example-org/example-repo", base)}}
	srv := newUserContribServer(t, "example-user", pages)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "example-user", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Fatalf("got %+v, want one contribution from example-org/example-repo", got)
	}
	if len(srv.paths) != 1 {
		t.Fatalf("got %d requests, want exactly 1 to /users/example-user/events/public", len(srv.paths))
	}
	if !strings.HasPrefix(srv.paths[0], "/users/example-user/events/public") {
		t.Errorf("request path = %q, want prefix /users/example-user/events/public (the user argument must be threaded into the request, not hardcoded)", srv.paths[0])
	}
}

func TestContributionsFiltersNonWorkEventTypes(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("WatchEvent", "octocat/Hello-World", base),
		ev("ForkEvent", "octocat/Hello-World", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	want := []model.Contribution{contrib("example-org/example-repo", 1, "octocat")}
	if len(got) != 1 || got[0] != want[0] {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestContributionsAppliesNameExclusionsBeforeLookup(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "excluded-org/excluded-repo", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, true, nil, []string{"excluded-org"}))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo", got)
	}
	// The excluded repo must never reach the detail-lookup stage: only the
	// surviving candidate should cost a GET /repos/{owner}/{name}.
	if n := srv.repoDetailRequests(); n != 1 {
		t.Errorf("repo detail requests = %d, want 1 (excluded repo must not be looked up)", n)
	}
}

func TestContributionsExcludesForksViaDetailLookup(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "octocat/a-fork", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	forks := map[string]bool{"/repos/octocat/a-fork": true}
	srv := newContribServer(t, pages, forks)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, true, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo", got)
	}
}

func TestContributionsCapsDetailLookupsAtLimit(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var events []event
	for i := 0; i < 5; i++ {
		events = append(events, ev("PushEvent", fmt.Sprintf("example-org/repo-%d", i), base.Add(time.Duration(i)*time.Hour)))
	}
	pages := [][]event{events}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	_, err := Contributions(context.Background(), c, "octocat", ghConfig(2, true, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if n := srv.repoDetailRequests(); n != 2 {
		t.Errorf("repo detail requests = %d, want exactly Limit=2", n)
	}
}

func TestContributionsSortsByMostRecentEventDescending(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "example-org/older", base),
		ev("PushEvent", "example-org/newer", base.Add(time.Hour)),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 2 || got[0].Repo != "example-org/newer" || got[1].Repo != "example-org/older" {
		t.Errorf("got %+v, want newer before older", got)
	}
}

func TestContributionsKeepsEveryContributionShapedEventType(t *testing.T) {
	// One repo per type: each must be counted, proving the type set is wired
	// entry by entry rather than merely "PushEvent passes, most things do".
	types := []string{
		"PushEvent",
		"PullRequestEvent",
		"PullRequestReviewEvent",
		"PullRequestReviewCommentEvent",
		"IssuesEvent",
		"IssueCommentEvent",
		"CommitCommentEvent",
		"CreateEvent",
		"ReleaseEvent",
	}

	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	var page []event
	for i, typ := range types {
		page = append(page, ev(typ, fmt.Sprintf("example-org/repo-%d", i), base.Add(time.Duration(i)*time.Minute)))
	}
	srv := newContribServer(t, [][]event{page}, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(len(types), false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != len(types) {
		t.Fatalf("got %d contributions, want %d (one per contribution-shaped type): %+v", len(got), len(types), got)
	}
	for i := range types {
		want := contrib(fmt.Sprintf("example-org/repo-%d", i), 1, "octocat")
		found := false
		for _, c := range got {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("missing contribution for type %s: %+v", types[i], want)
		}
	}
}

func TestContributionsExcludeOrgsIsCaseInsensitive(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "Excluded-Org/Excluded-Repo", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, []string{"excluded-org"}))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (exclude_orgs must match case-insensitively)", got)
	}
}

func TestContributionsExcludeReposIsCaseInsensitive(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "Excluded-Org/Excluded-Repo", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, []string{"excluded-org/excluded-repo"}, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (exclude_repos must match case-insensitively)", got)
	}
}

func TestContributionsMakesNoDetailRequestsWhenExcludeForksIsFalse(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{ev("PushEvent", "example-org/example-repo", base)}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	if _, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil)); err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if n := srv.repoDetailRequests(); n != 0 {
		t.Errorf("repo detail requests = %d, want 0 (ExcludeForks is false, so fork status is irrelevant)", n)
	}
}

func genEvents(n int, repoPrefix string, base time.Time) []event {
	events := make([]event, n)
	for i := range events {
		events[i] = ev("PushEvent", fmt.Sprintf("%s-%d", repoPrefix, i), base.Add(time.Duration(i)*time.Second))
	}
	return events
}

func TestContributionsContinuesPastAFullFirstPage(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{
		genEvents(eventsPerPage, "example-org/repo", base),
		{ev("PushEvent", "example-org/last", base.Add(time.Hour))},
	}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	if _, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil)); err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if n := srv.eventsListRequests(); n != 2 {
		t.Errorf("events list requests = %d, want 2 (a full page must not be treated as the last one)", n)
	}
}

func TestContributionsCapsPaginationAtThreePages(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	// Every page is full length, so only the hard page cap - not page
	// shortness - can stop the loop.
	pages := [][]event{
		genEvents(eventsPerPage, "example-org/repo-p1", base),
		genEvents(eventsPerPage, "example-org/repo-p2", base),
		genEvents(eventsPerPage, "example-org/repo-p3", base),
		genEvents(eventsPerPage, "example-org/repo-p4", base),
	}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	if _, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil)); err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if n := srv.eventsListRequests(); n != 3 {
		t.Errorf("want 3 pages (the ~300-event ceiling documented for this endpoint), got %d", n)
	}
}

// TestContributionsEventsListRequestsUseDocumentedPerPageAndAdvancePages pins
// the literal wire values of the events-list requests: per_page=100 on every
// request, and page advancing 1, 2, 3 across the three requests the hard cap
// allows. Deriving these from eventsPerPage/maxEventPages themselves (as the
// page-count assertion elsewhere does) would pass for any value of those
// constants; only literals catch a wrong per_page or a stalled/misadvancing
// page counter.
func TestContributionsEventsListRequestsUseDocumentedPerPageAndAdvancePages(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{
		genEvents(eventsPerPage, "example-org/repo-p1", base),
		genEvents(eventsPerPage, "example-org/repo-p2", base),
		genEvents(eventsPerPage, "example-org/repo-p3", base),
		genEvents(eventsPerPage, "example-org/repo-p4", base),
	}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	if _, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil)); err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	qs := srv.eventsListQueries(t)
	if len(qs) != 3 {
		t.Fatalf("got %d events-list requests, want 3", len(qs))
	}
	for i, q := range qs {
		if got := q.Get("per_page"); got != "100" {
			t.Errorf("request %d: per_page = %q, want \"100\"", i, got)
		}
	}
	wantPages := []string{"1", "2", "3"}
	for i, q := range qs {
		if got := q.Get("page"); got != wantPages[i] {
			t.Errorf("request %d: page = %q, want %q", i, got, wantPages[i])
		}
	}
}

func TestContributionsStopsPaginatingOnShortPage(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{ev("PushEvent", "example-org/example-repo", base)}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	if _, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil)); err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if n := srv.eventsListRequests(); n != 1 {
		t.Errorf("events list requests = %d, want 1 (short page must stop pagination)", n)
	}
}

// TestContributionsAccumulatesEventsAcrossPages proves the Events count is a
// real accumulation and not a constant: the same repo appears twice on page
// one and once on page two, so the collapsed entry must report 3.
func TestContributionsAccumulatesEventsAcrossPages(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	page1 := genEvents(eventsPerPage-2, "example-org/filler", base)
	page1 = append(page1,
		ev("PushEvent", "example-org/busy-repo", base.Add(98*time.Second)),
		ev("IssuesEvent", "example-org/busy-repo", base.Add(99*time.Second)),
	)
	pages := [][]event{
		page1,
		{ev("PullRequestEvent", "example-org/busy-repo", base.Add(time.Hour))},
	}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(1, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	// Limit=1 keeps only the most-recently-active repo, which is busy-repo
	// (its page-two event is the newest), collapsed into a single entry.
	want := contrib("example-org/busy-repo", 3, "octocat")
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want [%+v]", got, want)
	}
}

// TestContributionsExcludesSelfRepoUnconditionally proves the special GitHub
// profile repo ({user}/{user}) is always dropped - no config field controls
// it, and it must never reach the fork detail lookup.
func TestContributionsExcludesSelfRepoUnconditionally(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "octocat/octocat", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, true, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (self-repo must be excluded)", got)
	}
	if n := srv.repoDetailRequests(); n != 1 {
		t.Errorf("repo detail requests = %d, want 1 (self-repo must not be looked up)", n)
	}
}

// TestContributionsExcludesSelfRepoCaseInsensitively proves the self-repo
// match is case-insensitive, matching excluded()'s existing behavior for
// ExcludeRepos/ExcludeOrgs.
func TestContributionsExcludesSelfRepoCaseInsensitively(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "Octocat/Octocat", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}
	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (self-repo exclusion must be case-insensitive)", got)
	}
}

// TestContributionsSelfRepoExclusionRequiresBothOwnerAndNameMatch proves the
// self-repo guard in excluded() checks owner AND name against user, not
// either alone: a third party's repo named after the user
// (some-org/octocat) and the user's own differently-named repo
// (octocat/some-other-repo) are both real contributions, not the profile
// repo, and must not be excluded.
func TestContributionsSelfRepoExclusionRequiresBothOwnerAndNameMatch(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "some-org/octocat", base),
		ev("PushEvent", "octocat/some-other-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	want := map[string]bool{"some-org/octocat": false, "octocat/some-other-repo": false}
	for _, contrib := range got {
		if _, ok := want[contrib.Repo]; ok {
			want[contrib.Repo] = true
		}
	}
	for repo, found := range want {
		if !found {
			t.Errorf("got %+v, want %s present (self-repo exclusion requires both owner and name to match user)", got, repo)
		}
	}
}

func TestContributionsReturnsErrorFromEventsList(t *testing.T) {
	srv := newContribServerWithOpts(t, nil, nil, contribServerOpts{eventsStatus: http.StatusInternalServerError})
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err == nil {
		t.Fatal("Contributions: want error on non-200 events list response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

func TestContributionsReturnsErrorFromRepoDetailLookup(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{ev("PushEvent", "example-org/example-repo", base)}}
	srv := newContribServerWithOpts(t, pages, nil, contribServerOpts{reposStatus: http.StatusForbidden})
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, true, nil, nil))
	if err == nil {
		t.Fatal("Contributions: want error on non-200 repo detail response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

func TestContributionsDropsEventsWithEmptyRepoName(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	if len(got) != 1 || got[0].Repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (malformed empty repo name must be dropped)", got)
	}
}

// TestContributionsKeepsEventWithSlashlessRepoName pins excluded()'s current
// behavior on a malformed, slash-less repo full name reached through the
// public Contributions() path: it passes the earlier fullName == "" check,
// then excluded()'s "owner, _, ok := strings.Cut" guard reports !ok and
// returns false, so the event is kept rather than excluded. This is not an
// endorsement of that behavior over dropForks' sibling guard (which would
// skip an unsplittable candidate instead) - it only pins what excluded()
// does today, so a change to it is a deliberate, visible decision.
func TestContributionsKeepsEventWithSlashlessRepoName(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]event{{
		ev("PushEvent", "no-slash-name", base),
		ev("PushEvent", "example-org/example-repo", base),
	}}
	srv := newContribServer(t, pages, nil)
	c := newTestClient(t, srv)

	got, err := Contributions(context.Background(), c, "octocat", ghConfig(10, false, nil, nil))
	if err != nil {
		t.Fatalf("Contributions: %v", err)
	}

	want := []model.Contribution{
		contrib("no-slash-name", 1, "octocat"),
		contrib("example-org/example-repo", 1, "octocat"),
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
			}
		}
		if !found {
			t.Errorf("missing %+v in %+v (excluded() must keep a slash-less repo name, not treat it as excluded)", w, got)
		}
	}
}

// TestDropForksSkipsUnsplittableRepoName exercises dropForks directly:
// events always carry "owner/name" full names, so this guard is structurally
// unreachable through Contributions, but it is defensive against a malformed
// candidate and must not panic or misclassify.
func TestDropForksSkipsUnsplittableRepoName(t *testing.T) {
	srv := newContribServer(t, nil, map[string]bool{"/repos/example-org/example-repo": false})
	c := newTestClient(t, srv)

	in := []candidate{
		{repo: "no-slash-name", events: 1},
		{repo: "example-org/example-repo", events: 1},
	}
	got, err := dropForks(context.Background(), c, in)
	if err != nil {
		t.Fatalf("dropForks: %v", err)
	}

	if len(got) != 1 || got[0].repo != "example-org/example-repo" {
		t.Errorf("got %+v, want only example-org/example-repo (unsplittable name must be skipped, not looked up)", got)
	}
	if n := srv.repoDetailRequests(); n != 1 {
		t.Errorf("repo detail requests = %d, want 1 (unsplittable name must not reach the API)", n)
	}
}
