package commands_test

import (
	"strings"
	"testing"
)

func TestLogsPrintsContainerLogs(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app", "status": "running:unknown"}
	fc.setAppLogs("app-1", "[entrypoint] database reachable\nINFO Server running\n")

	code, out, errB := runCLI([]string{"logs", "--uuid", "app-1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{"[entrypoint] database reachable", "INFO Server running"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestLogsJSONCarriesLogs(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app", "status": "running:unknown"}
	fc.setAppLogs("app-1", "line one\nline two\n")

	code, out, errB := runCLI([]string{"logs", "--uuid", "app-1", "--json"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{`"command":"logs"`, `"application_uuid":"app-1"`, `"logs":"line one\nline two\n"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestLogsEmptyShowsPlaceholder(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app", "status": "running:unknown"}
	fc.setAppLogs("app-1", "")

	code, out, errB := runCLI([]string{"logs", "--uuid", "app-1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "no logs yet") {
		t.Fatalf("stdout %q", out)
	}
}

func TestLogsStoppedAppSuggestsDeploy(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app", "status": "stopped"}

	code, _, errB := runCLI([]string{"logs", "--uuid", "app-1"}, "")
	if code != 1 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
	if !strings.Contains(errB, "application is stopped") || !strings.Contains(errB, "berth deploy") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestLogsMissingUUID(t *testing.T) {
	startLaunch(t)

	_, _, errB := runCLI([]string{"logs"}, "")
	if !strings.Contains(errB, "no application linked") || !strings.Contains(errB, "BERTH_UUID") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestLogsUnknownAppIsAuthError(t *testing.T) {
	startLaunch(t)

	code, _, errB := runCLI([]string{"logs", "--uuid", "missing-app"}, "")
	if code != 3 {
		t.Fatalf("exit %d, stderr %q", code, errB)
	}
}
