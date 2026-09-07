package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/chadsmith12/berth/pkg/config"
)

func TestCredentialsRoundtrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	in := config.Credentials{Instances: []config.Instance{{
		URL:   "http://10.190.122.34:8000",
		Teams: []config.TeamToken{{ID: 0, Name: "Root Team", Token: "1|abc"}},
	}}}

	if err := config.SaveCredentials(path, in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := config.LoadCredentials(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(out.Instances) != 1 || out.Instances[0].URL != "http://10.190.122.34:8000" {
		t.Fatalf("got %+v", out)
	}
	if len(out.Instances[0].Teams) != 1 || out.Instances[0].Teams[0].Token != "1|abc" {
		t.Fatalf("got %+v", out.Instances[0].Teams)
	}
}

func TestLoadCredentialsMissingFile(t *testing.T) {
	creds, err := config.LoadCredentials(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error: %v", err)
	}
	if len(creds.Instances) != 0 {
		t.Fatalf("want empty, got %+v", creds)
	}
}

func TestSaveCredentialsFilePerms(t *testing.T) {
	path := filepath.Join(t.TempDir(), "berth", "credentials.json")
	if err := config.SaveCredentials(path, config.Credentials{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("perm %o, want 600", got)
	}
}

func TestUpsertToken(t *testing.T) {
	creds := config.Credentials{}

	config.UpsertToken(&creds, "http://host:8000/", config.TeamToken{ID: 0, Name: "Root Team", Token: "1|a"})
	if len(creds.Instances) != 1 || creds.Instances[0].URL != "http://host:8000" {
		t.Fatalf("trailing slash should be normalized: %+v", creds)
	}

	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 3, Name: "Client Work", Token: "45|b"})
	if len(creds.Instances) != 1 || len(creds.Instances[0].Teams) != 2 {
		t.Fatalf("second team should join instance: %+v", creds)
	}

	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 3, Name: "Client Work", Token: "45|renewed"})
	teams := creds.Instances[0].Teams
	if len(teams) != 2 || teams[1].Token != "45|renewed" {
		t.Fatalf("same team id should replace: %+v", teams)
	}
}

func TestFindToken(t *testing.T) {
	creds := config.Credentials{}
	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 3, Name: "Client Work", Token: "45|b"})

	tok, ok := config.FindToken(creds, "http://host:8000/", 3)
	if !ok || tok != "45|b" {
		t.Fatalf("got %q %v", tok, ok)
	}
	if _, ok := config.FindToken(creds, "http://host:8000", 9); ok {
		t.Fatal("missing team id should not be found")
	}
	if _, ok := config.FindToken(creds, "http://other:8000", 3); ok {
		t.Fatal("other instance should not be found")
	}
}

func TestTokensFor(t *testing.T) {
	creds := config.Credentials{}
	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 3, Name: "Client Work", Token: "45|b"})
	config.UpsertToken(&creds, "http://other:8000", config.TeamToken{ID: 1, Name: "Internal", Token: "1|a"})

	tokens := config.TokensFor(creds, "http://host:8000/")
	if len(tokens) != 1 || tokens[0].ID != 3 {
		t.Fatalf("got %+v", tokens)
	}
	if got := config.TokensFor(creds, "http://missing:8000"); got != nil {
		t.Fatalf("unknown instance should be empty, got %+v", got)
	}
}

func TestRemoveToken(t *testing.T) {
	creds := config.Credentials{}
	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 3, Name: "Client Work", Token: "45|b"})
	config.UpsertToken(&creds, "http://host:8000", config.TeamToken{ID: 1, Name: "Internal", Token: "1|a"})

	if !config.RemoveToken(&creds, "http://host:8000/", 3) {
		t.Fatal("expected removal")
	}
	if _, ok := config.FindToken(creds, "http://host:8000", 3); ok {
		t.Fatal("token should be gone")
	}
	if len(config.TokensFor(creds, "http://host:8000")) != 1 {
		t.Fatalf("other token should remain: %+v", creds)
	}

	if !config.RemoveToken(&creds, "http://host:8000", 1) {
		t.Fatal("expected removal")
	}
	if len(creds.Instances) != 0 {
		t.Fatalf("empty instance should be dropped: %+v", creds.Instances)
	}

	if config.RemoveToken(&creds, "http://host:8000", 9) {
		t.Fatal("removing a missing token should report false")
	}
}

func TestTruncateToken(t *testing.T) {
	if got := config.TruncateToken("12|verysecret"); got != "12|..." {
		t.Fatalf("got %q", got)
	}
	if got := config.TruncateToken("garbled"); got != "..." {
		t.Fatalf("got %q", got)
	}
}
