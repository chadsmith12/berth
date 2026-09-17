package commands_test

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite test-apps golden files from current output")

// generatedFiles are the artifacts generate produces, relative to the project root.
var generatedFiles = []string{
	"docker/app/Dockerfile",
	"docker/app/entrypoint.sh",
	".dockerignore",
	"docker-compose.production.yml",
}

// variantArgs are the non-interactive decisions per fixture variant. Variants
// not listed get defaultArgs; horizon variants omit --workers on purpose: the
// command must not ask for it.
var variantArgs = map[string][]string{
	"php-only":          {"--workers", "2", "--scheduler=false", "--port", "8080"},
	"npm-vite":          {"--workers", "2", "--scheduler=false", "--port", "8080"},
	"bun-ssr-wayfinder": {"--workers", "2", "--scheduler=false", "--port", "8080"},
	"horizon":           {"--scheduler=false", "--port", "8080"},
	"horizon-ssr":       {"--scheduler=false", "--port", "8080"},
}

func testAppsRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "test-apps"))
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		t.Fatalf("test-apps not found at %s", root)
	}
	return root
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if d.Name() == "golden" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0755)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(filepath.Join(dst, rel))
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGenerateE2E(t *testing.T) {
	root := testAppsRoot(t)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "real" {
			continue
		}
		variant := e.Name()
		t.Run(variant, func(t *testing.T) {
			src := filepath.Join(root, variant)
			work := t.TempDir()
			copyDir(t, src, work)

			args := append([]string{"generate", "--path", work}, variantArgs[variant]...)
			code, out, errB := runCLI(args, "")
			if code != 0 {
				t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
			}

			for _, rel := range generatedFiles {
				generated, err := os.ReadFile(filepath.Join(work, rel))
				if err != nil {
					t.Fatalf("generate did not produce %s: %v", rel, err)
				}
				goldenPath := filepath.Join(src, "golden", rel)
				if *updateGolden {
					if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(goldenPath, generated, 0644); err != nil {
						t.Fatal(err)
					}
					continue
				}
				expected, err := os.ReadFile(goldenPath)
				if err != nil {
					t.Fatalf("golden %s missing — run: go test ./pkg/commands -run TestGenerateE2E -update", rel)
				}
				if string(generated) != string(expected) {
					t.Errorf("%s differs from golden:\n%s", rel, diffPreview(string(expected), string(generated)))
				}
			}
		})
	}
}

func TestGenerateE2EMonorepo(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	app := filepath.Join(root, "web")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "composer.json"), []byte(`{"require": {"laravel/framework": "^11.0", "php": "^8.3"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "package.json"), []byte(`{"engines": {"node": "20"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	code, out, errB := runCLI([]string{"generate", "--path", root, "--workers", "2", "--scheduler=false", "--port", "8080"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "/web") {
		t.Fatalf("output should surface the base dir /web, got:\n%s", out)
	}

	for _, rel := range generatedFiles {
		if _, err := os.Stat(filepath.Join(app, rel)); err != nil {
			t.Fatalf("generate should write %s into the app subdir: %v", rel, err)
		}
	}

	compose, err := os.ReadFile(filepath.Join(app, "docker-compose.production.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(compose), "context: .") {
		t.Errorf("compose build context must stay app-relative:\n%s", compose)
	}
}

func TestGenerateE2EMonorepoMultipleAppsFails(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"web", "api"} {
		d := filepath.Join(root, dir)
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "composer.json"), []byte(`{"require": {"laravel/framework": "^11.0", "php": "^8.3"}}`), 0644); err != nil {
			t.Fatal(err)
		}
	}

	code, _, errB := runCLI([]string{"generate", "--path", root, "--workers", "2", "--scheduler=false", "--port", "8080"}, "")
	if code == 0 {
		t.Fatal("generate against a repo root with multiple apps must fail")
	}
	if !strings.Contains(errB, "web") || !strings.Contains(errB, "api") {
		t.Fatalf("error should name the app dirs, got: %q", errB)
	}
}

func diffPreview(want, got string) string {
	wantLines, gotLines := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < len(wantLines) || i < len(gotLines); i++ {
		var w, g string
		if i < len(wantLines) {
			w = wantLines[i]
		}
		if i < len(gotLines) {
			g = gotLines[i]
		}
		if w != g {
			b.WriteString("  want: " + w + "\n  got:  " + g + "\n")
		}
	}
	if b.Len() > 4000 {
		return b.String()[:4000] + "\n  …truncated"
	}
	return b.String()
}
