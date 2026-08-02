package fetch

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/mmcdole/gofeed"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// Feeds parses every configured feed and returns up to Limit items per feed,
// most recently published first. A feed with Limit == 0 is skipped entirely -
// no parse call is made for it. Feeds carry no credential of any kind.
// One feed's fetch/parse error aborts the whole call, matching Contributions'
// fail-fast precedent rather than silently degrading.
func Feeds(ctx context.Context, feeds map[string]config.Feed) (map[string][]model.FeedItem, error) {
	if len(feeds) == 0 {
		return nil, nil
	}

	// Sorted name order keeps behaviour (and test assertions) deterministic.
	names := make([]string, 0, len(feeds))
	for name := range feeds {
		names = append(names, name)
	}
	sort.Strings(names)

	parser := gofeed.NewParser()
	out := make(map[string][]model.FeedItem, len(feeds))
	for _, name := range names {
		f := feeds[name]
		if f.Limit == 0 {
			continue
		}
		items, err := fetchFeed(ctx, parser, f)
		if err != nil {
			return nil, fmt.Errorf("feed %q: %w", name, err)
		}
		out[name] = items
	}
	return out, nil
}

// fetchFeed parses one feed, drops items that carry no usable date, sorts the
// rest most-recent-first, and only then truncates to Limit: sorting before
// truncating is load-bearing, since a feed's native item order is not
// reliable.
func fetchFeed(ctx context.Context, parser *gofeed.Parser, f config.Feed) ([]model.FeedItem, error) {
	feed, err := parser.ParseURLWithContext(f.URL, ctx)
	if err != nil {
		return nil, err
	}

	items := make([]model.FeedItem, 0, len(feed.Items))
	for _, item := range feed.Items {
		published, ok := resolvedDate(item)
		if !ok {
			// Dropped, not kept at the end: a dateless item can't be placed
			// in a most-recent-first ordering.
			continue
		}
		items = append(items, model.FeedItem{
			Title:       item.Title,
			URL:         item.Link,
			Published:   published,
			Description: item.Description,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Published.After(items[j].Published)
	})
	if len(items) > f.Limit {
		items = items[:f.Limit]
	}
	return items, nil
}

// resolvedDate prefers PublishedParsed, falling back to UpdatedParsed. An
// item with neither is undatable and reports ok == false.
func resolvedDate(item *gofeed.Item) (time.Time, bool) {
	if item.PublishedParsed != nil {
		return *item.PublishedParsed, true
	}
	if item.UpdatedParsed != nil {
		return *item.UpdatedParsed, true
	}
	return time.Time{}, false
}
