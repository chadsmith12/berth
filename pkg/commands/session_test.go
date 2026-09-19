package commands_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/commands"
	"github.com/chadsmith12/berth/pkg/config"
	"github.com/chadsmith12/berth/pkg/output"
)

func openCtx(profile string) *cli.CmdContext {
	return &cli.CmdContext{
		Globals: cli.Globals{Profile: profile},
		Command: &cli.Command{Name: "test"},
		Stdout:  io.Discard,
		Stderr:  io.Discard,
	}
}

func TestOpenResolvesFromDefaultProfile(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "root", URL: srv.URL, Team: &config.TeamRef{ID: 0, Name: "Root Team"}})
	cfg.Default = "root"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}
	creds := config.Credentials{}
	config.UpsertToken(&creds, srv.URL, config.TeamToken{ID: 0, Name: "Root Team", Token: testToken})
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_TOKEN", "")

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sess.Placement.URL != srv.URL || sess.Placement.Team == nil || sess.Placement.Team.ID != 0 {
		t.Fatalf("placement %+v", sess.Placement)
	}
	if sess.Client == nil {
		t.Fatal("client must be built")
	}
}

func TestOpenBERTHTokenOverridesStored(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", "7|pipetoken")

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sess.Placement.URL != srv.URL {
		t.Fatalf("sess %+v", sess.Placement)
	}
	if _, err := sess.Client.CurrentTeam(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.authHeader != "Bearer 7|pipetoken" {
		t.Fatalf("token on the wire = %q", srv.authHeader)
	}
}

func TestOpenMissingTokenIsUsageError(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")
	t.Setenv("BERTH_TOKEN", "")

	_, err := commands.Open(openCtx(""), commands.SessionOptions{OptionalTeam: true})
	if err == nil || output.CodeOf(err) != 2 {
		t.Fatalf("want usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no token stored") {
		t.Fatalf("error %q", err)
	}
}

func TestOpenTeamTokenRequiredWithoutOptional(t *testing.T) {
	srv := newFakeCoolify(t)
	defer srv.Close()
	credPath := withIsolatedConfig(t)
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", "")

	creds := config.Credentials{}
	config.UpsertToken(&creds, srv.URL, config.TeamToken{ID: 0, Name: "Root Team", Token: "1|aaa"})
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "other", URL: srv.URL, Team: &config.TeamRef{ID: 5, Name: "Other Team"}})
	cfg.Default = "other"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	_, err := commands.Open(openCtx(""), commands.SessionOptions{})
	if err == nil || output.CodeOf(err) != 2 {
		t.Fatalf("want usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "no token stored for team") {
		t.Fatalf("error %q", err)
	}

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{OptionalTeam: true})
	if err != nil {
		t.Fatalf("optional open: %v", err)
	}
	if _, err := sess.Client.CurrentTeam(context.Background()); err != nil {
		t.Fatal(err)
	}
	if srv.authHeader != "Bearer 1|aaa" {
		t.Fatalf("optional mode should fall back to the single token, got %q", srv.authHeader)
	}
}

func TestOpenVerifyTeamMismatchIsAuthError(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", testToken)

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "other", URL: srv.URL, Team: &config.TeamRef{ID: 5, Name: "Other Team"}})
	cfg.Default = "other"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	_, err := commands.Open(openCtx(""), commands.SessionOptions{VerifyTeam: true})
	if err == nil || output.CodeOf(err) != 3 {
		t.Fatalf("want auth error, got %v", err)
	}
	for _, want := range []string{"Root Team", "team id 5"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must name %q", err, want)
		}
	}
}

func TestOpenVerifyTeamAcceptsMatch(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", testToken)

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "root", URL: srv.URL, Team: &config.TeamRef{ID: 0, Name: "Root Team"}})
	cfg.Default = "root"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{VerifyTeam: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sess.Client == nil {
		t.Fatal("client must be built")
	}
}

func TestOpenVerifyTeamWithoutTeamSkipsCheck(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", testToken)

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{VerifyTeam: true})
	if err != nil {
		t.Fatalf("pipeline mode must not require a team: %v", err)
	}
	if sess.Client == nil {
		t.Fatal("client must be built")
	}
}

func TestOpenInvalidBERTHTokenFormat(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")
	t.Setenv("BERTH_TOKEN", "justthesecret")

	_, err := commands.Open(openCtx(""), commands.SessionOptions{})
	if err == nil || output.CodeOf(err) != 2 {
		t.Fatalf("want usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "invalid token format") {
		t.Fatalf("error %q", err)
	}
}

func TestOpenPlacementOnlySkipsToken(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")
	t.Setenv("BERTH_TOKEN", "")

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{PlacementOnly: true, OptionalTeam: true})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sess.Client != nil {
		t.Fatalf("placement-only must not build a client, got %+v", sess)
	}
	if sess.Placement.URL != "https://coolify.example.com" {
		t.Fatalf("placement %+v", sess.Placement)
	}
}

func TestOpenTeamFlagMustBeAnInteger(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")
	t.Setenv("BERTH_TOKEN", testToken)

	_, err := commands.Open(openCtx(""), commands.SessionOptions{Team: "Client Work"})
	if err == nil || output.CodeOf(err) != 2 {
		t.Fatalf("want usage error, got %v", err)
	}
	if !strings.Contains(err.Error(), "--team must be a team id") {
		t.Fatalf("error %q", err)
	}
}

func TestOpenGlobalsProfileSelectsIdentity(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_TOKEN", testToken)

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "internal", URL: "https://internal.example.com", Team: &config.TeamRef{ID: 1, Name: "Internal"}})
	cfg.UpsertProfile(config.Profile{Name: "client", URL: srv.URL, Team: &config.TeamRef{ID: 3, Name: "Client Work"}})
	cfg.Default = "internal"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	sess, err := commands.Open(openCtx("client"), commands.SessionOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if sess.Placement.URL != srv.URL || sess.Placement.Team == nil || sess.Placement.Team.ID != 3 {
		t.Fatalf("placement %+v", sess.Placement)
	}
}

func TestOpenClientCarriesTeamFor404s(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", testToken)

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "root", URL: srv.URL, Team: &config.TeamRef{ID: 0, Name: "Root Team"}})
	cfg.Default = "root"
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	sess, err := commands.Open(openCtx(""), commands.SessionOptions{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_, err = sess.Client.CurrentTeam(context.Background())
	if err == nil {
		t.Fatal("expected a 404")
	}
	if !strings.Contains(err.Error(), "Root Team") {
		t.Fatalf("404 must name the team it acts as, got %q", err)
	}
}
