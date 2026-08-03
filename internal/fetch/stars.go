package fetch

import (
	"context"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// starsPerPage bounds each GET /user/starred page.
const starsPerPage = 100

// Stars lists the user's starred repositories. gh.Stars == nil means the
// section was not requested, so no request is made.
func Stars(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Star, error) {
	if gh.Stars == nil {
		return nil, nil
	}
	limit := gh.Stars.Limit
	if limit == 0 {
		return nil, nil
	}

	out := make([]model.Star, 0, limit)
	opts := &github.ListOptions{PerPage: starsPerPage, Page: 1}
	for len(out) < limit {
		repos, _, err := c.gh.Activity.ListStarred(ctx, user, &github.ActivityListStarredOptions{
			ListOptions: *opts,
		})
		if err != nil {
			return nil, err
		}
		for _, starred := range repos {
			out = append(out, model.Star{
				Repo: model.Repo{
					Name:        starred.Repository.GetName(),
					FullName:    starred.Repository.GetFullName(),
					Description: starred.Repository.GetDescription(),
					URL:         starred.Repository.GetHTMLURL(),
					Language:    starred.Repository.GetLanguage(),
					Stars:       starred.Repository.GetStargazersCount(),
					PushedAt:    starred.Repository.GetPushedAt().Time,
				},
				StarredAt: starred.GetStarredAt().Time,
			})
			if len(out) == limit {
				break
			}
		}
		if len(repos) < starsPerPage {
			break
		}
		opts.Page++
	}
	return out, nil
}
