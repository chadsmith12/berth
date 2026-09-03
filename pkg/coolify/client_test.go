package coolify_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/coolify"
)

func TestCurrentTeam(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/v1/teams/current" {
			t.Errorf("path %q", r.URL.Path)
		}
		w.Write([]byte(`{"id":3,"name":"Client Work","description":"x"}`))
	}))
	defer srv.Close()

	team, err := coolify.New(srv.URL, "tok123").CurrentTeam(context.Background())
	if err != nil {
		t.Fatalf("current team: %v", err)
	}
	if team.Id != 3 || team.Name != "Client Work" {
		t.Fatalf("got %+v", team)
	}
	if gotAuth != "Bearer tok123" {
		t.Fatalf("auth header %q", gotAuth)
	}
}

func TestDoRequiresToken(t *testing.T) {
	_, err := coolify.New("https://coolify.example.com", "").CurrentTeam(context.Background())
	if !errors.Is(err, coolify.ErrNoToken) {
		t.Fatalf("want ErrNoToken, got %v", err)
	}
}

func TestDoUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := coolify.New(url, "tok").CurrentTeam(context.Background())
	if err == nil || !strings.Contains(err.Error(), "cannot reach") {
		t.Fatalf("want 'cannot reach', got %v", err)
	}
}

func TestDoErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"message":"Unauthenticated."}`))
	}))
	defer srv.Close()

	_, err := coolify.New(srv.URL, "tok").CurrentTeam(context.Background())
	var apiErr *coolify.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want ApiError, got %v", err)
	}
	if apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("status %d", apiErr.Status)
	}
	if !strings.Contains(apiErr.Error(), "token was rejected") {
		t.Fatalf("message %q", apiErr.Error())
	}
}

func TestDoSurfacesForbiddenMessageVerbatim(t *testing.T) {
	const msg = "Missing abilities: deploy, read:sensitive."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"message":"` + msg + `"}`))
	}))
	defer srv.Close()

	_, err := coolify.New(srv.URL, "tok").CurrentTeam(context.Background())
	var apiErr *coolify.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want ApiError, got %v", err)
	}
	if apiErr.Message != msg {
		t.Fatalf("message must pass through verbatim, got %q", apiErr.Message)
	}
}

func TestDoNonJSONErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`<html>Bad Gateway</html>`))
	}))
	defer srv.Close()

	_, err := coolify.New(srv.URL, "tok").CurrentTeam(context.Background())
	var apiErr *coolify.ApiError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want ApiError, got %v", err)
	}
	if !strings.Contains(apiErr.Error(), "[502]") {
		t.Fatalf("message %q", apiErr.Error())
	}
}
