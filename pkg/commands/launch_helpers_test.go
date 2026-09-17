package commands

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComposeEnvSet(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"docker-compose.production.yml", "docker-compose.staging.yml", "Dockerfile"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	got := composeEnvSet(dir)
	want := map[string]bool{"production": true, "staging": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("composeEnvSet = %v, want %v", got, want)
	}
}

func TestComposeScanDirAtAppDir(t *testing.T) {
	dir := t.TempDir()
	writeCompose(t, dir, "staging")
	got, err := composeScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("composeScanDir = %q, want %q", got, dir)
	}
}

func TestComposeScanDirDescendsIntoMonorepoSubdir(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "web")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	writeCompose(t, app, "production")
	// a git root without a compose of its own must descend into the app subdir
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := composeScanDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "web" {
		t.Fatalf("composeScanDir = %q, want the web subdir", got)
	}
	if !strings.HasSuffix(got, "web") {
		t.Fatalf("composeScanDir = %q, want …/web", got)
	}
}

func writeCompose(t *testing.T, dir, env string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose."+env+".yml"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
}
