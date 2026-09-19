package commands_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/commands"
	"github.com/chadsmith12/berth/pkg/config"
)

const testToken = "2|00Zljwz2i5p3MBupnMQc37olBmbCipThdVkCgBYma465f8c9"

type fakeTeamServer struct {
	*httptest.Server
	authHeader string
}

func newFakeCoolify(t *testing.T) *fakeTeamServer {
	t.Helper()
	f := &fakeTeamServer{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/team" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		f.authHeader = r.Header.Get("Authorization")
		w.Write([]byte(`{"id":0,"name":"Root Team","description":"The root team"}`))
	}))
	return f
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

	code, _, _ := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d", code)
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

func TestAuthLoginSetsDefaultProfile(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatalf("load user config: %v", err)
	}
	if user.Default != "root-team" || len(user.Profiles) != 1 {
		t.Fatalf("got %+v", user)
	}
	p := user.Profiles[0]
	if p.Name != "root-team" || p.URL != srv.URL || p.Team == nil || p.Team.ID != 0 || p.Team.Name != "Root Team" {
		t.Fatalf("got %+v", p)
	}
	info, err := os.Stat(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0644 {
		t.Fatalf("perm %o, want 644", got)
	}
}

func TestAuthLoginAsNamesProfileWithoutChangingDefault(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("first login exit %d, stderr %q", code, errB)
	}

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--as", "second"}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if user.Default != "root-team" || len(user.Profiles) != 2 {
		t.Fatalf("got %+v", user)
	}
}

func TestAuthLoginDefaultFlagSwitchesDefault(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("first login exit %d, stderr %q", code, errB)
	}

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--as", "second", "--default"}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if user.Default != "second" || len(user.Profiles) != 2 {
		t.Fatalf("got %+v", user)
	}
}

func TestAuthLoginRejectsConflictingProfileName(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("first login exit %d, stderr %q", code, errB)
	}

	// "root-team" now exists for team 0 on this instance; a different team
	// with the same slug must be refused. The fake server always returns
	// team 0, so seed the conflict by hand: same name, other identity.
	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	user.UpsertProfile(config.Profile{Name: "other", URL: "https://elsewhere.example.com", Team: &config.TeamRef{ID: 5, Name: "Root Team"}})
	if err := config.SaveUserConfig(userPath, user); err != nil {
		t.Fatal(err)
	}

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--as", "other"}, testToken+"\n")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "different instance or team") || !strings.Contains(errB, "--as") {
		t.Fatalf("stderr %q", errB)
	}
	_ = credPath
}

func TestAuthDefaultShowsAndSets(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}
	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--as", "second"}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, out, errB := runCLI([]string{"auth", "default"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, "* root-team") || !strings.Contains(out, "  second") {
		t.Fatalf("stdout %q", out)
	}

	code, out, errB = runCLI([]string{"auth", "default", "second"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, `default profile is now "second"`) || !strings.Contains(out, "* second") {
		t.Fatalf("stdout %q", out)
	}

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		t.Fatal(err)
	}
	if user.Default != "second" {
		t.Fatalf("got %+v", user)
	}
}

func TestAuthDefaultUnknownProfile(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, _, errB := runCLI([]string{"auth", "default", "nope"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, `no profile named "nope"`) || !strings.Contains(errB, "root-team") {
		t.Fatalf("stderr %q", errB)
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

func TestAuthLoginTokenStdin(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	code, out, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--token-stdin"}, testToken+"\n")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if tok, ok := config.FindToken(creds, srv.URL, 0); !ok || tok != testToken {
		t.Fatalf("stored token %q %v", tok, ok)
	}
	if strings.Contains(out, "token?") {
		t.Fatal("stdin mode must not print the prompt")
	}
}

func TestAuthLoginTokenStdinEmpty(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL, "--token-stdin"}, "  \n")
	if code != 2 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(errB, "no token on stdin") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthWhoamiUsesStoredToken(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, out, errB := runCLI([]string{"auth", "whoami"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, `acting as team "Root Team" (id 0)`) || !strings.Contains(out, srv.URL) {
		t.Fatalf("stdout %q", out)
	}
}

func TestAuthWhoamiWithBERTHToken(t *testing.T) {
	withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()
	t.Setenv("BERTH_URL", srv.URL)
	t.Setenv("BERTH_TOKEN", testToken)

	code, out, errB := runCLI([]string{"auth", "whoami"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, "Root Team") {
		t.Fatalf("stdout %q", out)
	}
}

func TestAuthWhoamiWarnsOnTeamMismatch(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	userPath := filepath.Join(filepath.Dir(credPath), "config.json")
	userCfg := config.UserConfig{}
	userCfg.UpsertProfile(config.Profile{Name: "other", URL: srv.URL, Team: &config.TeamRef{ID: 5, Name: "Other Team"}})
	userCfg.Default = "other"
	if err := config.SaveUserConfig(userPath, userCfg); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_TOKEN", testToken)

	code, out, errB := runCLI([]string{"auth", "whoami"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	for _, want := range []string{"⚠", "Other Team", "Root Team"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestAuthWhoamiMissingTokenNonInteractive(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")

	code, _, errB := runCLI([]string{"auth", "whoami"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "no token stored") || !strings.Contains(errB, "BERTH_TOKEN") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthWhoamiAmbiguousTokens(t *testing.T) {
	credPath := withIsolatedConfig(t)
	creds := config.Credentials{}
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 1, Name: "Internal", Token: "1|aaa"})
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 3, Name: "Client Work", Token: "3|bbb"})
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_URL", "https://coolify.example.com")

	code, _, errB := runCLI([]string{"auth", "whoami"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "several tokens") || !strings.Contains(errB, "BERTH_TOKEN") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthLogoutRemovesDefaultTeamToken(t *testing.T) {
	credPath := withIsolatedConfig(t)
	srv := newFakeCoolify(t)
	defer srv.Close()

	if code, _, errB := runCLI([]string{"auth", "login", "--url", srv.URL}, testToken+"\n"); code != 0 {
		t.Fatalf("login exit %d, stderr %q", code, errB)
	}

	code, out, errB := runCLI([]string{"auth", "logout"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, "removed the token") || !strings.Contains(out, srv.URL) {
		t.Fatalf("stdout %q", out)
	}
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config.FindToken(creds, srv.URL, 0); ok {
		t.Fatal("token should be gone")
	}
}

func TestAuthLogoutNoTokens(t *testing.T) {
	withIsolatedConfig(t)
	t.Setenv("BERTH_URL", "https://coolify.example.com")

	code, out, _ := runCLI([]string{"auth", "logout"}, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "no tokens stored") {
		t.Fatalf("stdout %q", out)
	}
}

func TestAuthLogoutMultipleTeamsRequiresSelection(t *testing.T) {
	credPath := withIsolatedConfig(t)
	creds := config.Credentials{}
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 1, Name: "Internal", Token: "1|aaa"})
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 3, Name: "Client Work", Token: "3|bbb"})
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_URL", "https://coolify.example.com")

	code, _, errB := runCLI([]string{"auth", "logout"}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "several tokens") || !strings.Contains(errB, "--team") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestAuthLogoutTeamFlagSelects(t *testing.T) {
	credPath := withIsolatedConfig(t)
	creds := config.Credentials{}
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 1, Name: "Internal", Token: "1|aaa"})
	config.UpsertToken(&creds, "https://coolify.example.com", config.TeamToken{ID: 3, Name: "Client Work", Token: "3|bbb"})
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BERTH_URL", "https://coolify.example.com")

	code, out, errB := runCLI([]string{"auth", "logout", "--team", "3"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(out, `team "Client Work" (id 3)`) {
		t.Fatalf("stdout %q", out)
	}
	after, err := config.LoadCredentials(credPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := config.FindToken(after, "https://coolify.example.com", 3); ok {
		t.Fatal("team 3 token should be gone")
	}
	if tok, ok := config.FindToken(after, "https://coolify.example.com", 1); !ok || tok != "1|aaa" {
		t.Fatalf("team 1 token should remain, got %q %v", tok, ok)
	}
}

func TestAuthListEmpty(t *testing.T) {
	withIsolatedConfig(t)

	code, out, _ := runCLI([]string{"auth", "list"}, "")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(out, "no profiles or tokens stored") {
		t.Fatalf("stdout %q", out)
	}
}
