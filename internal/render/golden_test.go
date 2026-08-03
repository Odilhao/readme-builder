package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/model"
)

// realTemplatePath points at the real, shipped template - not a hand-written
// stand-in - so this file's tests exercise what actually renders, not a
// re-implementation of it.
func realTemplatePath(t *testing.T) string {
	t.Helper()
	path := filepath.Join("..", "..", "templates", "README.md.tmpl")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("real template not found at %s: %v", path, err)
	}
	return path
}

// TestRenderOmitsSeparatorForEmptyDescription proves the em-dash separator
// between a repo link and its description disappears entirely when
// Description is empty, rather than collapsing to a dangling "- ". It
// exercises the real template through Render, so a fix that only
// special-cases one fixture's literal text cannot pass this.
func TestRenderOmitsSeparatorForEmptyDescription(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "README.md")

	data := &model.Data{
		GitHub: model.GitHub{
			Repos: []model.Repo{
				{
					Name:     "no-description",
					URL:      "https://github.com/octocat/no-description",
					PushedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
		Now: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := Render(data, realTemplatePath(t), outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	if strings.Contains(string(out), "—") {
		t.Errorf("output carries a dangling em-dash separator for an empty description:\n%s", out)
	}
}

// TestRenderKeepsSeparatorForNonEmptyDescription is the inverse of
// TestRenderOmitsSeparatorForEmptyDescription: the guard must not swallow the
// separator when there is a description to show.
func TestRenderKeepsSeparatorForNonEmptyDescription(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "README.md")

	data := &model.Data{
		GitHub: model.GitHub{
			Repos: []model.Repo{
				{
					Name:        "has-description",
					Description: "an example repository",
					URL:         "https://github.com/octocat/has-description",
					PushedAt:    time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				},
			},
		},
		Now: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
	}

	if err := Render(data, realTemplatePath(t), outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	if !strings.Contains(string(out), "— an example repository") {
		t.Errorf("output missing the em-dash separator before a non-empty description:\n%s", out)
	}
}

// TestRenderProducesGoldenOutputForRealTemplate renders representative
// model.Data through the real template via Render and compares the result
// byte-for-byte to a golden file - proving the full pipeline (not a
// re-implementation of the template) end to end, including the
// self-repo-free, mixed-description data a real run would produce.
func TestRenderProducesGoldenOutputForRealTemplate(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "README.md")

	data := &model.Data{
		User: model.User{
			Login: "octocat",
			Name:  "The Octocat",
			Bio:   "A test account for the API and Octocat lovers everywhere.",
		},
		GitHub: model.GitHub{
			Contributions: []model.Contribution{
				{
					Repo:        "example-org/example-repo",
					Events:      5,
					CommitsURL:  "https://github.com/example-org/example-repo/commits?author=octocat",
					ActivityURL: "https://github.com/example-org/example-repo/issues?q=updated:30d+author:octocat",
				},
			},
			Repos: []model.Repo{
				{
					Name:        "Hello-World",
					FullName:    "octocat/Hello-World",
					Description: "My first repository on GitHub.",
					URL:         "https://github.com/octocat/Hello-World",
					PushedAt:    time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
				},
				{
					Name:     "Spoon-Knife",
					FullName: "octocat/Spoon-Knife",
					URL:      "https://github.com/octocat/Spoon-Knife",
					PushedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
				},
			},
			PullRequests: []model.PullRequest{
				{
					Repo:  "example-org/example-repo",
					Title: "Fix a bug",
					URL:   "https://github.com/example-org/example-repo/pull/42",
					State: "open",
				},
			},
		},
		Feeds: map[string][]model.FeedItem{
			"Blog": {
				{Title: "Example Post", URL: "https://example.com/posts/1"},
			},
		},
		Now: time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC),
	}

	if err := Render(data, realTemplatePath(t), outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	got, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read rendered output: %v", err)
	}

	goldenPath := filepath.Join("testdata", "readme.golden")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden file %s: %v", goldenPath, err)
	}

	if !bytes.Equal(got, want) {
		t.Errorf("rendered output does not match %s\n--- got ---\n%s\n--- want ---\n%s", goldenPath, got, want)
	}
}
