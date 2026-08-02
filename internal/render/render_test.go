package render

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/model"
)

func TestRenderWritesToFile(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	if err := Render(data, tmplPath, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	want := "Hello octocat"
	if got := string(out); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRenderStreamsToStdout(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	// Capture stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStdout := os.Stdout
	os.Stdout = w

	defer func() {
		os.Stdout = oldStdout
	}()

	if err := Render(data, tmplPath, "-"); err != nil {
		t.Fatalf("Render: %v", err)
	}

	w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r); err != nil {
		t.Fatalf("read stdout: %v", err)
	}

	want := "Hello octocat"
	if got := buf.String(); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestCheckDetectsDrift(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Write existing output
	if err := os.WriteFile(outPath, []byte("Hello stranger"), 0o644); err != nil {
		t.Fatalf("create existing output: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	drift, err := Check(data, tmplPath, outPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if !drift {
		t.Error("Check: expected drift but got none")
	}
}

func TestCheckDetectsNoDrift(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Write matching output
	if err := os.WriteFile(outPath, []byte("Hello octocat"), 0o644); err != nil {
		t.Fatalf("create existing output: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	drift, err := Check(data, tmplPath, outPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	if drift {
		t.Error("Check: expected no drift but got drift")
	}
}

func TestCheckReturnsFalseWhenOutputMissing(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	// Don't create output file - it's missing

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	drift, err := Check(data, tmplPath, outPath)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	// Missing output is considered drift
	if !drift {
		t.Error("Check: expected drift when output is missing")
	}
}

func TestDumpJSONEmitsData(t *testing.T) {
	data := &model.Data{
		User: model.User{
			Login:     "octocat",
			Name:      "The Octocat",
			Bio:       "GitHub's mascot",
			AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4",
		},
		GitHub: model.GitHub{
			Contributions: []model.Contribution{
				{Repo: "owner/repo", Events: 10},
			},
		},
		Feeds: map[string][]model.FeedItem{
			"blog": {
				{
					Title:       "Test Post",
					URL:         "https://example.com/post",
					Published:   time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
					Description: "A test post",
				},
			},
		},
		Now: time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	output, err := DumpJSON(data)
	if err != nil {
		t.Fatalf("DumpJSON: %v", err)
	}

	// Verify it's valid JSON
	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}

	// Check that the user was marshalled
	user, ok := result["user"].(map[string]interface{})
	if !ok {
		t.Fatal("user not found in JSON")
	}

	if got := user["login"]; got != "octocat" {
		t.Errorf("user.login = %v, want octocat", got)
	}

	if got := user["name"]; got != "The Octocat" {
		t.Errorf("user.name = %v, want The Octocat", got)
	}
}

func TestRenderUsesTextTemplate(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Use template that would be corrupted by HTML escaping
	// In text/template, ' remains '
	// In html/template, ' becomes &#39;
	tmplContent := `It's a {{.User.Login}} test`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	if err := Render(data, tmplPath, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	want := "It's a octocat test"
	if got := string(out); got != want {
		t.Errorf("output = %q, want %q (should not be HTML-escaped)", got, want)
	}

	// Verify it doesn't contain HTML entity
	if strings.Contains(string(out), "&#39;") {
		t.Error("output contains HTML entity &#39; (was html/template used?)")
	}
}

func TestRenderWithHumanize(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Use the humanize function directly from FuncMap
	tmplContent := `Last pushed: {{humanize (index .GitHub.Repos 0).PushedAt}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		GitHub: model.GitHub{
			Repos: []model.Repo{
				{
					Name:     "repo1",
					PushedAt: time.Date(2026, 7, 31, 12, 30, 0, 0, time.UTC), // 1 day ago from `now`
				},
			},
		},
		Now: time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	if err := Render(data, tmplPath, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	got := string(out)
	want := "Last pushed: 1d ago"
	if got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}

func TestRenderAtomicWrite(t *testing.T) {
	tmpdir := t.TempDir()
	tmplPath := filepath.Join(tmpdir, "test.md.tmpl")
	outPath := filepath.Join(tmpdir, "output.md")

	// Create a simple template
	tmplContent := `Hello {{.User.Login}}`
	if err := os.WriteFile(tmplPath, []byte(tmplContent), 0o644); err != nil {
		t.Fatalf("create template: %v", err)
	}

	data := &model.Data{
		User: model.User{Login: "octocat"},
		Now:  time.Date(2026, 8, 1, 12, 30, 45, 0, time.UTC),
	}

	if err := Render(data, tmplPath, outPath); err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Check that temp files matching the pattern are cleaned up
	// (The template file and output file are expected)
	entries, err := os.ReadDir(tmpdir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}

	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") && strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	want := "Hello octocat"
	if got := string(out); got != want {
		t.Errorf("output = %q, want %q", got, want)
	}
}
