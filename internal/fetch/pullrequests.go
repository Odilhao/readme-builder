package fetch

import (
	"context"
	"strings"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// searchOverfetchMultiplier is hardcoded, not config-driven. This is the one
// place the search budget (10/min unauthenticated) is spent, per PullRequests call.
const searchOverfetchMultiplier = 2

// maxSearchPerPage is the search API's own per_page ceiling.
const maxSearchPerPage = 100

// maxSearchPages limits pagination to protect the budget: GitHub's search API
// allows 10 unauthenticated requests/minute. PullRequests paginates to handle
// cases where exclude_repos/exclude_orgs filter out enough results from the
// first page, but stays bounded: worst case 3 GET /search/issues calls per
// PullRequests invocation still leaves budget for other fetchers in a single run.
const maxSearchPages = 3

// PullRequests searches for pull requests authored by user, most recently
// created first. gh.PullRequests == nil, or an explicit Limit of 0, means the
// section yields no items, so no request is made. Pages through results,
// bounded by maxSearchPages, to collect Limit items even when exclude_repos
// and exclude_orgs filter out many results from early pages.
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

	// Filtered then truncated in search order - never re-sorted, since the
	// search request already asked for sort=created&order=desc.
	out := make([]model.PullRequest, 0, limit)
	// Page starts at 1: a zero Page is omitted from the query entirely
	// (GitHub defaults an absent page to 1), so starting at 0 and
	// incrementing after the first request would re-request page 1.
	for page := 1; page <= maxSearchPages; page++ {
		result, _, err := c.gh.Search.Issues(ctx, "is:pr author:"+user, &github.SearchOptions{
			Sort:        "created",
			Order:       "desc",
			ListOptions: github.ListOptions{PerPage: perPage, Page: page},
		})
		if err != nil {
			return nil, err
		}

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
		// If we've reached the limit, stop paging.
		if len(out) == limit {
			break
		}
		// If this page had fewer items than requested, it's the last page.
		if len(result.Issues) < perPage {
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
