package generate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/generate"
	"github.com/chadsmith12/berth/pkg/plan"
)

func fixtureProject(t *testing.T, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	composer := `{"require": {"laravel/framework": "^11.0", "php": "^8.2"`
	for k, v := range extra {
		composer += `, "` + k + `": "` + v + `"`
	}
	composer += `}}`
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func testPlan(dir string) plan.Plan {
	return plan.Plan{Project: plan.Project{
		Path:       dir,
		Name:       "fixture",
		PhpVersion: "8.2",
		Workers:    2,
		Scheduler:  true,
		Port:       8080,
		Env:        "production",
	}}
}

func TestGenerateWritesNewFiles(t *testing.T) {
	dir := fixtureProject(t, nil)
	res, err := generate.Generate(testPlan(dir), generate.Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(res.Files) != 4 {
		t.Fatalf("expected 4 files, got %+v", res.Files)
	}
	for _, f := range res.Files {
		if f.Status != generate.StatusWritten {
			t.Fatalf("expected written, got %+v", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "docker-compose.production.yml")); err != nil {
		t.Fatalf("compose file: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker-compose.production.yml"))
	for _, want := range []string{"APP_ENV: production", `SERVER_NAME: ":8080"`, `"8080"`, "scheduler:", "queue:work"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("compose missing %q:\n%s", want, body)
		}
	}
}

func TestGenerateUnchangedNeedsNoForce(t *testing.T) {
	dir := fixtureProject(t, nil)
	if _, err := generate.Generate(testPlan(dir), generate.Options{}); err != nil {
		t.Fatal(err)
	}
	res, err := generate.Generate(testPlan(dir), generate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		if f.Status != generate.StatusUnchanged {
			t.Fatalf("expected unchanged, got %+v", f)
		}
	}
}

func TestGenerateRefusesChangedWithoutForce(t *testing.T) {
	dir := fixtureProject(t, nil)
	if _, err := generate.Generate(testPlan(dir), generate.Options{}); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(dir, "docker-compose.production.yml")
	before, _ := os.ReadFile(compose)
	if err := os.WriteFile(compose, append([]byte("# edited\n"), before...), 0644); err != nil {
		t.Fatal(err)
	}

	res, err := generate.Generate(testPlan(dir), generate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	var composeResult generate.FileResult
	for _, f := range res.Files {
		if strings.HasSuffix(f.Path, ".yml") {
			composeResult = f
		} else if f.Status != generate.StatusUnchanged {
			t.Fatalf("untouched file should be unchanged, got %+v", f)
		}
	}
	if composeResult.Status != generate.StatusChanged || composeResult.Diff == "" {
		t.Fatalf("expected changed with diff, got %+v", composeResult)
	}
	after, _ := os.ReadFile(compose)
	if string(after) != "# edited\n"+string(before) {
		t.Fatal("file must not be overwritten without --force")
	}

	res, err = generate.Generate(testPlan(dir), generate.Options{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		if f.Path == composeResult.Path && f.Status != generate.StatusWritten {
			t.Fatalf("expected written with force, got %+v", f)
		}
	}
	after, _ = os.ReadFile(compose)
	if strings.HasPrefix(string(after), "# edited") {
		t.Fatal("force must overwrite the edited file")
	}
}

func TestGenerateDryRunTouchesNothing(t *testing.T) {
	dir := fixtureProject(t, nil)
	if _, err := generate.Generate(testPlan(dir), generate.Options{}); err != nil {
		t.Fatal(err)
	}
	compose := filepath.Join(dir, "docker-compose.production.yml")
	before, _ := os.ReadFile(compose)

	res, err := generate.Generate(testPlan(dir), generate.Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range res.Files {
		if f.Status != generate.StatusUnchanged {
			t.Fatalf("expected unchanged in dry-run, got %+v", f)
		}
	}
	after, _ := os.ReadFile(compose)
	if string(after) != string(before) {
		t.Fatal("dry-run must not write")
	}
}

func TestGenerateHorizonTopology(t *testing.T) {
	dir := fixtureProject(t, map[string]string{"laravel/horizon": "^5.0"})
	p := testPlan(dir)
	p.Project.HasHorizon = true
	res, err := generate.Generate(p, generate.Options{Env: "staging"})
	if err != nil {
		t.Fatal(err)
	}
	var compose generate.FileResult
	for _, f := range res.Files {
		if strings.HasSuffix(f.Path, ".yml") {
			compose = f
		}
	}
	if !strings.Contains(compose.Path, "docker-compose.staging.yml") {
		t.Fatalf("env must name the compose file, got %q", compose.Path)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker-compose.staging.yml"))
	s := string(body)
	for _, want := range []string{"APP_ENV: staging", "horizon:", "php artisan horizon", "QUEUE_CONNECTION: redis", "CACHE_STORE: redis", "REDIS_HOST: ${REDIS_HOST}"} {
		if !strings.Contains(s, want) {
			t.Fatalf("compose missing %q:\n%s", want, s)
		}
	}
	for _, absent := range []string{"worker-1", "queue:work"} {
		if strings.Contains(s, absent) {
			t.Fatalf("horizon topology must not contain %q:\n%s", absent, s)
		}
	}
}

func TestGenerateSchedulerOmitted(t *testing.T) {
	dir := fixtureProject(t, nil)
	p := testPlan(dir)
	p.Project.Scheduler = false
	res, err := generate.Generate(p, generate.Options{})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker-compose.production.yml"))
	if strings.Contains(string(body), "schedule:work") {
		t.Fatalf("scheduler service must be absent:\n%s", body)
	}
	_ = res
}

func TestGenerateSsrScriptNameRendered(t *testing.T) {
	dir := fixtureProject(t, nil)
	p := testPlan(dir)
	p.Project.HasSsr = true
	p.Project.SsrScript = "build:inertia-ssr"
	if _, err := generate.Generate(p, generate.Options{}); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "docker", "app", "Dockerfile"))
	if !strings.Contains(string(body), "npm run build:inertia-ssr") {
		t.Fatalf("dockerfile must render the detected ssr script:\n%s", body)
	}
}

func TestGenerateValidatesDecisions(t *testing.T) {
	dir := fixtureProject(t, nil)

	p := testPlan(dir)
	p.Project.Port = 0
	if _, err := generate.Generate(p, generate.Options{}); err == nil || !strings.Contains(err.Error(), "--port") {
		t.Fatalf("missing port must error, got %v", err)
	}

	p = testPlan(dir)
	p.Project.Workers = -1
	if _, err := generate.Generate(p, generate.Options{}); err == nil || !strings.Contains(err.Error(), "--workers") {
		t.Fatalf("missing workers must error, got %v", err)
	}

	p = testPlan(dir)
	p.Project.HasSsr = true
	if _, err := generate.Generate(p, generate.Options{}); err == nil || !strings.Contains(err.Error(), "ssr build script") {
		t.Fatalf("missing ssr script must error, got %v", err)
	}
}
