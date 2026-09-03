package commands_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/commands"
	"github.com/chadsmith12/berth/pkg/config"
)

const testToken = "2|00Zljwz2i5p3MBupnMQc37olBmbCipThdVkCgBYma465f8c9"

func newFakeCoolify(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/teams/current" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Write([]byte(`{"id":0,"name":"Root Team","description":"The root team"}`))
	}))
}

func withIsolatedConfig(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "berth", "credentials.json")
}

func runCLI(args []string, stdin string) (int, string, string) {
	var out, errB bytes.Buffer
	code := commands.NewRootApp().Execute(args, strings.NewReader(stdin), &out, &errB)
	return code, out.String(), errB.String()
}

func TestAuthLoginStoresToken(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	code, out, _ := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, `stored token for team "Root Team" (id 0)`) {
		t.Fatalf("stdout %q", out)
	}

	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	tok, ok := config.FindToken(creds, srv.URL, 0)
	if !ok || tok != testToken {
		t.Fatalf("stored token %q %v", tok, ok)
	}
}

func TestAuthLoginRejectsBadTokenFormat(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, "justthesecret\n")
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errB, "invalid token format") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthLoginUnreachableIsAuthError(t *testing.T) {
	withIsolatedConfig(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()

	code, _, errB := runCLI([]string{"auth", "login", "--url", url}, testToken+"\n")
	if code != 3 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
}

func TestAuthLoginMissingURLNonInteractive(t *testing.T) {
	withIsolatedConfig(t)

	code, _, errB := runCLI([]string{"auth", "login"}, testToken+"\n")
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errB, "missing instance url") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthListPrintsStoredTokens(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, out, _ := runCLI([]string{"auth", "list"}, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	for _, want := range []string{srv.URL, "[0] Root Team", "2|..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, testToken) {
		t.Fatal("list must not print the full token")
	}
}

func TestAuthListJSONOmitsFullToken(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, out, _ := runCLI([]string{"auth", "list", "--json"}, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if strings.Contains(out, testToken) {
		t.Fatal("json output must not contain the full token")
	}
	var env struct {
		Data struct {
			Instances []struct {
				URL   string `json:"url"`
				Teams []struct {
					ID    int    `json:"id"`
					Name  string `json:"name"`
					Token string `json:"token"`
				} `json:"teams"`
			} `json:"instances"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("parse envelope: %v", err)
	}
	if len(env.Data.Instances) != 1 || len(env.Data.Instances[0].Teams) != 1 {
		t.Fatalf("got %q", out)
	}
}

func TestAuthListEmpty(t *testing.T) {
	withIsolatedConfig(t)

	code, out, _ := runCLI([]string{"auth", "list"}, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "no tokens stored") {
		t.Fatalf("stdout %q", out)
	}
}
