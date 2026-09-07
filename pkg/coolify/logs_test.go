package coolify_test

import (
	"encoding/json"
	"testing"

	"github.com/chadsmith12/berth/pkg/coolify"
)

const logsBlob = `[{"command":null,"output":"Docker 29.7.2 with BuildKit detected.","type":"stdout","timestamp":"2026-09-07T00:08:29.303856Z","hidden":false,"batch":1},
{"command":"docker stop --timeout=30 xyz","output":"Error response from daemon: No such container: xyz","type":"stderr","timestamp":"2026-09-07T00:08:29.785546Z","hidden":true,"batch":1,"order":4},
{"command":null,"output":"Preparing container with helper image: coolify-helper:1.0.16","type":"stdout","timestamp":"2026-09-07T00:08:29.690443Z","hidden":false,"batch":1,"order":3}]`

func TestLogEntriesParsesBlob(t *testing.T) {
	d := coolify.Deployment{Logs: logsBlob}
	entries := d.LogEntries()
	if len(entries) != 3 {
		t.Fatalf("entries %+v", entries)
	}
	if entries[1].Hidden != true || entries[1].Command == nil {
		t.Fatalf("entry 1 %+v", entries[1])
	}
}

func TestVisibleLogsFiltersHidden(t *testing.T) {
	d := coolify.Deployment{Logs: logsBlob}
	lines := d.VisibleLogs()
	if len(lines) != 2 {
		t.Fatalf("hidden entries must be filtered, got %v", lines)
	}
	for _, line := range lines {
		if line == "Error response from daemon: No such container: xyz" {
			t.Fatalf("debug line leaked: %v", lines)
		}
	}
}

func TestLogEntriesEmptyAndMalformed(t *testing.T) {
	if got := (coolify.Deployment{}).LogEntries(); got != nil {
		t.Fatalf("empty blob must be nil, got %v", got)
	}
	if got := (coolify.Deployment{Logs: "not json"}).LogEntries(); got != nil {
		t.Fatalf("malformed blob must be nil, got %v", got)
	}
	if got := (coolify.Deployment{Logs: "[]"}).VisibleLogs(); got != nil {
		t.Fatalf("empty array must be nil, got %v", got)
	}
}

func TestLogsFieldIsStringEncodedArray(t *testing.T) {
	// the API returns logs as a JSON string containing the array
	raw := `{"deployment_uuid":"d1","status":"failed","logs":` + escapeJSON(logsBlob) + `}`
	var d coolify.Deployment
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.VisibleLogs()) != 2 {
		t.Fatalf("visible %v", d.VisibleLogs())
	}
}

func escapeJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
