package fetch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Odilhao/readme-builder/internal/config"
	"github.com/Odilhao/readme-builder/internal/model"
)

type followerItem struct {
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
	HTMLURL   string `json:"html_url"`
}

type followersServer struct {
	*httptest.Server
	queries []string
}

func newFollowersServer(t *testing.T, pages [][]followerItem, status int) *followersServer {
	t.Helper()
	return newFollowersServerForUser(t, "octocat", pages, status)
}

func newFollowersServerForUser(t *testing.T, user string, pages [][]followerItem, status int) *followersServer {
	t.Helper()

	s := &followersServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/users/"+user+"/followers", func(w http.ResponseWriter, r *http.Request) {
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
			_ = json.NewEncoder(w).Encode([]followerItem{})
			return
		}
		_ = json.NewEncoder(w).Encode(pages[page-1])
	})
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)
	return s
}

func followerConfig(limit int) config.GitHub {
	return config.GitHub{Followers: &config.Section{Limit: limit}}
}

func TestFollowersNilSectionMakesNoRequest(t *testing.T) {
	srv := newFollowersServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Followers(context.Background(), c, "octocat", config.GitHub{})
	if err != nil {
		t.Fatalf("Followers: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestFollowersZeroLimitMakesNoRequest(t *testing.T) {
	srv := newFollowersServer(t, nil, 0)
	c := testClient(t, srv.URL)

	got, err := Followers(context.Background(), c, "octocat", followerConfig(0))
	if err != nil {
		t.Fatalf("Followers: %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if len(srv.queries) != 0 {
		t.Errorf("observed %d requests, want 0", len(srv.queries))
	}
}

func TestFollowersPopulatesEveryModelField(t *testing.T) {
	pages := [][]followerItem{{{
		Login:     "example-user",
		AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4",
		HTMLURL:   "https://github.com/example-user",
	}}}
	srv := newFollowersServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Followers(context.Background(), c, "octocat", followerConfig(10))
	if err != nil {
		t.Fatalf("Followers: %v", err)
	}

	want := model.Follower{
		Login:     "example-user",
		AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4",
		URL:       "https://github.com/example-user",
	}
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFollowersStopsAtLimit(t *testing.T) {
	pages := [][]followerItem{
		{{Login: "user-1", AvatarURL: "url1", HTMLURL: "https://github.com/user-1"}},
		{{Login: "user-extra", AvatarURL: "url-extra", HTMLURL: "https://github.com/user-extra"}},
	}
	srv := newFollowersServer(t, pages, 0)
	c := testClient(t, srv.URL)

	got, err := Followers(context.Background(), c, "octocat", followerConfig(1))
	if err != nil {
		t.Fatalf("Followers: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d followers, want 1", len(got))
	}
	if len(srv.queries) != 1 {
		t.Errorf("observed %d requests, want 1 (must stop once Limit is reached)", len(srv.queries))
	}
}

func TestFollowersReturnsErrorFromList(t *testing.T) {
	srv := newFollowersServer(t, nil, http.StatusInternalServerError)
	c := testClient(t, srv.URL)

	got, err := Followers(context.Background(), c, "octocat", followerConfig(10))
	if err == nil {
		t.Fatal("Followers: want error on non-200 response, got nil")
	}
	if got != nil {
		t.Errorf("got %+v, want nil result alongside the error", got)
	}
}
