package commands_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/chadsmith12/berth/pkg/config"
)

// fakeCoolify is a stateful in-memory Coolify for launch e2e tests.
type fakeCoolify struct {
	mu             sync.Mutex
	projects       map[string]fakeProject
	servers        []map[string]any
	keys           map[string]map[string]any
	databases      map[string]map[string]any
	apps           map[string]map[string]any
	envs           map[string][]map[string]any
	deployments    map[string]map[string]any
	appDeployments map[string][]map[string]any
	appLogs        map[string]string
	deployCalls    int
	depScript      [][]string // per GET /deployments/{uuid}: [status, ...] advancing
	seq            int
}

type fakeProject struct {
	uuid         string
	name         string
	environments []map[string]any
}

func (f *fakeCoolify) next() string {
	f.seq++
	const alphabet = "abcdefghijklmnopqrstuvwxyz012345"
	u := make([]byte, 24)
	for i := range u {
		u[i] = alphabet[(f.seq*7+i*3)%len(alphabet)]
	}
	return string(u)
}

func (f *fakeCoolify) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.route(t, w, r)
	}
}

func (f *fakeCoolify) route(t *testing.T, w http.ResponseWriter, r *http.Request) {
	t.Helper()
	write := func(status int, v any) {
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}
	segs := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	// segs[0] == "api", segs[1] == "v1"
	rest := segs[2:]

	switch {
	case len(rest) == 1 && rest[0] == "team":
		write(200, map[string]any{"id": 0, "name": "Root Team"})
	case rest[0] == "projects" && len(rest) == 1:
		switch r.Method {
		case http.MethodGet:
			out := []map[string]any{}
			for _, p := range f.projects {
				out = append(out, map[string]any{"uuid": p.uuid, "name": p.name, "description": nil})
			}
			write(200, out)
		case http.MethodPost:
			var body struct {
				Name string `json:"name"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			u := f.next()
			f.projects[u] = fakeProject{uuid: u, name: body.Name}
			write(201, map[string]any{"uuid": u})
		}
	case rest[0] == "projects" && len(rest) == 2 && r.Method == http.MethodGet:
		p, ok := f.projects[rest[1]]
		if !ok {
			write(404, map[string]any{"message": "not found"})
			return
		}
		write(200, map[string]any{"uuid": p.uuid, "name": p.name, "environments": p.environments})
	case rest[0] == "projects" && len(rest) == 3 && rest[2] == "environments" && r.Method == http.MethodPost:
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		u := f.next()
		p := f.projects[rest[1]]
		p.environments = append(p.environments, map[string]any{"uuid": u, "name": body.Name})
		f.projects[rest[1]] = p
		write(201, map[string]any{"uuid": u})
	case rest[0] == "servers" && len(rest) == 1:
		write(200, f.servers)
	case rest[0] == "security" && rest[1] == "keys" && len(rest) == 2:
		switch r.Method {
		case http.MethodGet:
			out := []map[string]any{}
			for _, k := range f.keys {
				out = append(out, k)
			}
			write(200, out)
		case http.MethodPost:
			var body struct {
				Name       string `json:"name"`
				PrivateKey string `json:"private_key"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if !strings.Contains(body.PrivateKey, "OPENSSH PRIVATE KEY") {
				t.Errorf("private key must be an OpenSSH PEM, got %.40q", body.PrivateKey)
			}
			u := f.next()
			f.keys[u] = map[string]any{"uuid": u, "name": body.Name}
			write(201, map[string]any{"uuid": u})
		}
	case rest[0] == "databases" && len(rest) == 1:
		out := []map[string]any{}
		for uuid, d := range f.databases {
			out = append(out, map[string]any{"uuid": uuid, "name": d["name"], "database_type": d["database_type"]})
		}
		write(200, out)
	case rest[0] == "databases" && len(rest) == 2:
		d, ok := f.databases[rest[1]]
		if !ok {
			write(404, map[string]any{"message": "not found"})
			return
		}
		write(200, d)
	case rest[0] == "applications" && rest[1] == "private-deploy-key" && r.Method == http.MethodPost:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		u := f.next()
		body["uuid"] = u
		f.apps[u] = body
		f.envs[u] = []map[string]any{}
		write(201, map[string]any{"uuid": u, "domains": nil})
	case rest[0] == "deploy" && len(rest) == 1 && r.Method == http.MethodPost:
		u := f.next()
		f.deployments[u] = map[string]any{
			"deployment_uuid": u,
			"status":          "queued",
			"deployment_url":  "/project/p/environment/e/application/a/deployment/" + u,
		}
		write(200, map[string]any{"deployments": []map[string]any{{
			"deployment_uuid": u,
			"resource_uuid":   r.URL.Query().Get("uuid"),
			"message":         "queued.",
		}}})
	case rest[0] == "deployments" && len(rest) == 2 && r.Method == http.MethodGet:
		d, ok := f.deployments[rest[1]]
		if !ok {
			write(404, map[string]any{"message": "not found"})
			return
		}
		// advance the scripted state per poll, if one is set
		if f.depScript != nil && len(f.depScript) > 0 {
			idx := f.deployCalls
			if idx >= len(f.depScript) {
				idx = len(f.depScript) - 1
			}
			step := f.depScript[idx]
			d["status"] = step[0]
			if len(step) > 1 {
				d["logs"] = step[1]
			}
			f.deployCalls++
		}
		write(200, d)
	case rest[0] == "deployments" && len(rest) == 3 && rest[1] == "applications" && r.Method == http.MethodGet:
		deps := f.appDeployments[rest[2]]
		if deps == nil {
			deps = []map[string]any{}
		}
		write(200, map[string]any{"count": len(deps), "deployments": deps})
	case rest[0] == "applications" && len(rest) == 2 && r.Method == http.MethodGet:
		a, ok := f.apps[rest[1]]
		if !ok {
			write(404, map[string]any{"message": "not found"})
			return
		}
		write(200, a)
	case rest[0] == "applications" && len(rest) == 3 && rest[2] == "logs" && r.Method == http.MethodGet:
		if _, ok := f.apps[rest[1]]; !ok {
			write(404, map[string]any{"message": "not found"})
			return
		}
		if logs, ok := f.appLogs[rest[1]]; ok {
			write(200, map[string]any{"logs": logs})
			return
		}
		// the real instance refuses while the application is not running
		write(400, map[string]any{"message": "application is not running"})
	case rest[0] == "applications" && len(rest) == 3 && rest[2] == "envs":
		appEnvs := f.envs[rest[1]]
		switch r.Method {
		case http.MethodGet:
			write(200, appEnvs)
		case http.MethodPost:
			var body struct {
				Key   string `json:"key"`
				Value string `json:"value"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.envs[rest[1]] = append(appEnvs, map[string]any{"uuid": f.next(), "key": body.Key})
			write(201, map[string]any{"uuid": f.next()})
		}
	default:
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		write(404, map[string]any{"message": "not found"})
	}
}

func seedLaunchIdentity(t *testing.T, srvURL string) {
	t.Helper()
	withIsolatedConfig(t)
	userPath := filepath.Join(userConfigDir(t), "config.json")
	userCfg := config.UserConfig{}
	userCfg.UpsertProfile(config.Profile{Name: "test", URL: srvURL, Team: &config.TeamRef{ID: 0, Name: "Root Team"}})
	userCfg.Default = "test"
	if err := config.SaveUserConfig(userPath, userCfg); err != nil {
		t.Fatal(err)
	}
	creds := config.Credentials{}
	config.UpsertToken(&creds, srvURL, config.TeamToken{ID: 0, Name: "Root Team", Token: testToken})
	credPath := filepath.Join(userConfigDir(t), "credentials.json")
	if err := config.SaveCredentials(credPath, creds); err != nil {
		t.Fatal(err)
	}
}

func userConfigDir(t *testing.T) string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		t.Fatal("XDG_CONFIG_HOME not isolated")
	}
	return filepath.Join(dir, "berth")
}

func launchProject(t *testing.T, compose string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.production.yml"), []byte(compose), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	remote := exec.Command("git", "-C", dir, "remote", "add", "origin", "ssh://git@git.example.com:2222/me/app.git")
	if out, err := remote.CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}
	return dir
}

const launchCompose = `services:
    app:
        environment:
            SERVER_NAME: ":8080"
        expose:
            - "8080"
    scheduler:
        command: php artisan schedule:work
`

func newFakeLaunchCoolify(t *testing.T) (*fakeCoolify, *httptest.Server) {
	t.Helper()
	fc := &fakeCoolify{
		projects: map[string]fakeProject{},
		servers:  []map[string]any{{"uuid": "srv-1", "name": "localhost", "ip": "10.0.0.1", "is_usable": true}},
		keys:     map[string]map[string]any{"key-1": {"uuid": "key-1", "name": "existing"}},
		databases: map[string]map[string]any{
			"db-1":    {"uuid": "db-1", "name": "pmc-postgres", "database_type": "standalone-postgresql", "postgres_user": "postgres", "postgres_password": "secret", "postgres_db": "app"},
			"redis-1": {"uuid": "redis-1", "name": "pmc-redis", "database_type": "standalone-redis", "redis_password": "redispass"},
		},
		apps:           map[string]map[string]any{},
		envs:           map[string][]map[string]any{},
		deployments:    map[string]map[string]any{},
		appDeployments: map[string][]map[string]any{},
		appLogs:        map[string]string{},
		depScript:      nil,
	}
	srv := httptest.NewServer(fc.handler(t))
	t.Cleanup(srv.Close)
	return fc, srv
}

func appNameOf(t *testing.T, cwd string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(cwd, ".config", "berth.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Environments []struct {
			UUID string `json:"uuid"`
		} `json:"environments"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) != 1 {
		t.Fatalf("registry %s", body)
	}
	return cfg.Environments[0].UUID
}

func startLaunch(t *testing.T) (*fakeCoolify, string) {
	t.Helper()
	// No t.Chdir: the repository config must land in the --path project
	// directory, never in the working directory.
	fc, srv := newFakeLaunchCoolify(t)
	seedLaunchIdentity(t, srv.URL)
	return fc, "."
}

func TestLaunchHappyPath(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose)

	code, out, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "[created] pmc") || !strings.Contains(out, "[created] pmc-production") {
		t.Fatalf("stdout %q", out)
	}
	if !strings.Contains(out, "next: berth deploy") {
		t.Fatalf("stdout %q", out)
	}
	// the fixture's files are uncommitted and the branch has no upstream
	if !strings.Contains(out, "commit and push") {
		t.Fatalf("checklist missing push reminder: %q", out)
	}

	// the registry must land in the project directory, not the working dir
	body, err := os.ReadFile(filepath.Join(dir, ".config", "berth.json"))
	if err != nil {
		t.Fatalf("repo config in project dir: %v", err)
	}
	var cfg struct {
		Environments []struct {
			Name string `json:"name"`
			UUID string `json:"uuid"`
		} `json:"environments"`
	}
	if err := json.Unmarshal(body, &cfg); err != nil {
		t.Fatal(err)
	}
	if len(cfg.Environments) != 1 || cfg.Environments[0].Name != "production" || cfg.Environments[0].UUID == "" {
		t.Fatalf("registry %s", body)
	}

	// re-running launch on a linked environment is refused: the application
	// exists but cannot be searched by name — use deploy, or --force to relink
	code, out2, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}, "")
	if code != 2 {
		t.Fatalf("second exit %d, stderr %q, stdout %q", code, errB, out2)
	}
	if !strings.Contains(errB, "already linked") {
		t.Fatalf("stderr %q", errB)
	}
	// nothing was duplicated on the fake: still exactly one application
	if n := len(fc.apps); n != 1 {
		t.Fatalf("expected exactly one application creation, got %d", n)
	}
}

func (f *fakeCoolify) app(uuid string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.apps[uuid]
}

func (f *fakeCoolify) deployment(uuid string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.deployments[uuid]
}

func (f *fakeCoolify) setDeployment(uuid string, d map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deployments[uuid] = d
}

func (f *fakeCoolify) setAppDeployments(uuid string, deps []map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appDeployments[uuid] = deps
}

func (f *fakeCoolify) setAppLogs(uuid, logs string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appLogs[uuid] = logs
}

func TestLaunchRecordsGitRepoNormalized(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose)

	code, _, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	app := fc.app(appNameOf(t, dir))
	if app == nil {
		t.Fatal("no application created on the fake")
	}
	if got := app["git_repository"]; got != "git@git.example.com:2222/me/app.git" {
		t.Fatalf("git_repository %v", got)
	}
	if got := app["docker_compose_location"]; got != "/docker-compose.production.yml" {
		t.Fatalf("docker_compose_location %v", got)
	}
	if got := app["connect_to_docker_network"]; got != true {
		t.Fatalf("connect_to_docker_network %v", got)
	}
	if got := app["autogenerate_domain"]; got != false {
		t.Fatalf("autogenerate_domain %v", got)
	}
	domains := app["docker_compose_domains"].(map[string]any)
	d := domains["app"].(map[string]any)
	if d["domain"] != "http://app.example.com:8080" {
		t.Fatalf("domain %v", d["domain"])
	}
}

func TestLaunchCustomEnvNonInteractive(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose)
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.staging.yml"), []byte(launchCompose), 0644); err != nil {
		t.Fatal(err)
	}

	code, out, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--env", "staging",
		"--branch", "main", "--domain", "http://app.example.com",
		"--database", "none", "--key", "key-1",
	}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "[created] pmc-staging") {
		t.Fatalf("stdout %q", out)
	}
	app := fc.app(appNameOf(t, dir))
	if app == nil {
		t.Fatal("no application created on the fake")
	}
	if got := app["environment_name"]; got != "staging" {
		t.Fatalf("environment_name %v", got)
	}
	if got := app["docker_compose_location"]; got != "/docker-compose.staging.yml" {
		t.Fatalf("docker_compose_location %v", got)
	}
	body, err := os.ReadFile(filepath.Join(dir, ".config", "berth.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"staging"`) {
		t.Fatalf("registry %s", body)
	}
}

func TestLaunchRefusesRelinkWithoutForce(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose)
	fc.apps["other-app"] = map[string]any{"uuid": "other-app", "name": "other"}

	args := []string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}
	if code, _, errB := runCLI(args, ""); code != 0 {
		t.Fatalf("first exit %d, stderr %q", code, errB)
	}
	_, _, errB := runCLI(append(args, "--uuid", "other-app"), "")
	if !strings.Contains(errB, "already linked") {
		t.Fatalf("stderr %q", errB)
	}
	code, _, errB := runCLI(append(args, "--uuid", "other-app", "--force"), "")
	if code != 0 {
		t.Fatalf("forced adopt exit %d, stderr %q", code, errB)
	}
	uuid := appNameOf(t, dir)
	if uuid != "other-app" {
		t.Fatalf("uuid %q", uuid)
	}
}

func TestLaunchAdoptsLinkedUUIDIdempotently(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose)

	args := []string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}
	if code, _, errB := runCLI(args, ""); code != 0 {
		t.Fatalf("first exit %d, stderr %q", code, errB)
	}
	linked := appNameOf(t, dir)
	// GET /applications returns the compose-domain blob as an encoded string;
	// the create body is a map, so give the fake the read shape it needs.
	fc.apps[linked] = map[string]any{"uuid": linked, "name": "app"}

	code, _, errB := runCLI(append(args, "--uuid", linked), "")
	if code != 0 {
		t.Fatalf("re-adopting the linked uuid must succeed, exit %d stderr %q", code, errB)
	}
	if got := appNameOf(t, dir); got != linked {
		t.Fatalf("uuid changed: %q -> %q", linked, got)
	}
}

func TestLaunchMissingCompose(t *testing.T) {
	startLaunch(t)
	dir := t.TempDir()

	_, _, errB := runCLI([]string{"launch", "--path", dir, "--project", "pmc"}, "")
	if !strings.Contains(errB, "berth generate") {
		t.Fatalf("stderr %q", errB)
	}
}

// An environment chosen that has no compose file is refused before anything is
// created in Coolify; the error names the generate step to run.
func TestLaunchEnvWithoutComposeFlag(t *testing.T) {
	fc, _ := startLaunch(t)
	dir := launchProject(t, launchCompose) // only docker-compose.production.yml exists

	code, _, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--env", "staging",
		"--branch", "main", "--domain", "http://app.example.com",
		"--database", "none", "--key", "key-1",
	}, "")
	if code != 2 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "docker-compose.staging.yml") || !strings.Contains(errB, "berth generate --env staging") {
		t.Fatalf("stderr %q", errB)
	}
	if n := len(fc.apps); n != 0 {
		t.Fatalf("expected no application created, got %d", n)
	}
}

func TestLaunchHorizonRequiresRedis(t *testing.T) {
	startLaunch(t)
	dir := launchProject(t, launchCompose+"\n    horizon:\n        command: php artisan horizon\n")

	_, _, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none", "--key", "key-1",
	}, "")
	if !strings.Contains(errB, "--redis") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestLaunchMissingProjectNonInteractive(t *testing.T) {
	startLaunch(t)
	dir := launchProject(t, launchCompose)

	_, _, errB := runCLI([]string{
		"launch", "--path", dir, "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none",
	}, "")
	if !strings.Contains(errB, "missing --project") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestLaunchMissingKeyNonInteractive(t *testing.T) {
	startLaunch(t)
	dir := launchProject(t, launchCompose)

	_, _, errB := runCLI([]string{
		"launch", "--path", dir, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none",
	}, "")
	if !strings.Contains(errB, "missing --key") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestLaunchMonorepoFindsComposeInSubdir(t *testing.T) {
	startLaunch(t)
	root := t.TempDir()
	app := filepath.Join(root, "web")
	if err := os.MkdirAll(app, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app, "docker-compose.production.yml"), []byte(launchCompose), 0644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	remote := exec.Command("git", "-C", root, "remote", "add", "origin", "ssh://git@git.example.com:2222/me/app.git")
	if out, err := remote.CombinedOutput(); err != nil {
		t.Fatalf("git remote: %v: %s", err, out)
	}

	// launch --path <repo root> must descend into web/ to find the compose.
	// Reaching "missing --key" (a decision made after the compose read at
	// docker-compose.production.yml) proves the file was located and parsed in
	// the app subdir.
	_, _, errB := runCLI([]string{
		"launch", "--path", root, "--project", "pmc", "--branch", "main",
		"--domain", "http://app.example.com", "--database", "none",
	}, "")
	if !strings.Contains(errB, "missing --key") {
		t.Fatalf("stderr %q, want the launch decisions to proceed past the compose read in the app subdir", errB)
	}
}
