package fetch

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// topProjectCounts accumulates per-repo activity across the three sources
// before scoring, sorting and the Limit cap are applied.
type topProjectCounts struct {
	repo         string
	commits      int
	pullRequests int
	reviews      int
}

// TopProjects ranks repos by summed activity (commits + pull requests +
// reviews) over gh.TopProjects.TimeWindow. gh.TopProjects == nil, or an
// explicit Limit of 0, means the section yields no items, so no request is
// made.
//
// Exactly 2 fixed, single-page search calls are spent here, independent of
// repo count or result volume - no pagination loop, per the invariant-9
// budget agreed at intake (10 unauthenticated search requests/minute, shared
// with PullRequests' own up-to-3 calls). Review counts come from the public
// events timeline (already the mechanism Contributions uses for
// PullRequestReviewEvent), a GET /users/{u}/events/public call rather than a
// third search endpoint.
func TopProjects(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.TopProject, error) {
	if gh.TopProjects == nil {
		return nil, nil
	}
	limit := gh.TopProjects.Limit
	if limit == 0 {
		return nil, nil
	}

	since := topProjectsSince(gh.TopProjects.TimeWindow, time.Now().UTC())

	counts := make(map[string]*topProjectCounts)
	var order []string
	get := func(repo string) *topProjectCounts {
		cnt, ok := counts[repo]
		if !ok {
			cnt = &topProjectCounts{repo: repo}
			counts[repo] = cnt
			order = append(order, repo)
		}
		return cnt
	}

	commitResult, _, err := c.gh.Search.Commits(ctx, "author:"+user+" author-date:>"+since, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: maxSearchPerPage},
	})
	if err != nil {
		return nil, err
	}
	for _, cr := range commitResult.Commits {
		repo := cr.GetRepository().GetFullName()
		if repo == "" || excluded(repo, user, gh) {
			continue
		}
		get(repo).commits++
	}

	issuesResult, _, err := c.gh.Search.Issues(ctx, "is:pr author:"+user+" created:>"+since, &github.SearchOptions{
		ListOptions: github.ListOptions{PerPage: maxSearchPerPage},
	})
	if err != nil {
		return nil, err
	}
	for _, issue := range issuesResult.Issues {
		if !issue.IsPullRequest() {
			continue
		}
		repo := repoFullNameFromURL(issue.GetRepositoryURL())
		if repo == "" || excluded(repo, user, gh) {
			continue
		}
		get(repo).pullRequests++
	}

	events, err := listPublicEvents(ctx, c, user)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		eventType := e.GetType()
		repo := e.GetRepo().GetName()
		if repo == "" || excluded(repo, user, gh) {
			continue
		}
		switch eventType {
		case "PullRequestReviewEvent":
			get(repo).reviews++
		case "PushEvent":
			// Mining PushEvent to widen candidate pool (no commit count
			// increment, as PushEvent payloads lack authoritative counts).
			get(repo)
		}
	}

	out := make([]model.TopProject, 0, len(order))
	for _, repo := range order {
		cnt := counts[repo]
		out = append(out, model.TopProject{
			Repo:         cnt.repo,
			Commits:      cnt.commits,
			PullRequests: cnt.pullRequests,
			Reviews:      cnt.reviews,
			Score:        cnt.commits + cnt.pullRequests + cnt.reviews,
		})
	}
	// Ties keep the order repos were first observed in (commits, then PRs,
	// then reviews), via SliceStable.
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if limit < len(out) {
		out = out[:limit]
	}

	// Fetch exact commit counts for the top-Limit candidates and recompute scores
	until := time.Now().UTC()
	out, err = exactCommitCounts(ctx, c, user, out, since, until)
	if err != nil {
		return nil, err
	}

	return out, nil
}

// exactCommitCounts fetches exact commit counts for each repo in the input
// list and recomputes scores, re-sorting by score (stable sort for ties).
// Each exact count is fetched via GET /repos/{owner}/{repo}/commits with
// Author, Since, Until, and PerPage=1, reading the page number from the
// Link: rel="last" header (Response.LastPage field).
func exactCommitCounts(ctx context.Context, c *Client, user string, candidates []model.TopProject, sinceStr string, until time.Time) ([]model.TopProject, error) {
	since, err := time.Parse("2006-01-02", sinceStr)
	if err != nil {
		// Fallback to 30 days if parsing fails, but should not happen
		since = until.AddDate(0, 0, -30)
	}

	for i, cand := range candidates {
		parts := strings.Split(cand.Repo, "/")
		if len(parts) != 2 {
			continue // defensive; should not happen
		}
		owner, repo := parts[0], parts[1]

		// Fetch commits with per_page=1 to get Link header with LastPage
		commits, resp, err := c.gh.Repositories.ListCommits(ctx, owner, repo, &github.CommitsListOptions{
			Author:      user,
			Since:       since,
			Until:       until,
			ListOptions: github.ListOptions{PerPage: 1},
		})
		if err != nil {
			return nil, err
		}

		// Determine exact count from Response.LastPage
		var exactCount int
		if resp.LastPage > 0 {
			// Multiple pages: exact count is LastPage
			exactCount = resp.LastPage
		} else if len(commits) > 0 {
			// Single page with one or more items: exact count is len(commits)
			exactCount = len(commits)
		}
		// else: no items and no LastPage = 0 commits

		candidates[i].Commits = exactCount
		candidates[i].Score = exactCount + candidates[i].PullRequests + candidates[i].Reviews
	}

	// Re-sort by recomputed Score (stable sort for tie order)
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].Score > candidates[j].Score
	})

	return candidates, nil
}

// topProjectsSince converts a validated Nd/Nw/Ny time-window string (format
// enforced by config's own time_window validation at load time, shared with
// Contributions per #72) into a "YYYY-MM-DD" cutoff for the search
// qualifiers' ">" comparison. An empty window defaults to 30 days, mirroring
// Contributions' own default.
func topProjectsSince(window string, now time.Time) string {
	if window == "" {
		window = "30d"
	}
	n, err := strconv.Atoi(window[:len(window)-1])
	if err != nil {
		n = 30
	}
	var d time.Duration
	switch window[len(window)-1] {
	case 'w':
		d = time.Duration(n) * 7 * 24 * time.Hour
	case 'y':
		d = time.Duration(n) * 365 * 24 * time.Hour
	default: // 'd' and any unexpected suffix
		d = time.Duration(n) * 24 * time.Hour
	}
	return now.Add(-d).Format("2006-01-02")
}
