package fetch

import (
	"context"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// reposPerPage bounds each GET /users/{u}/repos page; unlike search there is
// no tight per-minute budget here, so this just keeps pagination efficient.
const reposPerPage = 100

// Repos lists the user's own, non-fork repositories. gh.Repos == nil means
// the section was not requested, so no request is made.
func Repos(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Repo, error) {
	if gh.Repos == nil {
		return nil, nil
	}
	return listRepos(ctx, c, user, gh, gh.Repos.Limit, false)
}

// Forks lists the user's forked repositories. gh.Forks == nil means the
// section was not requested, so no request is made.
func Forks(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Repo, error) {
	if gh.Forks == nil {
		return nil, nil
	}
	return listRepos(ctx, c, user, gh, gh.Forks.Limit, true)
}

// listRepos pages GET /users/{u}/repos, applies the name-only exclusions
// (ExcludeRepos, ExcludeOrgs) and a wantFork filter using the list response's
// own Fork field - no per-repo GET /repos lookup is ever needed here, unlike
// Contributions' events-derived candidates which lack that field. Stops once
// limit items are collected or a short page signals the last one.
func listRepos(ctx context.Context, c *Client, user string, gh config.GitHub, limit int, wantFork bool) ([]model.Repo, error) {
	if limit == 0 {
		return nil, nil
	}

	out := make([]model.Repo, 0, limit)
	// Page starts at 1: a zero Page is omitted from the query entirely
	// (GitHub defaults an absent page to 1), so starting at 0 and
	// incrementing after the first request would re-request page 1.
	opts := &github.RepositoryListByUserOptions{
		Type:        "owner",
		Sort:        "created",
		ListOptions: github.ListOptions{PerPage: reposPerPage, Page: 1},
	}
	for len(out) < limit {
		repos, _, err := c.gh.Repositories.ListByUser(ctx, user, opts)
		if err != nil {
			return nil, err
		}
		for _, r := range repos {
			if r.GetFork() != wantFork || excluded(r.GetFullName(), gh) {
				continue
			}
			out = append(out, model.Repo{
				Name:        r.GetName(),
				FullName:    r.GetFullName(),
				Description: r.GetDescription(),
				URL:         r.GetHTMLURL(),
				Language:    r.GetLanguage(),
				Stars:       r.GetStargazersCount(),
				PushedAt:    r.GetPushedAt().Time,
			})
			if len(out) == limit {
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
