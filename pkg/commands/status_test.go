package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusShowsConfigAndLastDeployment(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{
		"uuid":                    "app-1",
		"name":                    "my-app",
		"status":                  "running:unknown",
		"git_repository":          "git@git.example.com:2222/me/app.git",
		"git_branch":              "master",
		"docker_compose_location": "/docker-compose.production.yml",
		"docker_compose_domains":  `{"app":{"domain":"http://app.example.com:8080"}}`,
	}
	fc.setAppDeployments("app-1", []map[string]any{
		{
			"deployment_uuid": "d2", "status": "finished",
			"commit":         "720737efc32fdbc98b8853421b04480ab156d728",
			"created_at":     "2026-09-07T02:24:21.000000Z",
			"finished_at":    "2026-09-07T02:25:01.000000Z",
			"deployment_url": "/project/p/environment/e/application/a/deployment/d2",
		},
		{"deployment_uuid": "d1", "status": "failed"},
	})

	code, out, errB := runCLI([]string{"status", "--uuid", "app-1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{
		"my-app (app-1)",
		"status     running",
		"branch     master",
		"repo       git@git.example.com:2222/me/app.git",
		"domain     http://app.example.com:8080",
		"compose    /docker-compose.production.yml",
		"last deployment  d2",
		"finished · 40s · commit 720737e",
		"started 2026-09-07 02:24 · finished 2026-09-07 02:25",
		"full logs: ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	// the newest record is the last deployment; the older one must not show
	if strings.Contains(out, "d1") {
		t.Fatalf("older deployment leaked into view: %q", out)
	}
}

func TestStatusJSONEnvelope(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{
		"uuid":                   "app-1",
		"name":                   "my-app",
		"status":                 "running:unknown",
		"git_branch":             "master",
		"docker_compose_domains": `{"app":{"domain":"http://app.example.com:8080"}}`,
	}
	fc.setAppDeployments("app-1", []map[string]any{
		{"deployment_uuid": "d2", "status": "finished", "commit": "720737e"},
	})

	code, out, errB := runCLI([]string{"status", "--uuid", "app-1", "--json"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{
		`"command":"status"`,
		`"application_uuid":"app-1"`,
		`"status":"running:unknown"`,
		`"git_branch":"master"`,
		`"domains":["http://app.example.com:8080"]`,
		`"last_deployment":{"deployment_uuid":"d2","status":"finished","commit":"720737e"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestStatusNoDeploymentsYet(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "my-app", "status": "running:unknown"}

	code, out, errB := runCLI([]string{"status", "--uuid", "app-1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "no deployments yet") {
		t.Fatalf("stdout %q", out)
	}
}

func TestStatusRegistryResolution(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["reg-1"] = map[string]any{"uuid": "reg-1", "name": "my-app", "status": "running:unknown"}
	fc.setAppDeployments("reg-1", []map[string]any{
		{"deployment_uuid": "d1", "status": "finished"},
	})

	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config"), 0755); err != nil {
		t.Fatal(err)
	}
	registry := `{"environments": [{"name": "production", "uuid": "reg-1"}]}`
	if err := os.WriteFile(filepath.Join(tmp, ".config", "berth.json"), []byte(registry), 0644); err != nil {
		t.Fatal(err)
	}

	code, out, errB := runCLI([]string{"status"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "my-app (reg-1)") {
		t.Fatalf("stdout %q", out)
	}
}

func TestStatusMissingUUID(t *testing.T) {
	startLaunch(t)

	_, _, errB := runCLI([]string{"status"}, "")
	if !strings.Contains(errB, "no application linked") || !strings.Contains(errB, "BERTH_UUID") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestStatusUnknownAppIsAuthError(t *testing.T) {
	startLaunch(t)

	code, _, errB := runCLI([]string{"status", "--uuid", "missing-app"}, "")
	if code != 3 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
}
