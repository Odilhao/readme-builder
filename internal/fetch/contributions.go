package fetch

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/go-github/v89/github"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// eventsPerPage and maxEventPages bound the events list at exactly the
// endpoint's own ceiling (~300 events across public timelines), never on
// config: this is a property of the API, not something a user can dial up.
const (
	eventsPerPage = 100
	maxEventPages = 3
)

// contributionEventTypes are the event types that represent someone doing
// work, mirroring what GraphQL's contributionsCollection covers (commits,
// issues, pull requests, reviews, releases, repo creation) plus the closest
// REST equivalents. WatchEvent (starring) and ForkEvent are deliberately
// absent per the issue: starring or forking is not working on something.
// Administrative/noise events — DeleteEvent, MemberEvent, PublicEvent,
// GollumEvent and friends — are excluded too: they describe changes to a
// repo's state, not a contribution by the actor.
var contributionEventTypes = map[string]bool{
	"PushEvent":                     true,
	"PullRequestEvent":              true,
	"PullRequestReviewEvent":        true,
	"PullRequestReviewCommentEvent": true,
	"IssuesEvent":                   true,
	"IssueCommentEvent":             true,
	"CommitCommentEvent":            true,
	"CreateEvent":                   true,
	"ReleaseEvent":                  true,
}

// candidate accumulates per-repo activity before the fork check and the
// Limit cap are applied.
type candidate struct {
	repo       string
	events     int
	maxCreated time.Time
}

// Contributions derives contribution-shaped activity from
// GET /users/{u}/events/public, grouped by repository and sorted by most
// recent activity first. gh.Contributions == nil means the section was not
// requested at all, so no request is made.
func Contributions(ctx context.Context, c *Client, user string, gh config.GitHub) ([]model.Contribution, error) {
	if gh.Contributions == nil {
		return nil, nil
	}

	events, err := listPublicEvents(ctx, c, user)
	if err != nil {
		return nil, err
	}

	candidates := groupByRepo(events, gh)
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].maxCreated.After(candidates[j].maxCreated)
	})

	limit := gh.Contributions.Limit
	if limit < len(candidates) {
		candidates = candidates[:limit]
	}

	if gh.ExcludeForks {
		candidates, err = dropForks(ctx, c, candidates)
		if err != nil {
			return nil, err
		}
	}

	out := make([]model.Contribution, 0, len(candidates))
	for _, cand := range candidates {
		out = append(out, model.Contribution{Repo: cand.repo, Events: cand.events})
	}
	return out, nil
}

// listPublicEvents pages through the public events timeline up to the
// endpoint's own ~300-event ceiling, stopping early on a short (final) page.
func listPublicEvents(ctx context.Context, c *Client, user string) ([]*github.Event, error) {
	var all []*github.Event
	// Page starts at 1, not 0: a zero Page is omitted from the request
	// entirely (GitHub defaults that to page 1), so starting the counter at
	// 0 and incrementing after the first request would re-request page 1
	// instead of advancing to page 2.
	opts := &github.ListOptions{PerPage: eventsPerPage, Page: 1}
	for page := 0; page < maxEventPages; page++ {
		events, _, err := c.gh.Activity.ListEventsPerformedByUser(ctx, user, true, opts)
		if err != nil {
			return nil, err
		}
		all = append(all, events...)
		if len(events) < eventsPerPage {
			break
		}
		opts.Page++
	}
	return all, nil
}

// groupByRepo filters to contribution-shaped events, applies the name-only
// exclusions (ExcludeRepos, ExcludeOrgs), and accumulates per-repo event
// counts and the max CreatedAt. Excluded repos are dropped entirely here,
// before any detail lookup, per the issue.
func groupByRepo(events []*github.Event, gh config.GitHub) []candidate {
	byRepo := make(map[string]*candidate)
	var order []string

	for _, e := range events {
		if !contributionEventTypes[e.GetType()] {
			continue
		}
		fullName := e.GetRepo().GetName()
		if fullName == "" || excluded(fullName, gh) {
			continue
		}

		cand, ok := byRepo[fullName]
		if !ok {
			cand = &candidate{repo: fullName}
			byRepo[fullName] = cand
			order = append(order, fullName)
		}
		cand.events++
		if created := e.GetCreatedAt().Time; created.After(cand.maxCreated) {
			cand.maxCreated = created
		}
	}

	out := make([]candidate, 0, len(order))
	for _, name := range order {
		out = append(out, *byRepo[name])
	}
	return out
}

// excluded reports whether fullName ("owner/name") matches ExcludeRepos or
// its owner matches ExcludeOrgs. Matching is case-insensitive: GitHub logins
// and repo names are case-insensitive even though they preserve case.
func excluded(fullName string, gh config.GitHub) bool {
	for _, r := range gh.ExcludeRepos {
		if strings.EqualFold(r, fullName) {
			return true
		}
	}
	owner, _, ok := strings.Cut(fullName, "/")
	if !ok {
		return false
	}
	for _, o := range gh.ExcludeOrgs {
		if strings.EqualFold(o, owner) {
			return true
		}
	}
	return false
}

// dropForks looks up each candidate's fork flag, which the events payload
// never carries, and removes it if set. The caller has already trimmed
// candidates to at most Limit, so this makes at most Limit requests — never
// more lookups than there are slots in the eventual output.
func dropForks(ctx context.Context, c *Client, candidates []candidate) ([]candidate, error) {
	out := make([]candidate, 0, len(candidates))
	for _, cand := range candidates {
		owner, name, ok := strings.Cut(cand.repo, "/")
		if !ok {
			// Defensive only: every candidate.repo comes from an event's
			// repo.name, which GitHub always sends as "owner/name". Kept so a
			// malformed candidate is skipped rather than looked up wrong.
			continue
		}
		repo, _, err := c.gh.Repositories.Get(ctx, owner, name)
		if err != nil {
			return nil, err
		}
		if repo.GetFork() {
			continue
		}
		out = append(out, cand)
	}
	return out, nil
}
