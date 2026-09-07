package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chadsmith12/berth/pkg/config"
)

func TestFindRepoConfig(t *testing.T) {
	root := t.TempDir()

	if _, ok, err := config.FindRepoConfig(root); err != nil || ok {
		t.Fatalf("empty root: ok=%v err=%v", ok, err)
	}

	dotConfig := filepath.Join(root, ".config", "berth.json")
	if err := os.MkdirAll(filepath.Dir(dotConfig), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dotConfig, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	found, ok, err := config.FindRepoConfig(root)
	if err != nil || !ok || found != dotConfig {
		t.Fatalf("dotconfig: %q %v %v", found, ok, err)
	}

	rootConfig := filepath.Join(root, "berth.json")
	if err := os.WriteFile(rootConfig, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	_, _, err = config.FindRepoConfig(root)
	if err == nil {
		t.Fatal("both present must be an error")
	}
	if err := os.Remove(dotConfig); err != nil {
		t.Fatal(err)
	}
	found, ok, err = config.FindRepoConfig(root)
	if err != nil || !ok || found != rootConfig {
		t.Fatalf("root fallback: %q %v %v", found, ok, err)
	}
}

func TestRepoConfigRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".config", "berth.json")
	in := config.RepoConfig{
		Environments: []config.Environment{
			{Name: "production", UUID: "j9e2lm7xssikhtpvr3e9x1jp"},
			{Name: "staging", UUID: "kw8oc4k0ggs4wcw0wkwcgsw8"},
		},
		Coolify: &config.Coolify{
			URL:     "https://coolify.example.com",
			Team:    &config.TeamRef{ID: 3, Name: "Client Work"},
			Project: "pmc",
		},
	}

	if err := config.SaveRepoConfig(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := config.LoadRepoConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if out.Schema != config.SchemaV1 {
		t.Fatalf("schema %q", out.Schema)
	}
	if len(out.Environments) != 2 || out.Environments[1].UUID != "kw8oc4k0ggs4wcw0wkwcgsw8" {
		t.Fatalf("environments %+v", out.Environments)
	}
	if out.Coolify == nil || out.Coolify.Team == nil || out.Coolify.Team.ID != 3 || out.Coolify.Project != "pmc" {
		t.Fatalf("coolify %+v", out.Coolify)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("perm %o, want 644", got)
	}
}

func TestLoadRepoConfigMissingFile(t *testing.T) {
	if _, err := config.LoadRepoConfig(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Fatal("missing file must be an error")
	}
}

func TestLoadRepoConfigRejectsDuplicateEnvironments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "berth.json")
	body := `{"environments": [{"name": "production", "uuid": "a"}, {"name": "production", "uuid": "b"}]}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRepoConfig(path); err == nil {
		t.Fatal("duplicate names must be an error")
	}
}

func TestLoadRepoConfigRejectsEmptyEnvironmentName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "berth.json")
	if err := os.WriteFile(path, []byte(`{"environments": [{"name": "", "uuid": "a"}]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRepoConfig(path); err == nil {
		t.Fatal("empty name must be an error")
	}
}

func TestLoadRepoConfigRejectsTeamWithoutID(t *testing.T) {
	path := filepath.Join(t.TempDir(), "berth.json")
	body := `{"coolify": {"team": {"name": "Client Work"}}}`
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRepoConfig(path); err == nil {
		t.Fatal("team without id must be an error")
	}
}

func TestLoadRepoConfigInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "berth.json")
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := config.LoadRepoConfig(path); err == nil {
		t.Fatal("invalid json must be an error")
	}
}

func TestEnvironmentFor(t *testing.T) {
	cfg := config.RepoConfig{Environments: []config.Environment{
		{Name: "production", UUID: "prod-uuid"},
	}}
	env, ok := cfg.EnvironmentFor("production")
	if !ok || env.UUID != "prod-uuid" {
		t.Fatalf("got %+v %v", env, ok)
	}
	if _, ok := cfg.EnvironmentFor("staging"); ok {
		t.Fatal("unknown environment must not be found")
	}
}
