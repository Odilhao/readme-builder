package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

type starItem struct {
	StarredAt time.Time `json:"starred_at"`
	Repo      repoItem  `json:"repo"`
}

type starsServer struct {
	*httptest.Server
	queries []string
}

func newStarsServer(t *testing.T, pages [][]starItem, status int) *starsServer {
	t.Helper()
	return newStarsServerForUser(t, "octocat", pages, status)
}

func newStarsServerForUser(t *testing.T, user string, pages [][]starItem, status int) *starsServer {
	t.Helper()

	s := &starsServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+user+"/starred", func(w http.ResponseWriter, r *http.Request) {
		s.queries = append(s.queries, r.URL.RawQuery)
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		page := 1
		if p := r.URL.Query().Get("page"); p != "" {
			fmt.Sscanf(p, "%d", &page)
		}
		w.Header().Set("Content-Type", "application/json")
		if page < 1 || page > len(pages) {
			_ = json.NewEncoder(w).Encode([]starItem{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func starConfig(limit int) config.GitHub {
	return config.GitHub{Stars: &config.Section{Limit: limit}}
}

func TestStarsNilSectionMakesNoRequest(t *testing.T) {
	srv := newStarsServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Stars(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Stars: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestStarsZeroLimitMakesNoRequest(t *testing.T) {
	srv := newStarsServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Stars(context.Background(), c, "octocat", starConfig(0))
	if err != nil {
		t.Fatalf("Stars: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestStarsPopulatesEveryModelField(t *testing.T) {
	starredAt := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	pushed := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	pages := [][]starItem{{{
		StarredAt: starredAt,
		Repo: repoItem{
			Name:            "Hello-World",
			FullName:        "octocat/Hello-World",
			Description:     "my first repo",
			HTMLURL:         "https://github.com/octocat/Hello-World",
			Language:        "Go",
			StargazersCount: 42,
			PushedAt:        pushed,
			Fork:            false,
		},
	}}}
	srv := newStarsServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Stars(context.Background(), c, "octocat", starConfig(10))
	if err != nil {
		t.Fatalf("Stars: %v", err)
	}

	want := model.Star{
		Repo: model.Repo{
			Name:        "Hello-World",
			FullName:    "octocat/Hello-World",
			Description: "my first repo",
			URL:         "https://github.com/octocat/Hello-World",
			Language:    "Go",
			Stars:       42,
			PushedAt:    pushed,
		},
		StarredAt: starredAt,
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestStarsStopsAtLimit(t *testing.T) {
	starredAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pushed := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]starItem{
		{{
			StarredAt: starredAt,
			Repo: repoItem{
				Name: "r-1", FullName: "example-org/r-1", PushedAt: pushed,
			},
		}},
		{{
			StarredAt: starredAt,
			Repo: repoItem{
				Name: "r-extra", FullName: "example-org/r-extra", PushedAt: pushed,
			},
		}},
	}
	srv := newStarsServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Stars(context.Background(), c, "octocat", starConfig(1))
	if err != nil {
		t.Fatalf("Stars: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d stars, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (must stop once Limit is reached)", len(srv.queries))
	}
}

func TestStarsReturnsErrorFromList(t *testing.T) {
	srv := newStarsServer(t, nil, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := Stars(context.Background(), c, "octocat", starConfig(10))
	if err == nil {
		t.Fatal("Stars: want error on non-200 response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}
