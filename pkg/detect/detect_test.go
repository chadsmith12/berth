package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeProject(t *testing.T, composer, packageJson string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(composer), 0644); err != nil {
		t.Fatal(err)
	}
	if packageJson != "" {
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(packageJson), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const baseComposer = `{"require": {"laravel/framework": "^11.0", "php": "^8.2"}}`

func TestScanDetectsHorizon(t *testing.T) {
	dir := writeProject(t, `{"require": {"laravel/framework": "^11.0", "php": "^8.2", "laravel/horizon": "^5.0"}}`, "")

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !p.Project.HasHorizon {
		t.Fatal("horizon must be detected")
	}
}

func TestScanWithoutHorizon(t *testing.T) {
	dir := writeProject(t, baseComposer, "")

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.HasHorizon {
		t.Fatal("horizon must not be detected")
	}
}

func TestScanRecordsSsrScriptName(t *testing.T) {
	dir := writeProject(t, baseComposer, `{
		"scripts": {"build": "vite build", "build:ssr": "vite build --ssr"},
		"devDependencies": {"@laravel/vite-plugin-wayfinder": "^1.0"}
	}`)

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if !p.Project.HasSsr || p.Project.SsrScript != "build:ssr" {
		t.Fatalf("ssr script: %+v", p.Project)
	}
}

func TestScanSsrWithoutScriptNameFailsCheck(t *testing.T) {
	dir := writeProject(t, baseComposer, "")
	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	p.Project.HasSsr = true
	p.Project.SsrScript = ""

	report := p.Check()
	if !report.HasBlockingFailure() {
		t.Fatal("ssr without a build script must be a blocking failure")
	}
}

func TestScanRaisesPhpFromComposerLock(t *testing.T) {
	dir := writeProject(t, baseComposer, "")
	// the real-world case: a lock resolved on PHP 8.5 locked symfony
	// packages that require >= 8.4.1, while composer.json says ^8.3
	lock := `{
		"platform": {"php": "^8.3"},
		"packages": [
			{"name": "laravel/framework", "require": {"php": "^8.3"}},
			{"name": "symfony/http-foundation", "require": {"php": ">=8.4.1"}},
			{"name": "dasprid/enum", "require": {"php": ">=7.1 <9.0"}},
			{"name": "dragonmantank/cron-expression", "require": {"php": "^8.2|^8.3|^8.4|^8.5"}}
		],
		"packages-dev": [
			{"name": "phpunit/phpunit", "require": {"php": ">=99.0"}}
		]
	}`
	if err := os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(lock), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.PhpVersion != "8.4" {
		t.Fatalf("php %q, want 8.4 (lock max requirement)", p.Project.PhpVersion)
	}
}

func TestScanIgnoresUpperBoundPhpConstraint(t *testing.T) {
	dir := writeProject(t, `{"require": {"laravel/framework": "^11.0", "php": "<8.5"}}`, "")

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.PhpVersion == "8.5" {
		t.Fatalf("an upper bound must not become the floor, got %q", p.Project.PhpVersion)
	}
	if p.Project.PhpVersion != defaultPhpVersion {
		t.Fatalf("php %q, want default %s", p.Project.PhpVersion, defaultPhpVersion)
	}
}

func TestScanKeepsJsonVersionWhenHigher(t *testing.T) {
	dir := writeProject(t, `{"require": {"laravel/framework": "^11.0", "php": "^8.5"}}`, "")
	lock := `{
		"platform": {"php": "^8.3"},
		"packages": [{"name": "laravel/framework", "require": {"php": "^8.2"}}]
	}`
	if err := os.WriteFile(filepath.Join(dir, "composer.lock"), []byte(lock), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.PhpVersion != "8.5" {
		t.Fatalf("php %q, want 8.5", p.Project.PhpVersion)
	}
}

func TestMinSatisfyingPhp(t *testing.T) {
	cases := map[string]string{
		"^8.3":                "8.3",
		">=8.4.1":             "8.4",
		">=7.1 <9.0":          "7.1",
		"^8.2|^8.3|^8.4|^8.5": "8.2",
		"~8.2.0":              "8.2",
		"*":                   "",
		"":                    "",
		">=8.1, <9.0":         "8.1",
	}
	for in, want := range cases {
		if got := minSatisfyingPhp(in); got != want {
			t.Errorf("minSatisfyingPhp(%q) = %q, want %q", in, got, want)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestScanMonorepoDescendsIntoSingleApp(t *testing.T) {
	root := repoRoot(t)
	app := filepath.Join(root, "web")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "composer.json"), []byte(baseComposer), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.Name != "web" {
		t.Fatalf("name %q, want the app subdir name", p.Project.Name)
	}
	if p.Project.BaseDir != "web" {
		t.Fatalf("base dir %q, want web", p.Project.BaseDir)
	}
}

func TestScanMultipleAppsIsAnError(t *testing.T) {
	root := repoRoot(t)
	for _, dir := range []string{"web", "api"} {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "composer.json"), []byte(baseComposer), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := Scan(root); err == nil {
		t.Fatal("multiple laravel apps under the repo root must be an error")
	} else if !strings.Contains(err.Error(), "web") || !strings.Contains(err.Error(), "api") {
		t.Fatalf("error should name the app dirs, got: %v", err)
	}
}

func TestScanAppAtRepoRootHasEmptyBaseDir(t *testing.T) {
	root := repoRoot(t)
	if err := os.WriteFile(filepath.Join(root, "composer.json"), []byte(baseComposer), 0644); err != nil {
		t.Fatal(err)
	}

	p, err := Scan(root)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.BaseDir != "" {
		t.Fatalf("base dir %q, want empty (app at repo root)", p.Project.BaseDir)
	}
}

func TestScanNoGitKeepsEmptyBaseDir(t *testing.T) {
	dir := writeProject(t, baseComposer, "")
	p, err := Scan(dir)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if p.Project.BaseDir != "" {
		t.Fatalf("base dir %q, want empty when no git repo", p.Project.BaseDir)
	}
}

func TestRepoRootWalksUp(t *testing.T) {
	root := repoRoot(t)
	app := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	got, ok := RepoRoot(app)
	if !ok || got != root {
		t.Fatalf("RepoRoot(%q) = %q, %v; want %q, true", app, got, ok, root)
	}
	if _, ok := RepoRoot(t.TempDir()); ok {
		t.Fatal("RepoRoot outside a git repo must return false")
	}
}
