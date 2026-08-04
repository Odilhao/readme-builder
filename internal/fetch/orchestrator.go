package fetch

import (
	"context"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// Fetch walks a resolved config, calls each configured fetcher, and assembles
// the results into a model.Data value. A nil GitHub section means the section
// was not requested at all, so no call is made. Feeds is treated similarly:
// a nil map means no feeds were configured.
//
// If any fetcher returns an error, Fetch stops and returns that error.
func Fetch(ctx context.Context, c *Client, cfg *config.Config) (*model.Data, error) {
	data := &model.Data{
		User: model.User{
			Login: cfg.Username,
		},
		Now: time.Now().UTC(),
	}

	// Fetch GitHub sections only if they are configured (non-nil).
	if cfg.GitHub.Contributions != nil {
		contributions, err := Contributions(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Contributions = contributions
	}

	if cfg.GitHub.Repos != nil {
		repos, err := Repos(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Repos = repos
	}

	if cfg.GitHub.Forks != nil {
		forks, err := Forks(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Forks = forks
	}

	if cfg.GitHub.PullRequests != nil {
		prs, err := PullRequests(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.PullRequests = prs
	}

	if cfg.GitHub.Stars != nil {
		stars, err := Stars(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Stars = stars
	}

	if cfg.GitHub.Releases != nil {
		releases, err := Releases(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Releases = releases
	}

	if cfg.GitHub.Followers != nil {
		followers, err := Followers(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Followers = followers
	}

	if cfg.GitHub.Gists != nil {
		gists, err := Gists(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.Gists = gists
	}

	if cfg.GitHub.TopProjects != nil {
		topProjects, err := TopProjects(ctx, c, cfg.Username, cfg.GitHub)
		if err != nil {
			return nil, err
		}
		data.GitHub.TopProjects = topProjects
	}

	// Fetch feeds only if configured (non-nil map).
	if cfg.Feeds != nil {
		feeds, err := Feeds(ctx, cfg.Feeds)
		if err != nil {
			return nil, err
		}
		data.Feeds = feeds
	}

	return data, nil
}
