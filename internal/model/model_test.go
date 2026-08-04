package model

import (
	"encoding/json"
	"testing"
	"time"
)

// json.Unmarshal into map[string]any gives string for JSON strings (including
// RFC 3339 timestamps) and float64 for all JSON numbers. These helpers assert
// that shape, not just that a key is present — a key-presence check alone
// would not catch PushedAt silently becoming a string.

func assertString(t *testing.T, v any, field, want string) {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s: got %T (%#v), want string", field, v, v)
		return
	}
	if s != want {
		t.Errorf("%s: got %q, want %q", field, s, want)
	}
}

func assertNumber(t *testing.T, v any, field string, want float64) {
	t.Helper()
	n, ok := v.(float64)
	if !ok {
		t.Errorf("%s: got %T (%#v), want number", field, v, v)
		return
	}
	if n != want {
		t.Errorf("%s: got %v, want %v", field, n, want)
	}
}

func assertRFC3339(t *testing.T, v any, field string, want time.Time) {
	t.Helper()
	s, ok := v.(string)
	if !ok {
		t.Errorf("%s: got %T (%#v), want RFC3339 string", field, v, v)
		return
	}
	got, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Errorf("%s: %q does not parse as RFC3339: %v", field, s, err)
		return
	}
	if !got.Equal(want) {
		t.Errorf("%s: got %v, want %v", field, got, want)
	}
}

func TestDataJSONWireFormat(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)

	d := Data{
		User: User{
			Login:     "octocat",
			Name:      "Octo Cat",
			Bio:       "example bio",
			AvatarURL: "https://example.com/avatar.png",
		},
		GitHub: GitHub{
			Contributions: []Contribution{{Repo: "octocat/example-org", Events: 5, CommitsURL: "https://github.com/octocat/example-org/commits?author=octocat", ActivityURL: "https://github.com/octocat/example-org/issues?q=updated:30d+author:octocat"}},
			Repos: []Repo{{
				Name:        "example-org",
				FullName:    "octocat/example-org",
				Description: "an example repo",
				URL:         "https://example.com/octocat/example-org",
				Language:    "Go",
				Stars:       3,
				PushedAt:    now,
			}},
			Forks: []Repo{{Name: "forked-repo", FullName: "octocat/forked-repo", PushedAt: now}},
			PullRequests: []PullRequest{{
				Repo:     "octocat/example-org",
				Number:   1,
				Title:    "example pull request",
				URL:      "https://example.com/pr/1",
				State:    "merged",
				MergedAt: now,
			}},
			Stars: []Star{{
				Repo:      Repo{Name: "example-org"},
				StarredAt: now,
			}},
			Releases: []Release{{
				Repo:        "octocat/example-org",
				TagName:     "v1.0.0",
				Name:        "First release",
				URL:         "https://example.com/releases/v1.0.0",
				PublishedAt: now,
			}},
			Followers: []Follower{{
				Login:     "octocat",
				AvatarURL: "https://example.com/avatar.png",
				URL:       "https://example.com/octocat",
			}},
			Gists: []Gist{{
				ID:          "abc123",
				Description: "an example gist",
				URL:         "https://example.com/gists/abc123",
				CreatedAt:   now,
			}},
			TopProjects: []TopProject{{
				Repo:         "octocat/example-org",
				Commits:      3,
				PullRequests: 2,
				Reviews:      1,
				Score:        6,
			}},
		},
		Feeds: map[string][]FeedItem{
			"blog": {{
				Title:       "an example post",
				URL:         "https://example.com/posts/1",
				Published:   now,
				Description: "example description",
			}},
		},
		Now: now,
	}

	b, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal top level: %v", err)
	}

	for _, key := range []string{"user", "github", "feeds", "now"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("top level missing key %q", key)
		}
	}
	assertRFC3339(t, raw["now"], "now", now)

	user, ok := raw["user"].(map[string]any)
	if !ok {
		t.Fatalf("user is not an object")
	}
	for _, key := range []string{"login", "name", "bio", "avatar_url"} {
		if _, ok := user[key]; !ok {
			t.Errorf("user missing key %q", key)
		}
	}
	assertString(t, user["login"], "user.login", "octocat")
	assertString(t, user["name"], "user.name", "Octo Cat")
	assertString(t, user["bio"], "user.bio", "example bio")
	assertString(t, user["avatar_url"], "user.avatar_url", "https://example.com/avatar.png")

	gh, ok := raw["github"].(map[string]any)
	if !ok {
		t.Fatalf("github is not an object")
	}
	for _, key := range []string{
		"contributions", "repos", "forks", "pull_requests",
		"stars", "releases", "followers", "gists", "top_projects",
	} {
		if _, ok := gh[key]; !ok {
			t.Errorf("github missing key %q", key)
		}
	}

	contributions, ok := gh["contributions"].([]any)
	if !ok || len(contributions) != 1 {
		t.Fatalf("github.contributions unexpected: %v", gh["contributions"])
	}
	contribution, ok := contributions[0].(map[string]any)
	if !ok {
		t.Fatalf("contribution is not an object")
	}
	for _, key := range []string{"repo", "events", "commits_url", "activity_url"} {
		if _, ok := contribution[key]; !ok {
			t.Errorf("contribution missing key %q", key)
		}
	}
	assertString(t, contribution["repo"], "contribution.repo", "octocat/example-org")
	assertNumber(t, contribution["events"], "contribution.events", 5)
	assertString(t, contribution["commits_url"], "contribution.commits_url", "https://github.com/octocat/example-org/commits?author=octocat")
	assertString(t, contribution["activity_url"], "contribution.activity_url", "https://github.com/octocat/example-org/issues?q=updated:30d+author:octocat")

	repos, ok := gh["repos"].([]any)
	if !ok || len(repos) != 1 {
		t.Fatalf("github.repos unexpected: %v", gh["repos"])
	}
	repo, ok := repos[0].(map[string]any)
	if !ok {
		t.Fatalf("repo is not an object")
	}
	for _, key := range []string{
		"name", "full_name", "description", "url", "language", "stars", "pushed_at",
	} {
		if _, ok := repo[key]; !ok {
			t.Errorf("repo missing key %q", key)
		}
	}
	assertString(t, repo["name"], "repo.name", "example-org")
	assertString(t, repo["full_name"], "repo.full_name", "octocat/example-org")
	assertString(t, repo["description"], "repo.description", "an example repo")
	assertString(t, repo["url"], "repo.url", "https://example.com/octocat/example-org")
	assertString(t, repo["language"], "repo.language", "Go")
	assertNumber(t, repo["stars"], "repo.stars", 3)
	assertRFC3339(t, repo["pushed_at"], "repo.pushed_at", now)

	forks, ok := gh["forks"].([]any)
	if !ok || len(forks) != 1 {
		t.Fatalf("github.forks unexpected: %v", gh["forks"])
	}
	fork, ok := forks[0].(map[string]any)
	if !ok {
		t.Fatalf("fork is not an object")
	}
	assertString(t, fork["name"], "fork.name", "forked-repo")
	assertString(t, fork["full_name"], "fork.full_name", "octocat/forked-repo")
	assertRFC3339(t, fork["pushed_at"], "fork.pushed_at", now)

	prs, ok := gh["pull_requests"].([]any)
	if !ok || len(prs) != 1 {
		t.Fatalf("github.pull_requests unexpected: %v", gh["pull_requests"])
	}
	pr, ok := prs[0].(map[string]any)
	if !ok {
		t.Fatalf("pull request is not an object")
	}
	for _, key := range []string{"repo", "number", "title", "url", "state", "merged_at"} {
		if _, ok := pr[key]; !ok {
			t.Errorf("pull_request missing key %q", key)
		}
	}
	assertString(t, pr["repo"], "pull_request.repo", "octocat/example-org")
	assertNumber(t, pr["number"], "pull_request.number", 1)
	assertString(t, pr["title"], "pull_request.title", "example pull request")
	assertString(t, pr["url"], "pull_request.url", "https://example.com/pr/1")
	assertString(t, pr["state"], "pull_request.state", "merged")
	assertRFC3339(t, pr["merged_at"], "pull_request.merged_at", now)

	stars, ok := gh["stars"].([]any)
	if !ok || len(stars) != 1 {
		t.Fatalf("github.stars unexpected: %v", gh["stars"])
	}
	star, ok := stars[0].(map[string]any)
	if !ok {
		t.Fatalf("star is not an object")
	}
	for _, key := range []string{"repo", "starred_at"} {
		if _, ok := star[key]; !ok {
			t.Errorf("star missing key %q", key)
		}
	}
	starRepo, ok := star["repo"].(map[string]any)
	if !ok {
		t.Fatalf("star.repo is not an object")
	}
	assertString(t, starRepo["name"], "star.repo.name", "example-org")
	assertRFC3339(t, star["starred_at"], "star.starred_at", now)

	releases, ok := gh["releases"].([]any)
	if !ok || len(releases) != 1 {
		t.Fatalf("github.releases unexpected: %v", gh["releases"])
	}
	release, ok := releases[0].(map[string]any)
	if !ok {
		t.Fatalf("release is not an object")
	}
	for _, key := range []string{"repo", "tag_name", "name", "url", "published_at"} {
		if _, ok := release[key]; !ok {
			t.Errorf("release missing key %q", key)
		}
	}
	assertString(t, release["repo"], "release.repo", "octocat/example-org")
	assertString(t, release["tag_name"], "release.tag_name", "v1.0.0")
	assertString(t, release["name"], "release.name", "First release")
	assertString(t, release["url"], "release.url", "https://example.com/releases/v1.0.0")
	assertRFC3339(t, release["published_at"], "release.published_at", now)

	followers, ok := gh["followers"].([]any)
	if !ok || len(followers) != 1 {
		t.Fatalf("github.followers unexpected: %v", gh["followers"])
	}
	follower, ok := followers[0].(map[string]any)
	if !ok {
		t.Fatalf("follower is not an object")
	}
	for _, key := range []string{"login", "avatar_url", "url"} {
		if _, ok := follower[key]; !ok {
			t.Errorf("follower missing key %q", key)
		}
	}
	assertString(t, follower["login"], "follower.login", "octocat")
	assertString(t, follower["avatar_url"], "follower.avatar_url", "https://example.com/avatar.png")
	assertString(t, follower["url"], "follower.url", "https://example.com/octocat")

	gists, ok := gh["gists"].([]any)
	if !ok || len(gists) != 1 {
		t.Fatalf("github.gists unexpected: %v", gh["gists"])
	}
	gist, ok := gists[0].(map[string]any)
	if !ok {
		t.Fatalf("gist is not an object")
	}
	for _, key := range []string{"id", "description", "url", "created_at"} {
		if _, ok := gist[key]; !ok {
			t.Errorf("gist missing key %q", key)
		}
	}
	assertString(t, gist["id"], "gist.id", "abc123")
	assertString(t, gist["description"], "gist.description", "an example gist")
	assertString(t, gist["url"], "gist.url", "https://example.com/gists/abc123")
	assertRFC3339(t, gist["created_at"], "gist.created_at", now)

	topProjects, ok := gh["top_projects"].([]any)
	if !ok || len(topProjects) != 1 {
		t.Fatalf("github.top_projects unexpected: %v", gh["top_projects"])
	}
	topProject, ok := topProjects[0].(map[string]any)
	if !ok {
		t.Fatalf("top_project is not an object")
	}
	for _, key := range []string{"repo", "commits", "pull_requests", "reviews", "score"} {
		if _, ok := topProject[key]; !ok {
			t.Errorf("top_project missing key %q", key)
		}
	}
	assertString(t, topProject["repo"], "top_project.repo", "octocat/example-org")
	assertNumber(t, topProject["commits"], "top_project.commits", 3)
	assertNumber(t, topProject["pull_requests"], "top_project.pull_requests", 2)
	assertNumber(t, topProject["reviews"], "top_project.reviews", 1)
	assertNumber(t, topProject["score"], "top_project.score", 6)

	feeds, ok := raw["feeds"].(map[string]any)
	if !ok {
		t.Fatalf("feeds is not an object")
	}
	blog, ok := feeds["blog"].([]any)
	if !ok || len(blog) != 1 {
		t.Fatalf("feeds.blog unexpected: %v", feeds["blog"])
	}
	item, ok := blog[0].(map[string]any)
	if !ok {
		t.Fatalf("feed item is not an object")
	}
	for _, key := range []string{"title", "url", "published", "description"} {
		if _, ok := item[key]; !ok {
			t.Errorf("feed item missing key %q", key)
		}
	}
	assertString(t, item["title"], "feed_item.title", "an example post")
	assertString(t, item["url"], "feed_item.url", "https://example.com/posts/1")
	assertRFC3339(t, item["published"], "feed_item.published", now)
	assertString(t, item["description"], "feed_item.description", "example description")
}

func TestDataZeroValueMarshals(t *testing.T) {
	b, err := json.Marshal(Data{})
	if err != nil {
		t.Fatalf("Marshal zero value: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatalf("Unmarshal zero value: %v", err)
	}

	// A nil Feeds map is a tested, documented decision: it renders as
	// JSON null rather than an empty object.
	feedsRaw, ok := raw["feeds"]
	if !ok {
		t.Fatalf("zero-value Data missing feeds key")
	}
	if feedsRaw != nil {
		t.Errorf("zero-value Feeds = %#v, want JSON null", feedsRaw)
	}
}
