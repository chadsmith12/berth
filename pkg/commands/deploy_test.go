package commands_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeployFollowsToFinish(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}
	fc.depScript = [][]string{{"finished"}}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "✓ deployed") {
		t.Fatalf("stdout %q", out)
	}
}

func TestDeployJSONFinalEnvelope(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}
	fc.depScript = [][]string{{"finished"}}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1", "--json"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{`"status":"finished"`, `"deployment_uuid":"`, `"triggered":true`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestDeployJSONIsOneEnvelopeWithTail(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}
	logs := `[{"output":"composer install starting","hidden":false},` +
		`{"output":"Your requirements could not be resolved.","hidden":false}]`
	fc.depScript = [][]string{{"in_progress", logs}, {"failed", logs}}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1", "--json"}, "")
	if code != 1 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if strings.Contains(out, "● ") || strings.Contains(out, "\n  ") {
		t.Fatalf("json mode must not stream the event log to stdout: %q", out)
	}
	if strings.Count(out, "\n") > 1 {
		t.Fatalf("json mode must emit exactly one document: %q", out)
	}
	for _, want := range []string{`"success":false`, `"log_tail"`, `Your requirements could not be resolved.`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestDeployFailureExitsOne(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}
	fc.depScript = [][]string{{"in_progress"}, {"failed"}}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1"}, "")
	if code != 1 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(errB, "deployment failed") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestDeployDetach(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1", "--detach"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "follow it: berth deploy --deployment") {
		t.Fatalf("stdout %q", out)
	}
}

func TestDeployAttachExisting(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.setDeployment("d1", map[string]any{
		"deployment_uuid": "d1",
		"status":          "finished",
		"deployment_url":  "/project/p/environment/e/application/a/deployment/d1",
	})

	code, out, errB := runCLI([]string{"deploy", "--deployment", "d1"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "✓ deployed") || !strings.Contains(out, "logs: ") {
		t.Fatalf("stdout %q", out)
	}
	if strings.Contains(out, "queued") && !strings.Contains(out, "✓ deployed") {
		t.Fatalf("attach must not print queued only: %q", out)
	}
	_ = fc
}

func TestDeployFailureShowsVisibleTail(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["app-1"] = map[string]any{"uuid": "app-1", "name": "app"}
	logs := `[{"output":"composer install starting","hidden":false},` +
		`{"output":"docker run helper","hidden":true},` +
		`{"output":"Your requirements could not be resolved.","hidden":false}]`
	fc.depScript = [][]string{{"in_progress"}, {"failed", logs}}

	code, out, errB := runCLI([]string{"deploy", "--uuid", "app-1"}, "")
	if code != 1 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	for _, want := range []string{
		"✗ deployment failed",
		"Your requirements could not be resolved.",
		"full logs: ",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "docker run helper") {
		t.Fatalf("hidden debug entries must not appear: %q", out)
	}
}

func TestDeployMissingUUID(t *testing.T) {
	startLaunch(t)

	_, _, errB := runCLI([]string{"deploy"}, "")
	if !strings.Contains(errB, "no application linked") || !strings.Contains(errB, "BERTH_UUID") {
		t.Fatalf("stderr %q", errB)
	}
}

func TestDeployBERTHUUIDPipeline(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["pipe-1"] = map[string]any{"uuid": "pipe-1", "name": "app"}
	fc.depScript = [][]string{{"finished"}}
	t.Setenv("BERTH_UUID", "pipe-1")

	code, out, errB := runCLI([]string{"deploy"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "✓ deployed") {
		t.Fatalf("stdout %q", out)
	}
}

func TestDeployRegistryUUID(t *testing.T) {
	fc, _ := startLaunch(t)
	fc.apps["reg-1"] = map[string]any{"uuid": "reg-1", "name": "app"}
	fc.depScript = [][]string{{"finished"}}

	tmp := t.TempDir()
	t.Chdir(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, ".config"), 0755); err != nil {
		t.Fatal(err)
	}
	registry := `{"environments": [{"name": "production", "uuid": "reg-1"}]}`
	if err := os.WriteFile(filepath.Join(tmp, ".config", "berth.json"), []byte(registry), 0644); err != nil {
		t.Fatal(err)
	}

	code, out, errB := runCLI([]string{"deploy"}, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errB, out)
	}
	if !strings.Contains(out, "✓ deployed") {
		t.Fatalf("stdout %q", out)
	}
}
