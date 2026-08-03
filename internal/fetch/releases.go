package fetch

import (
	"context"
	"strings"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// releasesPerPage bounds each GET /repos/{owner}/{repo}/releases page.
const releasesPerPage = 100

// maxReleasesReposScan caps the number of repos scanned for releases,
// independent of how many releases are found. This bounds the request count to
// O(1) + maxReleasesReposScan, preventing unbounded API budget consumption
// when sparse or absent releases are found across many owned repos.
const maxReleasesReposScan = 10

// Releases lists the user's own repositories' releases. Releases are fetched
// from the user's own repositories (not forks or contributed-to repos), sorted
// by recency (pushed_at descending), to stay within the API budget — checking
// each repo costs one request. Scans at most maxReleasesReposScan repos, and
// stops early if the release limit is reached. gh.Releases == nil means the
// section was not requested, so no request is made.
func Releases(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Release, error) {
	if gh.Releases == nil {
		return nil, nil
	}
	limit := gh.Releases.Limit
	if limit == 0 {
		return nil, nil
	}

	// Fetch the user's own repos to check for releases, sorted by recency.
	repos, err := listOwnRepos(ctx, c, user, gh, maxReleasesReposScan)
	if err != nil {
		return nil, err
	}

	out := make([]model.Release, 0, limit)
	for _, repo := range repos {
		if len(out) >= limit {
			break
		}

		owner, name, ok := strings.Cut(repo, "/")
		if !ok {
			continue
		}

		// Fetch releases from this repo.
		opts := &github.ListOptions{PerPage: releasesPerPage, Page: 1}
		for len(out) < limit {
			releases, _, err := c.gh.Repositories.ListReleases(ctx, owner, name, opts)
			if err != nil {
				return nil, err
			}
			for _, rel := range releases {
				out = append(out, model.Release{
					Repo:        repo,
					TagName:     rel.GetTagName(),
					Name:        rel.GetName(),
					URL:         rel.GetHTMLURL(),
					PublishedAt: rel.GetPublishedAt().Time,
				})
				if len(out) == limit {
					break
				}
			}
			if len(releases) < releasesPerPage {
				break
			}
			opts.Page++
		}
	}
	return out, nil
}

// listOwnRepos fetches the user's own repositories (not forks), sorted by
// pushed_at descending (most recently active first), returning at most maxRepos
// full names. Forks are filtered by the fork field alone; exclusion filters are
// not applied here. This bounds the repo-scan cost for release checks to at most
// maxRepos API requests.
func listOwnRepos(ctx context.Context, c *Client, user string, gh config.GitHub, maxRepos int) ([]string, error) {
	var out []string
	// Page starts at 1: a zero Page is omitted from the query entirely, so
	// starting at 0 and incrementing after would re-request page 1.
	opts := &github.RepositoryListByUserOptions{
		Type:        "owner",
		Sort:        "pushed",
		Direction:   "desc",
		ListOptions: github.ListOptions{PerPage: reposPerPage, Page: 1},
	}
	for len(out) < maxRepos {
		repos, _, err := c.gh.Repositories.ListByUser(ctx, user, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.GetFork() {
				continue
			}
			out = append(out, r.GetFullName())
			if len(out) >= maxRepos {
				break
			}
		}
		if len(repos) < reposPerPage {
			break
		}
		opts.Page++
	}
	return out, nil
}
