package model

import "time"

// Data is the complete set of resolved data handed to a template render.
type Data struct {
	User   User                  `json:"user"`
	GitHub GitHub                `json:"github"`
	Feeds  map[string][]FeedItem `json:"feeds"`
	// Now is the fetch time; templates flatten it via humanize before use.
	Now time.Time `json:"now"`
}

type User struct {
	Login     string `json:"login"`
	Name      string `json:"name"`
	Bio       string `json:"bio"`
	AvatarURL string `json:"avatar_url"`
}

type GitHub struct {
	Contributions []Contribution `json:"contributions"`
	Repos         []Repo         `json:"repos"`
	Forks         []Repo         `json:"forks"`
	PullRequests  []PullRequest  `json:"pull_requests"`
	Stars         []Star         `json:"stars"`
	Releases      []Release      `json:"releases"`
	Followers     []Follower     `json:"followers"`
	Gists         []Gist         `json:"gists"`
	TopProjects   []TopProject   `json:"top_projects"`
}

type Contribution struct {
	Repo        string `json:"repo"`
	Events      int    `json:"events"`
	CommitsURL  string `json:"commits_url"`
	ActivityURL string `json:"activity_url"`
	// Events counts observed events per repo from GET /users/{u}/events/public.
	// That API retains only ~90 days and 300 events, so this is a floor, not
	// a lifetime total.
}

// TopProject is a repository ranked by summed activity (commits + pull
// requests + reviews) over a configured time window. Score is the sum of the
// other three counts, computed once in fetch rather than in the template,
// per invariant 3.
type TopProject struct {
	Repo         string `json:"repo"`
	Commits      int    `json:"commits"`
	PullRequests int    `json:"pull_requests"`
	Reviews      int    `json:"reviews"`
	Score        int    `json:"score"`
}

type Repo struct {
	Name        string    `json:"name"`
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	Language    string    `json:"language"`
	Stars       int       `json:"stars"`
	PushedAt    time.Time `json:"pushed_at"`
}

type PullRequest struct {
	Repo     string    `json:"repo"`
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	State    string    `json:"state"`
	MergedAt time.Time `json:"merged_at"`
}

type Star struct {
	Repo      Repo      `json:"repo"`
	StarredAt time.Time `json:"starred_at"`
}

type Release struct {
	Repo        string    `json:"repo"`
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
}

type Follower struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	URL       string `json:"url"`
}

type Gist struct {
	ID          string    `json:"id"`
	Description string    `json:"description"`
	URL         string    `json:"url"`
	CreatedAt   time.Time `json:"created_at"`
}

type FeedItem struct {
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Published   time.Time `json:"published"`
	Description string    `json:"description"`
}
