package fetch

import (
	"context"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// followersPerPage bounds each GET /users/{u}/followers page.
const followersPerPage = 100

// Followers lists the user's followers. gh.Followers == nil means the
// section was not requested, so no request is made.
func Followers(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Follower, error) {
	if gh.Followers == nil {
		return nil, nil
	}
	limit := gh.Followers.Limit
	if limit == 0 {
		return nil, nil
	}

	out := make([]model.Follower, 0, limit)
	opts := &github.ListOptions{PerPage: followersPerPage, Page: 1}
	for len(out) < limit {
		users, _, err := c.gh.Users.ListFollowers(ctx, user, opts)
		if err != nil {
			return nil, err
		}
		for _, u := range users {
			out = append(out, model.Follower{
				Login:     u.GetLogin(),
				AvatarURL: u.GetAvatarURL(),
				URL:       u.GetHTMLURL(),
			})
			if len(out) == limit {
				break
			}
		}
		if len(users) < followersPerPage {
			break
		}
		opts.Page++
	}
	return out, nil
}
