package fetch

import (
	"context"
	"strings"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// searchOverfetchMultiplier is hardcoded, not config-driven: post-filtering
// (ExcludeRepos/ExcludeOrgs) can drop the result below Limit, and adding a
// knob here risks breaking the one-search-call invariant or implying a
// top-up call that does not exist. This is the one place the search budget
// (10/min unauthenticated) is spent, ever, per PullRequests call.
const searchOverfetchMultiplier = 2

// maxSearchPerPage is the search API's own per_page ceiling.
const maxSearchPerPage = 100

// PullRequests searches for pull requests authored by user, most recently
// created first. gh.PullRequests == nil, or an explicit Limit of 0, means the
// section yields no items, so no request is made. Exactly one
// GET /search/issues call is ever issued.
func PullRequests(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.PullRequest, error) {
	if gh.PullRequests == nil {
		return nil, nil
	}
	limit := gh.PullRequests.Limit
	if limit == 0 {
		return nil, nil
	}

	perPage := limit * searchOverfetchMultiplier
	if perPage > maxSearchPerPage {
		perPage = maxSearchPerPage
	}

	result, _, err := c.gh.Search.Issues(ctx, "is:pr author:"+user, &github.SearchOptions{
		Sort:        "created",
		Order:       "desc",
		ListOptions: github.ListOptions{PerPage: perPage},
	})
	if err != nil {
		return nil, err
	}

	// Filtered then truncated in search order - never re-sorted, since the
	// search request already asked for sort=created&order=desc.
	out := make([]model.PullRequest, 0, limit)
	for _, issue := range result.Issues {
		if !issue.IsPullRequest() {
			continue
		}
		repo := repoFullNameFromURL(issue.GetRepositoryURL())
		if excluded(repo, user, gh) {
			continue
		}
		out = append(out, model.PullRequest{
			Repo:   repo,
			Number: issue.GetNumber(),
			Title:  issue.GetTitle(),
			URL:    issue.GetHTMLURL(),
			State:  issue.GetState(),
			// MergedAt is left at its zero value: /search/issues payloads
			// don't carry it, and a second per-PR call to fetch it would
			// spend exactly the budget this function exists to avoid.
		})
		if len(out) == limit {
			break
		}
	}
	return out, nil
}

// repoFullNameFromURL parses "owner/name" out of a repository_url like
// "https://api.github.com/repos/owner/name" - string parsing only, never a
// GET /repos call.
func repoFullNameFromURL(url string) string {
	const marker = "/repos/"
	i := strings.LastIndex(url, marker)
	if i == -1 {
		return ""
	}
	return url[i+len(marker):]
}
