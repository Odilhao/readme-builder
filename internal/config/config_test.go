package config

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testdataDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatalf("abs testdata: %v", err)
	}
	return dir
}

func load(t *testing.T, name string) *Config {
	t.Helper()
	cfg, err := Load(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("Load(%s): unexpected error: %v", name, err)
	}
	return cfg
}

func loadErr(t *testing.T, name string) string {
	t.Helper()
	cfg, err := Load(filepath.Join("testdata", name))
	if err == nil {
		t.Fatalf("Load(%s): got %+v, want error", name, cfg)
	}
	return err.Error()
}

func wantContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("error %q does not mention %q", got, w)
		}
	}
}

func TestLoadValid(t *testing.T) {
	cfg := load(t, "valid.toml")
	dir := testdataDir(t)

	if cfg.Username != "octocat" {
		t.Errorf("Username: got %q, want %q", cfg.Username, "octocat")
	}

	wantGitHub := GitHub{
		ExcludeForks:  true,
		ExcludeRepos:  []string{"example-org/example-repo", "octocat/another-example"},
		ExcludeOrgs:   []string{"example-org"},
		Contributions: &Section{Limit: 0, TimeWindow: "30d"},
		Repos:         &Section{Limit: 3, TimeWindow: ""},
		Stars:         &Section{Limit: DefaultSectionLimit, TimeWindow: ""},
	}
	if !reflect.DeepEqual(cfg.GitHub, wantGitHub) {
		t.Errorf("GitHub:\n got %+v\nwant %+v", cfg.GitHub, wantGitHub)
	}

	wantFeeds := map[string]Feed{
		"blog":  {URL: "https://example.com/feed.xml", Limit: 2},
		"notes": {URL: "https://example.com/notes.xml", Limit: DefaultFeedLimit},
	}
	if !reflect.DeepEqual(cfg.Feeds, wantFeeds) {
		t.Errorf("Feeds:\n got %+v\nwant %+v", cfg.Feeds, wantFeeds)
	}

	wantRender := []Render{
		{
			Template: filepath.Join(dir, "templates", "README.md.tmpl"),
			Output:   filepath.Join(dir, "README.md"),
		},
		{
			Template: "/opt/example/absolute.tmpl",
			Output:   filepath.Join(dir, "..", "out", "index.html"),
		},
	}
	if !reflect.DeepEqual(cfg.Render, wantRender) {
		t.Errorf("Render:\n got %+v\nwant %+v", cfg.Render, wantRender)
	}
}

// The whole GitHub struct is compared, not one field: a section that starts
// defaulting to non-nil is exactly the invariant-9 break this guards.
func TestSectionPresence(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    GitHub
	}{
		{"absent stays nil", "section_absent.toml", GitHub{}},
		{"present but empty takes the default", "section_empty.toml", GitHub{Repos: &Section{Limit: DefaultSectionLimit}}},
		{"explicit limit is used", "section_limit.toml", GitHub{Repos: &Section{Limit: 3}}},
		{"explicit zero is honoured, not defaulted", "section_limit_zero.toml", GitHub{Repos: &Section{Limit: 0}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := load(t, tt.fixture).GitHub
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GitHub:\n got %+v\nwant %+v", got, tt.want)
			}
			if tt.want.Repos != nil && got.Repos != nil && *got.Repos != *tt.want.Repos {
				t.Errorf("Repos: got %+v, want %+v", *got.Repos, *tt.want.Repos)
			}
		})
	}
}

// Every section is named once here, with a limit distinct from every other and
// from the default, so that a section wired to the wrong field cannot pass: the
// misdirected limit lands where another section's belongs.
func TestEverySectionIsWired(t *testing.T) {
	gh := load(t, "all_sections.toml").GitHub

	want := GitHub{
		Contributions: &Section{Limit: 1, TimeWindow: "30d"},
		Repos:         &Section{Limit: 2, TimeWindow: ""},
		Forks:         &Section{Limit: 3, TimeWindow: ""},
		PullRequests:  &Section{Limit: 4, TimeWindow: ""},
		Stars:         &Section{Limit: 5, TimeWindow: ""},
		Releases:      &Section{Limit: 6, TimeWindow: ""},
		Followers:     &Section{Limit: 7, TimeWindow: ""},
		Gists:         &Section{Limit: 8, TimeWindow: ""},
		TopProjects:   &Section{Limit: 9, TimeWindow: "30d"},
	}
	if !reflect.DeepEqual(gh, want) {
		t.Errorf("GitHub:\n got %s\nwant %s", formatSections(gh), formatSections(want))
	}

	for _, s := range []struct {
		name string
		got  *Section
		want int
	}{
		{"contributions", gh.Contributions, 1},
		{"repos", gh.Repos, 2},
		{"forks", gh.Forks, 3},
		{"pull_requests", gh.PullRequests, 4},
		{"stars", gh.Stars, 5},
		{"releases", gh.Releases, 6},
		{"followers", gh.Followers, 7},
		{"gists", gh.Gists, 8},
		{"topprojects", gh.TopProjects, 9},
	} {
		if s.got == nil {
			t.Errorf("github.%s: got nil, want a section with limit %d", s.name, s.want)
			continue
		}
		if s.got.Limit != s.want {
			t.Errorf("github.%s.limit: got %d, want %d", s.name, s.got.Limit, s.want)
		}
	}
}

// %+v on GitHub prints section pointers as addresses, which says nothing about
// which section went where.
func formatSections(gh GitHub) string {
	limit := func(s *Section) string {
		if s == nil {
			return "nil"
		}
		return fmt.Sprint(s.Limit)
	}
	return fmt.Sprintf("{contributions:%s repos:%s forks:%s pull_requests:%s stars:%s releases:%s followers:%s gists:%s topprojects:%s}",
		limit(gh.Contributions), limit(gh.Repos), limit(gh.Forks), limit(gh.PullRequests),
		limit(gh.Stars), limit(gh.Releases), limit(gh.Followers), limit(gh.Gists), limit(gh.TopProjects))
}

func TestDefaultLimits(t *testing.T) {
	if DefaultSectionLimit != 10 {
		t.Errorf("DefaultSectionLimit: got %d, want 10", DefaultSectionLimit)
	}
	if DefaultFeedLimit != 5 {
		t.Errorf("DefaultFeedLimit: got %d, want 5", DefaultFeedLimit)
	}
	cfg := load(t, "section_empty.toml")
	if cfg.GitHub.Repos == nil {
		t.Fatal("github.repos: got nil, want a section")
	}
	if cfg.GitHub.Repos.Limit != 10 {
		t.Errorf("github.repos.limit: got %d, want 10", cfg.GitHub.Repos.Limit)
	}
	if got := load(t, "valid.toml").Feeds["notes"].Limit; got != 5 {
		t.Errorf("feeds.notes.limit: got %d, want 5", got)
	}
}

func TestFilters(t *testing.T) {
	gh := load(t, "valid.toml").GitHub
	if !gh.ExcludeForks {
		t.Error("exclude_forks: got false, want true")
	}
	wantRepos := []string{"example-org/example-repo", "octocat/another-example"}
	if !reflect.DeepEqual(gh.ExcludeRepos, wantRepos) {
		t.Errorf("exclude_repos: got %v, want %v", gh.ExcludeRepos, wantRepos)
	}
	wantOrgs := []string{"example-org"}
	if !reflect.DeepEqual(gh.ExcludeOrgs, wantOrgs) {
		t.Errorf("exclude_orgs: got %v, want %v", gh.ExcludeOrgs, wantOrgs)
	}
}

// A typo must be named, at every depth, including inside the dynamically keyed
// feeds map, where struct-level strictness is not obviously in force.
func TestUnknownKeyIsRejectedAtEveryDepth(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    string
	}{
		{"top level", "unknown_top.toml", "usernme"},
		{"inside [github]", "unknown_github.toml", "github.exclude_fork"},
		{"inside [github.repos]", "unknown_github_section.toml", "github.repos.limt"},
		{"unknown [github.<section>] table", "unknown_github_table.toml", "github.repose"},
		{"inside [feeds.<name>]", "unknown_feed.toml", "feeds.blog.lmit"},
		{"inside [[render]]", "unknown_render.toml", "render.ouput"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := loadErr(t, tt.fixture)
			wantContains(t, got, "unknown key", tt.want, tt.fixture)
		})
	}
}

// The tool must never need a credential, so a token in the environment may not
// stand in for a missing username.
func TestUsernameRequired(t *testing.T) {
	for _, fixture := range []string{"no_username.toml", "blank_username.toml"} {
		t.Run(fixture, func(t *testing.T) {
			t.Setenv("GITHUB_TOKEN", "not-a-token")
			t.Setenv("GH_TOKEN", "not-a-token")
			t.Setenv("GITHUB_ACTOR", "octocat")
			wantContains(t, loadErr(t, fixture), "username is required", fixture)
		})
	}
}

func TestValidationErrors(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    []string
	}{
		{"negative section limit", "section_limit_negative.toml", []string{"github.repos", "negative"}},
		{"negative feed limit", "feed_limit_negative.toml", []string{"feeds.blog", "negative"}},
		{"feed name is not a template identifier", "feed_bad_name.toml", []string{"my-feed", "invalid feed name"}},
		{"feed without url", "feed_no_url.toml", []string{"feeds.blog", "url is required"}},
		{"bare exclude_repos entry", "exclude_repo_bare.toml", []string{"exclude_repos", "example-repo", "owner/name"}},
		{"qualified exclude_orgs entry", "exclude_org_qualified.toml", []string{"exclude_orgs", "example-org/example-repo"}},
		{"no render target", "no_render.toml", []string{"at least one", "[[render]]"}},
		{"render without template", "render_no_template.toml", []string{"render[0]", "template is required"}},
		{"render without output", "render_no_output.toml", []string{"render[0]", "output is required"}},
		{"wrong scalar type", "bad_type.toml", []string{"username"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantContains(t, loadErr(t, tt.fixture), append(tt.want, tt.fixture)...)
		})
	}
}

func TestFeedNamesAccepted(t *testing.T) {
	feeds := load(t, "valid.toml").Feeds
	for _, name := range []string{"blog", "notes"} {
		if _, ok := feeds[name]; !ok {
			t.Errorf("feeds: %q missing from %v", name, feeds)
		}
	}
}

func TestMissingFile(t *testing.T) {
	if _, err := Load(filepath.Join("testdata", "does-not-exist.toml")); err == nil {
		t.Fatal("Load: got nil error for a missing file")
	}
}

// Contributions time_window parsing: valid values (3d, 2w, 1y), invalid suffixes, and absence defaults.
func TestContributionsTimeWindowParsing(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    string
		wantErr bool
	}{
		{"valid 7d", "contributions_time_window_valid.toml", "7d", false},
		{"valid 2w", "contributions_time_window_valid_weeks.toml", "2w", false},
		{"valid 1y", "contributions_time_window_valid_years.toml", "1y", false},
		{"invalid suffix", "contributions_time_window_invalid.toml", "", true},
		{"absent uses default", "contributions_time_window_absent.toml", "30d", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr {
				got := loadErr(t, tt.fixture)
				wantContains(t, got, "time_window", "invalid")
			} else {
				cfg := load(t, tt.fixture)
				if cfg.GitHub.Contributions == nil {
					t.Fatal("github.contributions: got nil, want a section")
				}
				if cfg.GitHub.Contributions.TimeWindow != tt.want {
					t.Errorf("TimeWindow: got %q, want %q", cfg.GitHub.Contributions.TimeWindow, tt.want)
				}
			}
		})
	}
}

// TopProjects reuses the same time_window parsing and validation as
// Contributions (#72): valid format, an invalid suffix, and the same 30d
// default when the section is present but time_window is omitted.
func TestTopProjectsTimeWindowParsing(t *testing.T) {
	tests := []struct {
		name    string
		fixture string
		want    string
		wantErr bool
	}{
		{"valid 7d", "topprojects_time_window_valid.toml", "7d", false},
		{"invalid suffix", "topprojects_time_window_invalid.toml", "", true},
		{"absent uses default", "topprojects_time_window_absent.toml", "30d", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantErr {
				got := loadErr(t, tt.fixture)
				wantContains(t, got, "time_window", "invalid")
			} else {
				cfg := load(t, tt.fixture)
				if cfg.GitHub.TopProjects == nil {
					t.Fatal("github.topprojects: got nil, want a section")
				}
				if cfg.GitHub.TopProjects.TimeWindow != tt.want {
					t.Errorf("TimeWindow: got %q, want %q", cfg.GitHub.TopProjects.TimeWindow, tt.want)
				}
			}
		})
	}
}

// Paths resolve against the config file's directory, so -config /elsewhere/x.toml
// works from any cwd. Run from an unrelated directory: a test run from the
// package directory passes even when resolution is against cwd.
func TestPathsResolveAgainstConfigDir(t *testing.T) {
	dir := testdataDir(t)
	cfgPath := filepath.Join(dir, "paths.toml")

	want := []Render{
		{
			Template: filepath.Join(dir, "templates", "README.md.tmpl"),
			Output:   filepath.Join(dir, "README.md"),
		},
		{
			Template: "/opt/example/absolute.tmpl",
			Output:   "/opt/example/out/README.md",
		},
		{
			Template: filepath.Join(dir, "..", "shared", "page.html.tmpl"),
			Output:   filepath.Join(dir, "..", "out", "index.html"),
		},
	}

	t.Run("absolute config path from an unrelated cwd", func(t *testing.T) {
		t.Chdir(t.TempDir())
		cfg, err := Load(cfgPath)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !reflect.DeepEqual(cfg.Render, want) {
			t.Errorf("Render:\n got %+v\nwant %+v", cfg.Render, want)
		}
	})

	t.Run("relative config path from an unrelated cwd", func(t *testing.T) {
		tmp := t.TempDir()
		rel, err := filepath.Rel(tmp, cfgPath)
		if err != nil {
			t.Fatalf("rel: %v", err)
		}
		t.Chdir(tmp)
		cfg, err := Load(rel)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !reflect.DeepEqual(cfg.Render, want) {
			t.Errorf("Render:\n got %+v\nwant %+v", cfg.Render, want)
		}
	})
}
