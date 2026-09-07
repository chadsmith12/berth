package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chadsmith12/berth/pkg/config"
)

func TestUserConfigRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := config.UserConfig{Default: "client-work"}
	in.UpsertProfile(config.Profile{
		Name: "client-work",
		URL:  "https://coolify.example.com",
		Team: &config.TeamRef{ID: 3, Name: "Client Work"},
	})

	if err := config.SaveUserConfig(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := config.LoadUserConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.Default != "client-work" || len(out.Profiles) != 1 {
		t.Fatalf("got %+v", out)
	}
	p := out.Profiles[0]
	if p.URL != "https://coolify.example.com" || p.Team == nil || p.Team.ID != 3 {
		t.Fatalf("got %+v", p)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("perm %o, want 644", got)
	}
}

func TestLoadUserConfigMissingFile(t *testing.T) {
	cfg, err := config.LoadUserConfig(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if cfg.Default != "" || len(cfg.Profiles) != 0 {
		t.Fatalf("want empty, got %+v", cfg)
	}
}

func TestLoadUserConfigRejectsDuplicateProfileNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"default": "a", "profiles": [
		{"name": "a", "url": "https://a.example.com", "team": {"id": 1}},
		{"name": "a", "url": "https://b.example.com", "team": {"id": 2}}
	]}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadUserConfig(path); err == nil {
		t.Fatal("duplicate profile names must be an error")
	}
}

func TestLoadUserConfigRejectsDanglingDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := `{"default": "nope", "profiles": [
		{"name": "a", "url": "https://a.example.com", "team": {"id": 1}}
	]}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadUserConfig(path); err == nil {
		t.Fatal("default naming a missing profile must be an error")
	}
}

func TestLoadUserConfigRejectsProfileWithoutTeam(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"profiles": [{"name": "a", "url": "https://a.example.com"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadUserConfig(path); err == nil {
		t.Fatal("profile without team must be an error")
	}
}

func TestProfileConflict(t *testing.T) {
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "a", URL: "https://a.example.com", Team: &config.TeamRef{ID: 1, Name: "Internal"}})

	if cfg.ProfileConflict("a", "https://a.example.com/", 1) {
		t.Fatal("same identity is not a conflict")
	}
	if cfg.ProfileConflict("b", "https://a.example.com", 1) {
		t.Fatal("unknown name is not a conflict")
	}
	if !cfg.ProfileConflict("a", "https://other.example.com", 1) {
		t.Fatal("same name, other instance is a conflict")
	}
	if !cfg.ProfileConflict("a", "https://a.example.com", 2) {
		t.Fatal("same name, other team is a conflict")
	}
}

func TestSetDefault(t *testing.T) {
	cfg := config.UserConfig{}
	cfg.UpsertProfile(config.Profile{Name: "a", URL: "https://a.example.com", Team: &config.TeamRef{ID: 1}})
	cfg.UpsertProfile(config.Profile{Name: "b", URL: "https://b.example.com", Team: &config.TeamRef{ID: 2}})

	if err := cfg.SetDefault("b"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if cfg.Default != "b" {
		t.Fatalf("default %q", cfg.Default)
	}
	if err := cfg.SetDefault("nope"); err == nil {
		t.Fatal("unknown profile must be an error")
	}
}

func TestProfileSlug(t *testing.T) {
	cases := map[string]string{
		"Software Celebrate":     "software-celebrate",
		"Client Work":            "client-work",
		"  Spaced   Out  ":       "spaced-out",
		"Trailing dash ---":      "trailing-dash",
		"already-slugged":        "already-slugged",
		"Symbols & Such! (2024)": "symbols-such-2024",
		"":                       "",
	}
	for in, want := range cases {
		if got := config.ProfileSlug(in); got != want {
			t.Errorf("ProfileSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUserConfigPathUnderUserConfigDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	path, err := config.UserConfigPath()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if want := filepath.Join(dir, "berth", "config.json"); path != want {
		t.Fatalf("got %q, want %q", path, want)
	}
}
