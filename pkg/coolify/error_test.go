package coolify_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chadsmith12/berth/pkg/coolify"
)

func TestError422FieldErrorsIncluded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		w.Write([]byte(`{"message":"Validation failed.","errors":{"domain":["The domain field is required."],"port":["The port must be an integer."]}}`))
	}))
	defer srv.Close()

	_, err := coolify.New(srv.URL, "tok").CurrentTeam(context.Background())
	if err == nil {
		t.Fatal("expected a 422 error")
	}
	for _, want := range []string{"domain: The domain field is required.", "port: The port must be an integer."} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("missing %q in %q", want, err.Error())
		}
	}
}

func TestError404NamesWrongTeam(t *testing.T) {
	plain := &coolify.ApiError{
		Status: http.StatusNotFound,
		Method: http.MethodGet,
		Path:   "/api/v1/applications",
	}
	if !strings.Contains(plain.Error(), "wrong team") {
		t.Fatalf("message %q", plain.Error())
	}

	scoped := &coolify.ApiError{
		Status: http.StatusNotFound,
		Method: http.MethodGet,
		Path:   "/api/v1/applications",
		Team:   "Client Work (3)",
	}
	if !strings.Contains(scoped.Error(), "Currently acting as: Client Work (3)") {
		t.Fatalf("message %q", scoped.Error())
	}
}

func TestError409DomainConflict(t *testing.T) {
	conflict := &coolify.ApiError{
		Status: http.StatusConflict,
		Conflicts: []coolify.DomainConflict{
			{Domain: "test.example.com", ResourceName: "test", ResourceUuid: "abc123", ResourceType: "application"},
		},
	}
	got := conflict.Error()
	for _, want := range []string{"test.example.com", "test", "abc123", "--force-domain"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %q", want, got)
		}
	}

	plain := &coolify.ApiError{Status: http.StatusConflict, Message: "duplicate resource"}
	if !strings.Contains(plain.Error(), "[409] conflict: duplicate resource") {
		t.Fatalf("message %q", plain.Error())
	}
}

func TestError429RetryAfter(t *testing.T) {
	err := &coolify.ApiError{Status: http.StatusTooManyRequests, Message: "slow down", RetryAfter: 2 * time.Second}
	if !strings.Contains(err.Error(), "Retry after 2s") {
		t.Fatalf("message %q", err.Error())
	}
}

func TestErrorDefault(t *testing.T) {
	err := &coolify.ApiError{Status: http.StatusTeapot, Message: "teapot"}
	if got := err.Error(); got != "[418] teapot" {
		t.Fatalf("message %q", got)
	}
}
