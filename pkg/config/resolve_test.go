package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/config"
)

func writeRepoConfig(t *testing.T, root, rel, body string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func saveUserProfile(t *testing.T, path, name, url string, teamID int, teamName string) {
	t.Helper()
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: name, URL: url, Team: &config.TeamRef{ID: teamID, Name: teamName}})
	cfg.Default = name
	if err := config.SaveUserConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
}

// isolateHome points the user config directory at a temp dir, so tests that
// omit UserPath cannot see the developer's real profiles.
func isolateHome(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

const repoConfigBody = `{
  "environments": [{"name": "production", "uuid": "prod-uuid"}],
  "coolify": {
    "url": "https://repo.example.com",
    "team": {"id": 3, "name": "Client Work"},
    "project": "pmc"
  }
}`

func TestResolveFromRepoConfig(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", repoConfigBody)

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://repo.example.com" || p.Team == nil || p.Team.ID != 3 || p.Team.Name != "Client Work" || p.Project != "pmc" {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveFromDefaultProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://user.example.com" || p.Team == nil || p.Team.ID != 1 || p.Team.Name != "Internal" {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveFromNamedProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	cfg, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertProfile(config.Profile{Name: "client", URL: "https://client.example.com", Team: &config.TeamRef{ID: 3, Name: "Client Work"}})
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath, Overrides: config.Overrides{Profile: "client"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://client.example.com" || p.Team == nil || p.Team.ID != 3 || p.Team.Name != "Client Work" {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveBERTHProfileSelectsProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	cfg, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg.UpsertProfile(config.Profile{Name: "client", URL: "https://client.example.com", Team: &config.TeamRef{ID: 3, Name: "Client Work"}})
	if err := config.SaveUserConfig(userPath, cfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_PROFILE", "client")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://client.example.com" || p.Team == nil || p.Team.ID != 3 {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveMissingProfileErrorNamesKnownProfiles(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")

	_, err := config.Resolve(config.ResolveOptions{UserPath: userPath, Overrides: config.Overrides{Profile: "nope"}})
	if err == nil {
		t.Fatal("unknown profile must be an error")
	}
	for _, want := range []string{`no profile named "nope"`, "internal"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must name %q", err, want)
		}
	}
}

func TestResolveProfileBeatsRepoAndDefault(t *testing.T) {
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", repoConfigBody)
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	t.Setenv("BERTH_PROFILE", "internal")

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root, UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://user.example.com" || p.Team == nil || p.Team.ID != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveFieldOverrideBeatsProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	t.Setenv("BERTH_URL", "https://env.example.com")
	t.Setenv("BERTH_TEAM", "7")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://env.example.com" || p.Team == nil || p.Team.ID != 7 {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveOverrideTeamZero(t *testing.T) {
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", repoConfigBody)
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	t.Setenv("BERTH_URL", "https://env.example.com")
	t.Setenv("BERTH_TEAM", "7")

	zero := 0
	p, err := config.Resolve(config.ResolveOptions{
		RepoRoot:  root,
		UserPath:  userPath,
		Overrides: config.Overrides{TeamID: &zero},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Team == nil || p.Team.ID != 0 {
		t.Fatalf("an explicit --team 0 must win, got %+v", p)
	}
}

func TestResolveNilTeamDoesNotOverride(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 4, "Internal")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Team == nil || p.Team.ID != 4 {
		t.Fatalf("nil override must not clobber the profile team, got %+v", p)
	}
}

func TestResolveTeamZeroFromProfile(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "root", "https://user.example.com", 0, "Root Team")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Team == nil || p.Team.ID != 0 || p.Team.Name != "Root Team" {
		t.Fatalf("team id 0 must still resolve, got %+v", p)
	}
}

func TestResolvePrecedenceOverridesBeatsAll(t *testing.T) {
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", repoConfigBody)
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")
	t.Setenv("BERTH_URL", "https://env.example.com")
	t.Setenv("BERTH_TEAM", "7")
	t.Setenv("BERTH_PROJECT", "env-project")

	teamID := 9
	p, err := config.Resolve(config.ResolveOptions{
		RepoRoot: root,
		UserPath: userPath,
		Overrides: config.Overrides{
			URL:     "https://flag.example.com",
			TeamID:  &teamID,
			Project: "flag-project",
		},
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://flag.example.com" || p.Team == nil || p.Team.ID != 9 || p.Project != "flag-project" {
		t.Fatalf("got %+v", p)
	}
}

func TestResolvePrecedenceEnvBeatsConfigs(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeRepoConfig(t, root, "berth.json", repoConfigBody)
	t.Setenv("BERTH_URL", "https://env.example.com/")
	t.Setenv("BERTH_TEAM", "7")

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://env.example.com" || p.Team == nil || p.Team.ID != 7 {
		t.Fatalf("got %+v", p)
	}
	if p.Team.Name != "" {
		t.Fatalf("team id 7 has no configured name, got %q", p.Team.Name)
	}
}

func TestResolveTeamNameFilledFromMatchingConfig(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeRepoConfig(t, root, "berth.json", repoConfigBody)
	t.Setenv("BERTH_URL", "https://env.example.com")
	t.Setenv("BERTH_TEAM", "3")

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Team == nil || p.Team.ID != 3 || p.Team.Name != "Client Work" {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveOptionalTeamLeavesTeamNil(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", `{"coolify": {"url": "https://repo.example.com"}}`)

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root, OptionalTeam: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://repo.example.com" || p.Team != nil {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveOptionalTeamStillResolvesWhenConfigured(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath, OptionalTeam: true})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Team == nil || p.Team.ID != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveMissingURLErrorsNamingPlaces(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")

	_, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err == nil {
		t.Fatal("missing url must be an error")
	}
	for _, want := range []string{"--url", "BERTH_URL", "--profile", ".config/berth.json", "berth auth login"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must name %q", err, want)
		}
	}
}

func TestResolveMissingTeamErrorsNamingPlaces(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", `{"coolify": {"url": "https://repo.example.com"}}`)

	_, err := config.Resolve(config.ResolveOptions{RepoRoot: root, UserPath: userPath})
	if err == nil {
		t.Fatal("missing team must be an error")
	}
	for _, want := range []string{"--team", "BERTH_TEAM", "--profile", "team", "berth auth login"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q must name %q", err, want)
		}
	}
}

func TestResolveNonIntegerBERTHTeamIsError(t *testing.T) {
	isolateHome(t)
	t.Setenv("BERTH_URL", "https://env.example.com")
	t.Setenv("BERTH_TEAM", "Client Work")

	_, err := config.Resolve(config.ResolveOptions{})
	if err == nil || !strings.Contains(err.Error(), "BERTH_TEAM") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveBothRepoConfigsIsError(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	writeRepoConfig(t, root, ".config/berth.json", repoConfigBody)
	writeRepoConfig(t, root, "berth.json", repoConfigBody)

	_, err := config.Resolve(config.ResolveOptions{RepoRoot: root})
	if err == nil || !strings.Contains(err.Error(), "remove one") {
		t.Fatalf("got %v", err)
	}
}

func TestResolveExplicitConfigPathSkipsScan(t *testing.T) {
	isolateHome(t)
	root := t.TempDir()
	other := filepath.Join(t.TempDir(), "custom.json")
	writeRepoConfig(t, root, ".config/berth.json", `{"coolify": {"url": "https://repo.example.com", "team": {"id": 3}}}`)
	writeRepoConfig(t, other, "", `{"coolify": {"url": "https://custom.example.com", "team": {"id": 5}}}`)

	p, err := config.Resolve(config.ResolveOptions{RepoRoot: root, ConfigPath: other})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.URL != "https://custom.example.com" || p.Team == nil || p.Team.ID != 5 {
		t.Fatalf("got %+v", p)
	}
}

func TestResolveProjectOptional(t *testing.T) {
	userPath := filepath.Join(t.TempDir(), "config.json")
	saveUserProfile(t, userPath, "internal", "https://user.example.com", 1, "Internal")

	p, err := config.Resolve(config.ResolveOptions{UserPath: userPath})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Project != "" {
		t.Fatalf("project should be empty, got %q", p.Project)
	}
}
