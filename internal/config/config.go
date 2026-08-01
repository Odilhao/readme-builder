package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Item counts used when a present section or feed omits limit.
const (
	DefaultSectionLimit = 10
	DefaultFeedLimit    = 5
)

// Config is a validated configuration with every path already resolved.
type Config struct {
	Username string
	GitHub   GitHub
	Feeds    map[string]Feed
	Render   []Render
}

// GitHub carries the repository filters and one field per fetchable section.
// A nil section was absent from the config and must not be fetched at all.
type GitHub struct {
	ExcludeForks bool
	ExcludeRepos []string
	ExcludeOrgs  []string

	Contributions *Section
	Repos         *Section
	Forks         *Section
	PullRequests  *Section
	Stars         *Section
	Releases      *Section
	Followers     *Section
	Gists         *Section
}

// Section is a requested GitHub section. Limit 0 means it yields no items;
// an omitted limit becomes DefaultSectionLimit, never 0.
type Section struct {
	Limit int
}

type Feed struct {
	URL   string
	Limit int
}

type Render struct {
	Template string
	Output   string
}

// The decode types are separate from the resolved ones above so that absent and
// explicitly zero limits stay distinguishable, and so nothing exported here can
// be decoded into by accident.
type rawConfig struct {
	Username string             `toml:"username"`
	GitHub   rawGitHub          `toml:"github"`
	Feeds    map[string]rawFeed `toml:"feeds"`
	Render   []rawRender        `toml:"render"`
}

type rawGitHub struct {
	ExcludeForks bool     `toml:"exclude_forks"`
	ExcludeRepos []string `toml:"exclude_repos"`
	ExcludeOrgs  []string `toml:"exclude_orgs"`

	Contributions *rawSection `toml:"contributions"`
	Repos         *rawSection `toml:"repos"`
	Forks         *rawSection `toml:"forks"`
	PullRequests  *rawSection `toml:"pull_requests"`
	Stars         *rawSection `toml:"stars"`
	Releases      *rawSection `toml:"releases"`
	Followers     *rawSection `toml:"followers"`
	Gists         *rawSection `toml:"gists"`
}

type rawSection struct {
	Limit *int `toml:"limit"`
}

type rawFeed struct {
	URL   string `toml:"url"`
	Limit *int   `toml:"limit"`
}

type rawRender struct {
	Template string `toml:"template"`
	Output   string `toml:"output"`
}

// feedName must be reachable as .Feeds.<name> in a template, where a hyphen is
// a parse error. ASCII only, to keep confusables out of a JSON object key.
var feedName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Load reads and validates the config at path. It needs no credential and
// performs no network access.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var raw rawConfig
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return nil, decodeError(path, err)
	}

	dir, err := configDir(path)
	if err != nil {
		return nil, err
	}
	return build(path, dir, &raw)
}

// decodeError names the offending key: the bare strict-mode error does not,
// and an unnamed typo is the failure this package exists to prevent.
func decodeError(path string, err error) error {
	var strict *toml.StrictMissingError
	if errors.As(err, &strict) {
		keys := make([]string, 0, len(strict.Errors))
		for _, e := range strict.Errors {
			row, _ := e.Position()
			keys = append(keys, fmt.Sprintf("%q (line %d)", strings.Join(e.Key(), "."), row))
		}
		return fmt.Errorf("%s: unknown key %s", path, strings.Join(keys, ", "))
	}
	var decode *toml.DecodeError
	if errors.As(err, &decode) && len(decode.Key()) > 0 {
		return fmt.Errorf("%s: %s: %w", path, strings.Join(decode.Key(), "."), err)
	}
	return fmt.Errorf("%s: %w", path, err)
}

func configDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return filepath.Dir(abs), nil
}

func build(path, dir string, raw *rawConfig) (*Config, error) {
	// Never derived from a token or the environment: GET /user would require a
	// user token, which is exactly what this tool refuses to need.
	if strings.TrimSpace(raw.Username) == "" {
		return nil, fmt.Errorf("%s: username is required", path)
	}

	cfg := &Config{Username: raw.Username}

	var err error
	if cfg.GitHub, err = buildGitHub(path, &raw.GitHub); err != nil {
		return nil, err
	}
	if cfg.Feeds, err = buildFeeds(path, raw.Feeds); err != nil {
		return nil, err
	}
	if cfg.Render, err = buildRender(path, dir, raw.Render); err != nil {
		return nil, err
	}
	return cfg, nil
}

func buildGitHub(path string, raw *rawGitHub) (GitHub, error) {
	gh := GitHub{
		ExcludeForks: raw.ExcludeForks,
		ExcludeRepos: raw.ExcludeRepos,
		ExcludeOrgs:  raw.ExcludeOrgs,
	}

	for i, r := range raw.ExcludeRepos {
		owner, name, ok := strings.Cut(r, "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return gh, fmt.Errorf("%s: github.exclude_repos[%d]: %q must be owner/name", path, i, r)
		}
	}
	for i, o := range raw.ExcludeOrgs {
		if o == "" || strings.Contains(o, "/") {
			return gh, fmt.Errorf("%s: github.exclude_orgs[%d]: %q must be a bare organisation login, not owner/name", path, i, o)
		}
	}

	sections := []struct {
		name string
		raw  *rawSection
		dst  **Section
	}{
		{"contributions", raw.Contributions, &gh.Contributions},
		{"repos", raw.Repos, &gh.Repos},
		{"forks", raw.Forks, &gh.Forks},
		{"pull_requests", raw.PullRequests, &gh.PullRequests},
		{"stars", raw.Stars, &gh.Stars},
		{"releases", raw.Releases, &gh.Releases},
		{"followers", raw.Followers, &gh.Followers},
		{"gists", raw.Gists, &gh.Gists},
	}
	for _, s := range sections {
		if s.raw == nil {
			continue
		}
		limit, err := limitOf(path, "github."+s.name, s.raw.Limit, DefaultSectionLimit)
		if err != nil {
			return gh, err
		}
		*s.dst = &Section{Limit: limit}
	}
	return gh, nil
}

func buildFeeds(path string, raw map[string]rawFeed) (map[string]Feed, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	feeds := make(map[string]Feed, len(raw))
	for _, name := range names {
		if !feedName.MatchString(name) {
			return nil, fmt.Errorf("%s: feeds.%q: invalid feed name; must match %s so that .Feeds.%s parses in a template", path, name, feedName, name)
		}
		f := raw[name]
		if f.URL == "" {
			return nil, fmt.Errorf("%s: feeds.%s: url is required", path, name)
		}
		limit, err := limitOf(path, "feeds."+name, f.Limit, DefaultFeedLimit)
		if err != nil {
			return nil, err
		}
		feeds[name] = Feed{URL: f.URL, Limit: limit}
	}
	return feeds, nil
}

func buildRender(path, dir string, raw []rawRender) ([]Render, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("%s: at least one [[render]] section is required", path)
	}

	out := make([]Render, 0, len(raw))
	for i, r := range raw {
		if r.Template == "" {
			return nil, fmt.Errorf("%s: render[%d]: template is required", path, i)
		}
		if r.Output == "" {
			return nil, fmt.Errorf("%s: render[%d]: output is required", path, i)
		}
		out = append(out, Render{
			Template: resolvePath(dir, r.Template),
			Output:   resolvePath(dir, r.Output),
		})
	}
	return out, nil
}

// An explicit 0 is honoured rather than defaulted: it means the config declined
// to spend the request budget here, and silently overriding that would waste it.
func limitOf(path, key string, limit *int, def int) (int, error) {
	if limit == nil {
		return def, nil
	}
	if *limit < 0 {
		return 0, fmt.Errorf("%s: %s.limit: %d must not be negative", path, key, *limit)
	}
	return *limit, nil
}

// Relative paths anchor to the config file's directory so -config /elsewhere/x.toml
// works from any cwd; absolute paths are left exactly as written.
func resolvePath(dir, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(dir, p)
}
