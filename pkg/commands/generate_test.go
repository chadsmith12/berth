package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func generateProject(t *testing.T, extraRequires ...string) string {
	t.Helper()
	dir := t.TempDir()
	composer := `{"require": {"laravel/framework": "^11.0", "php": "^8.2"`
	for _, r := range extraRequires {
		composer += `, ` + r
	}
	composer += `}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestGenerateWritesFilesNonInteractive(t *testing.T) {
	dir := generateProject(t)

	code, out, errB := runCLI([]string{
		"generate", "--path", dir, "--workers", "2", "--scheduler=false", "--port", "8080",
	}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, status := range []string{"[written] docker/app/Dockerfile", "[written] .dockerignore", "[written] docker-compose.production.yml"} {
		if !strings.Contains(out, status) {
			t.Fatalf("missing %q in %q", status, out)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.production.yml")); err != nil {
		t.Fatalf("compose: %v", err)
	}
}

func TestGenerateSecondRunIsUnchanged(t *testing.T) {
	dir := generateProject(t)
	args := []string{"generate", "--path", dir, "--workers", "2", "--scheduler=false", "--port", "8080"}
	if code, _, errB := runCLI(args, ""); code != 0 {
		t.Fatalf("first exit %d, stderr %q", code, errB)
	}
	code, out, _ := runCLI(args, "")
	if code != 0 {
		t.Fatal("second run must succeed")
	}
	if strings.Contains(out, "[written]") {
		t.Fatalf("second run must not rewrite, got %q", out)
	}
	if !strings.Contains(out, "[unchanged] docker/app/Dockerfile") {
		t.Fatalf("expected unchanged statuses, got %q", out)
	}
}

func TestGenerateMissingSchedulerNonInteractive(t *testing.T) {
	dir := generateProject(t)

	code, _, errB := runCLI([]string{"generate", "--path", dir, "--workers", "1", "--port", "8080"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "missing --scheduler") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestGenerateMissingPortNonInteractive(t *testing.T) {
	dir := generateProject(t)

	code, _, errB := runCLI([]string{"generate", "--path", dir, "--workers", "1", "--scheduler=false"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "missing --port") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestGenerateMissingWorkersNonInteractive(t *testing.T) {
	dir := generateProject(t)

	code, _, errB := runCLI([]string{"generate", "--path", dir, "--scheduler=false", "--port", "8080"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "missing --workers") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestGenerateHorizonSkipsWorkers(t *testing.T) {
	dir := generateProject(t, `"laravel/horizon": "^5.0"`)

	code, out, errB := runCLI([]string{"generate", "--path", dir, "--scheduler=false", "--port", "8080"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	body, err := os.ReadFile(filepath.Join(dir, "docker-compose.production.yml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	if !strings.Contains(s, "php artisan horizon") || strings.Contains(s, "queue:work") {
		t.Fatalf("horizon topology wrong:\n%s", s)
	}
}

func TestGenerateEnvNamesComposeFile(t *testing.T) {
	dir := generateProject(t)

	code, out, errB := runCLI([]string{
		"generate", "--path", dir, "--workers", "1", "--scheduler=false", "--port", "8080", "--env", "staging",
	}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.staging.yml")); err != nil {
		t.Fatalf("staging compose: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker-compose.staging.yml"))
	if !strings.Contains(string(body), "APP_ENV: staging") {
		t.Fatalf("APP_ENV must follow --env:\n%s", body)
	}
}
