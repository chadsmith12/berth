package launch_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/launch"
)

func writeCompose(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "docker-compose.production.yml")
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadCompose(t *testing.T) {
	path := writeCompose(t, `
services:
    app:
        expose:
            - "8080"
    horizon:
        command: php artisan horizon
`)
	info, err := launch.ReadCompose(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if info.Port != 8080 || !info.Horizon {
		t.Fatalf("got %+v", info)
	}
}

func TestReadComposeIntPort(t *testing.T) {
	path := writeCompose(t, `
services:
    app:
        expose:
            - 3000
`)
	info, err := launch.ReadCompose(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if info.Port != 3000 || info.Horizon {
		t.Fatalf("got %+v", info)
	}
}

func TestReadComposeMissingApp(t *testing.T) {
	path := writeCompose(t, "services:\n    web:\n        expose:\n            - 80\n")
	if _, err := launch.ReadCompose(path); err == nil || !strings.Contains(err.Error(), "no app service") {
		t.Fatalf("got %v", err)
	}
}

func TestReadComposeNoPort(t *testing.T) {
	path := writeCompose(t, "services:\n    app:\n        environment:\n            FOO: bar\n")
	if _, err := launch.ReadCompose(path); err == nil || !strings.Contains(err.Error(), "exposes no port") {
		t.Fatalf("got %v", err)
	}
}

func TestReadComposeMissingFile(t *testing.T) {
	if _, err := launch.ReadCompose(filepath.Join(t.TempDir(), "nope.yml")); err == nil {
		t.Fatal("missing file must error")
	}
}
