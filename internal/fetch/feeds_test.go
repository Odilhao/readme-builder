package fetch

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

// rssItem is the minimal set of fields a fixture needs to control.
type rssItem struct {
	title       string
	link        string
	description string
	pubDate     string // RFC 1123Z, empty to omit <pubDate>
}

// newFeedServer serves a single RSS 2.0 feed built from items.
func newFeedServer(t *testing.T, items []rssItem) *httptest.Server {
	t.Helper()

	var body string
	body += `<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel><title>Test Feed</title>`
	for _, it := range items {
		body += "<item>"
		body += fmt.Sprintf("<title>%s</title>", it.title)
		body += fmt.Sprintf("<link>%s</link>", it.link)
		body += fmt.Sprintf("<description>%s</description>", it.description)
		if it.pubDate != "" {
			body += fmt.Sprintf("<pubDate>%s</pubDate>", it.pubDate)
		}
		body += "</item>"
	}
	body += `</channel></rss>`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func rfc1123z(tm time.Time) string {
	return tm.Format(time.RFC1123Z)
}

func TestFeedsEmptyMapReturnsNilWithoutRequest(t *testing.T) {
	got, err := Feeds(context.Background(), nil)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestFeedsSkipsZeroLimitFeedEntirely(t *testing.T) {
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 0}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if len(got["blog"]) != 0 {
		t.Errorf("got %v, want no items", got["blog"])
	}
	if requested {
		t.Error("feed with Limit == 0 must not be fetched at all")
	}
}

func TestFeedsPopulatesEveryModelField(t *testing.T) {
	base := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	srv := newFeedServer(t, []rssItem{{
		title:       "Hello",
		link:        "https://example.com/hello",
		description: "a post",
		pubDate:     rfc1123z(base),
	}})

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 5}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}

	want := model.FeedItem{
		Title:       "Hello",
		URL:         "https://example.com/hello",
		Published:   base,
		Description: "a post",
	}
	items := got["blog"]
	if len(items) != 1 || !items[0].Published.Equal(want.Published) || items[0].Title != want.Title || items[0].URL != want.URL || items[0].Description != want.Description {
		t.Errorf("got %+v, want %+v", items, want)
	}
}

// TestFeedsFallsBackToUpdatedWhenPublishedIsAbsent proves the fallback order.
// gofeed's own Atom translator already backfills PublishedParsed from
// <updated> when <published> is absent, so an Atom fixture never reaches
// fetchFeed's fallback code at all - confirmed empirically against
// github.com/mmcdole/gofeed v1.4.0 (see translator.go's translateItemEntry,
// which sets item.PublishedParsed = entry.UpdatedParsed itself). An RSS 2.0
// item with a bare <atom:updated> extension and no <pubDate> or <dc:date>
// does leave PublishedParsed nil while UpdatedParsed is set - that's the only
// shape that actually exercises resolvedDate's fallback branch.
func TestFeedsFallsBackToUpdatedWhenPublishedIsAbsent(t *testing.T) {
	base := time.Date(2026, 7, 2, 8, 0, 0, 0, time.UTC)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom">
<channel><title>Test Feed</title>
<item>
<title>Updated Only</title>
<link>https://example.com/updated-only</link>
<description>an entry with no published date</description>
<atom:updated>%s</atom:updated>
</item>
</channel>
</rss>`, base.Format(time.RFC3339))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 5}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}

	items := got["blog"]
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if !items[0].Published.Equal(base) {
		t.Errorf("Published = %v, want %v (fallback to <updated>)", items[0].Published, base)
	}
}

// TestFeedsPrefersPublishedOverUpdatedWhenBothPresent proves the fallback
// order itself, not just that a fallback exists: an entry carrying both
// <published> and <updated> at different times must be dated by <published>.
func TestFeedsPrefersPublishedOverUpdatedWhenBothPresent(t *testing.T) {
	published := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC)
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
<title>Test Feed</title>
<entry>
<title>Both Dates</title>
<link href="https://example.com/both-dates"/>
<summary>an entry with both dates set</summary>
<published>%s</published>
<updated>%s</updated>
<id>tag:example.com,2026:both-dates</id>
</entry>
</feed>`, published.Format(time.RFC3339), updated.Format(time.RFC3339))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 5}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}

	items := got["blog"]
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if !items[0].Published.Equal(published) {
		t.Errorf("Published = %v, want %v (must prefer <published> over <updated>)", items[0].Published, published)
	}
}

// TestFeedsDropsItemsWithNoDate proves a dateless item is dropped rather than
// kept at the end: only the dated item must survive.
func TestFeedsDropsItemsWithNoDate(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newFeedServer(t, []rssItem{
		{title: "Dateless", link: "https://example.com/dateless", description: "no date"},
		{title: "Dated", link: "https://example.com/dated", description: "has a date", pubDate: rfc1123z(base)},
	})

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 5}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}

	items := got["blog"]
	if len(items) != 1 || items[0].Title != "Dated" {
		t.Errorf("got %+v, want only the dated item", items)
	}
}

// TestFeedsSortsByDateDescendingBeforeTruncating proves ordering-before-cut
// is load-bearing: the feed is served oldest-first (its native, unreliable
// order), and Limit=1 must keep the *newest* item, which only happens if
// sorting runs before truncation.
func TestFeedsSortsByDateDescendingBeforeTruncating(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srv := newFeedServer(t, []rssItem{
		{title: "Older", link: "https://example.com/older", description: "older", pubDate: rfc1123z(base)},
		{title: "Newer", link: "https://example.com/newer", description: "newer", pubDate: rfc1123z(base.Add(time.Hour))},
	})

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 1}}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}

	items := got["blog"]
	if len(items) != 1 || items[0].Title != "Newer" {
		t.Errorf("got %+v, want only the newer item (sort must run before truncation)", items)
	}
}

// TestFeedsAbortsWholeCallOnOneFeedError matches Contributions' fail-fast
// precedent: one feed's fetch/parse error aborts the entire call rather than
// silently degrading to fewer feeds.
func TestFeedsAbortsWholeCallOnOneFeedError(t *testing.T) {
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badSrv.Close)
	goodSrv := newFeedServer(t, []rssItem{{title: "ok", link: "https://example.com/ok", pubDate: rfc1123z(time.Now())}})

	feeds := map[string]config.Feed{
		"bad":  {URL: badSrv.URL, Limit: 5},
		"good": {URL: goodSrv.URL, Limit: 5},
	}
	got, err := Feeds(context.Background(), feeds)
	if err == nil {
		t.Fatal("Feeds: want error when one feed fails, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

// TestFeedsHonorsCanceledContext proves ctx actually reaches the parse call
// (ParseURLWithContext, not ParseURL): a context canceled before the call
// must abort the request rather than silently completing.
func TestFeedsHonorsCanceledContext(t *testing.T) {
	srv := newFeedServer(t, []rssItem{{title: "ok", link: "https://example.com/ok", pubDate: rfc1123z(time.Now())}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	feeds := map[string]config.Feed{"blog": {URL: srv.URL, Limit: 5}}
	got, err := Feeds(ctx, feeds)
	if err == nil {
		t.Fatal("Feeds: want error with a canceled context, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}

// TestFeedsErrorsOnAlphabeticallyFirstFailingFeedFirst proves the sorted
// iteration order feeds.go's own comment calls deliberate is actually
// observable: with two feeds that both fail, the error must name the
// alphabetically-first one, since fail-fast stops at the first name visited.
// Go randomizes map iteration order per range, so a single call would only
// catch a missing sort.Strings by chance (~50% of the time with two keys);
// the loop calls Feeds repeatedly so that removing the sort fails reliably
// rather than flakily, while sorted code passes deterministically every time.
func TestFeedsErrorsOnAlphabeticallyFirstFailingFeedFirst(t *testing.T) {
	badA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badA.Close)
	badB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(badB.Close)

	feeds := map[string]config.Feed{
		"zzz_feed": {URL: badB.URL, Limit: 5},
		"aaa_feed": {URL: badA.URL, Limit: 5},
	}
	for i := 0; i < 30; i++ {
		_, err := Feeds(context.Background(), feeds)
		if err == nil {
			t.Fatal("Feeds: want error when both feeds fail, got nil")
		}
		if !strings.Contains(err.Error(), `feed "aaa_feed"`) {
			t.Fatalf(`err = %q, want it to name "aaa_feed" (the alphabetically-first feed)`, err.Error())
		}
	}
}

func TestFeedsMultipleFeedsAreKeptSeparate(t *testing.T) {
	base := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	srvA := newFeedServer(t, []rssItem{{title: "A item", link: "https://example.com/a", pubDate: rfc1123z(base)}})
	srvB := newFeedServer(t, []rssItem{{title: "B item", link: "https://example.com/b", pubDate: rfc1123z(base)}})

	feeds := map[string]config.Feed{
		"blog_a": {URL: srvA.URL, Limit: 5},
		"blog_b": {URL: srvB.URL, Limit: 5},
	}
	got, err := Feeds(context.Background(), feeds)
	if err != nil {
		t.Fatalf("Feeds: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d feeds, want 2", len(got))
	}
	if len(got["blog_a"]) != 1 || got["blog_a"][0].Title != "A item" {
		t.Errorf("blog_a = %+v, want [A item]", got["blog_a"])
	}
	if len(got["blog_b"]) != 1 || got["blog_b"][0].Title != "B item" {
		t.Errorf("blog_b = %+v, want [B item]", got["blog_b"])
	}
}
