package fetch

import (
	"context"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// gistsPerPage bounds each GET /users/{u}/gists page.
const gistsPerPage = 100

// Gists lists the user's gists. gh.Gists == nil means the section was not
// requested, so no request is made.
func Gists(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Gist, error) {
	if gh.Gists == nil {
		return nil, nil
	}
	limit := gh.Gists.Limit
	if limit == 0 {
		return nil, nil
	}

	out := make([]model.Gist, 0, limit)
	opts := &github.GistListOptions{
		ListOptions: github.ListOptions{PerPage: gistsPerPage, Page: 1},
	}
	for len(out) < limit {
		gists, _, err := c.gh.Gists.List(ctx, user, opts)
		if err != nil {
			return nil, err
		}
		for _, g := range gists {
			out = append(out, model.Gist{
				ID:          g.GetID(),
				Description: g.GetDescription(),
				URL:         g.GetHTMLURL(),
				CreatedAt:   g.GetCreatedAt().Time,
			})
			if len(out) == limit {
				break
			}
		}
		if len(gists) < gistsPerPage {
			break
		}
		opts.ListOptions.Page++
	}
	return out, nil
}
