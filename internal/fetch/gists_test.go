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

type gistItem struct {
	ID          string    `json:"id"`
	URL         string    `json:"html_url"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type gistsServer struct {
	*httptest.Server
	queries []string
}

func newGistsServer(t *testing.T, pages [][]gistItem, status int) *gistsServer {
	t.Helper()
	return newGistsServerForUser(t, "octocat", pages, status)
}

func newGistsServerForUser(t *testing.T, user string, pages [][]gistItem, status int) *gistsServer {
	t.Helper()

	s := &gistsServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+user+"/gists", func(w http.ResponseWriter, r *http.Request) {
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
			_ = json.NewEncoder(w).Encode([]gistItem{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func gistConfig(limit int) config.GitHub {
	return config.GitHub{Gists: &config.Section{Limit: limit}}
}

func TestGistsNilSectionMakesNoRequest(t *testing.T) {
	srv := newGistsServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Gists(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Gists: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestGistsZeroLimitMakesNoRequest(t *testing.T) {
	srv := newGistsServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Gists(context.Background(), c, "octocat", gistConfig(0))
	if err != nil {
		t.Fatalf("Gists: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestGistsPopulatesEveryModelField(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 12, 30, 0, 0, time.UTC)
	pages := [][]gistItem{{{
		ID:          "gist123",
		URL:         "https://gist.github.com/octocat/gist123",
		Description: "my first gist",
		CreatedAt:   createdAt,
	}}}
	srv := newGistsServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Gists(context.Background(), c, "octocat", gistConfig(10))
	if err != nil {
		t.Fatalf("Gists: %v", err)
	}

	want := model.Gist{
		ID:          "gist123",
		URL:         "https://gist.github.com/octocat/gist123",
		Description: "my first gist",
		CreatedAt:   createdAt,
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestGistsStopsAtLimit(t *testing.T) {
	createdAt := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	pages := [][]gistItem{
		{{ID: "g-1", URL: "url1", Description: "d1", CreatedAt: createdAt}},
		{{ID: "g-extra", URL: "url-extra", Description: "d-extra", CreatedAt: createdAt}},
	}
	srv := newGistsServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Gists(context.Background(), c, "octocat", gistConfig(1))
	if err != nil {
		t.Fatalf("Gists: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d gists, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (must stop once Limit is reached)", len(srv.queries))
	}
}

func TestGistsReturnsErrorFromList(t *testing.T) {
	srv := newGistsServer(t, nil, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := Gists(context.Background(), c, "octocat", gistConfig(10))
	if err == nil {
		t.Fatal("Gists: want error on non-200 response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}
