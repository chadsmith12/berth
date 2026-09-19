package deploy_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/deploy"
)

type scriptClient struct {
	states []coolify.Deployment
	calls  int
	errAt  int // -1 = never error
}

func (s *scriptClient) Deployment(ctx context.Context, uuid string) (coolify.Deployment, error) {
	if s.calls == s.errAt {
		return coolify.Deployment{}, errors.New("boom")
	}
	if s.calls >= len(s.states) {
		return s.states[len(s.states)-1], nil
	}
	d := s.states[s.calls]
	s.calls++
	return d, nil
}

func collect(t *testing.T, events <-chan deploy.Event) []deploy.Event {
	t.Helper()
	var out []deploy.Event
	for e := range events {
		out = append(out, e)
	}
	return out
}

func TestFollowFinishes(t *testing.T) {
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{
		{Status: "queued"},
		{Status: "in_progress"},
		{Status: "finished"},
	}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	got := collect(t, events)
	if len(got) == 0 || got[len(got)-1].Kind != deploy.KindDone {
		t.Fatalf("last event must be done, got %+v", got)
	}
	if got[0].Kind != deploy.KindQueued {
		t.Fatalf("first event must be queued, got %+v", got[0])
	}
}

func TestFollowFails(t *testing.T) {
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{
		{Status: "in_progress"},
		{Status: "failed"},
	}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	got := collect(t, events)
	if got[len(got)-1].Kind != deploy.KindFailed {
		t.Fatalf("last event must be failed, got %+v", got)
	}
}

func TestFollowCancelledIsFailure(t *testing.T) {
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{
		{Status: "in_progress"},
		{Status: "cancelled_by_user"},
	}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	got := collect(t, events)
	if got[len(got)-1].Kind != deploy.KindFailed {
		t.Fatalf("cancelled must fail, got %+v", got)
	}
}

func blobOf(entries ...string) string {
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		parts = append(parts, `{"output":`+strconv.Quote(e)+`,"hidden":false}`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func TestFollowStreamsLogDeltas(t *testing.T) {
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{
		{Status: "in_progress", Logs: blobOf("line1")},
		{Status: "in_progress", Logs: blobOf("line1", "line2", "line3")},
		{Status: "finished", Logs: blobOf("line1", "line2", "line3")},
	}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	var logs [][]string
	for e := range events {
		if e.Kind == deploy.KindLog {
			logs = append(logs, e.Logs)
		}
	}
	if len(logs) != 2 || strings.Join(logs[0], "") != "line1" || strings.Join(logs[1], "") != "line2line3" {
		t.Fatalf("log deltas wrong: %+v", logs)
	}
}

func TestFollowFiltersHiddenLogEntries(t *testing.T) {
	entry := func(output string, hidden bool) string {
		return `{"output":"` + output + `","hidden":` + boolStr(hidden) + `}`
	}
	blob := func(entries ...string) string {
		return "[" + strings.Join(entries, ",") + "]"
	}
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{
		{Status: "in_progress", Logs: blob(entry("building...", false))},
		{Status: "in_progress", Logs: blob(
			entry("building...", false),
			entry("docker run -d coolify-helper", true),
			entry("error from helper", true),
			entry("build done", false),
		)},
		{Status: "finished", Logs: blob(
			entry("building...", false),
			entry("docker run -d coolify-helper", true),
			entry("error from helper", true),
			entry("build done", false),
		)},
	}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	var logs [][]string
	for e := range events {
		if e.Kind == deploy.KindLog {
			logs = append(logs, e.Logs)
		}
	}
	if len(logs) != 2 || strings.Join(logs[0], "") != "building..." || strings.Join(logs[1], "") != "build done" {
		t.Fatalf("hidden entries must never stream: %+v", logs)
	}
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func TestFollowTimeout(t *testing.T) {
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{{Status: "in_progress"}}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   5 * time.Millisecond,
		Timeout:        20 * time.Millisecond,
	})
	got := collect(t, events)
	last := got[len(got)-1]
	if last.Kind != deploy.KindFailed || !strings.Contains(last.Message, "timed out") {
		t.Fatalf("expected timeout failure, got %+v", last)
	}
}

type ctxErrClient struct{}

func (ctxErrClient) Deployment(ctx context.Context, uuid string) (coolify.Deployment, error) {
	return coolify.Deployment{}, ctx.Err()
}

// The deadline racing the tick must always surface as a timeout failure, not
// as a KindError carrying context.DeadlineExceeded.
func TestFollowDeadlineSurfacesAsTimeout(t *testing.T) {
	events := deploy.Follow(context.Background(), ctxErrClient{}, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   10 * time.Millisecond,
		Timeout:        10 * time.Millisecond,
	})
	got := collect(t, events)
	last := got[len(got)-1]
	if last.Kind != deploy.KindFailed || !strings.Contains(last.Message, "timed out") {
		t.Fatalf("deadline must surface as timed out, got %+v", last)
	}
}

func TestFollowContextCancelClosesQuietly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := &scriptClient{errAt: -1, states: []coolify.Deployment{{Status: "in_progress"}}}
	events := deploy.Follow(ctx, client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	cancel()
	got := collect(t, events)
	for _, e := range got {
		if e.Kind == deploy.KindFailed && strings.Contains(e.Message, "timed out") {
			t.Fatal("user cancel must not look like a timeout")
		}
	}
}

func TestFollowGivesUpAfterConsecutiveErrors(t *testing.T) {
	client := &scriptClient{errAt: 0, states: []coolify.Deployment{{Status: "in_progress"}}}
	events := deploy.Follow(context.Background(), client, deploy.Options{
		DeploymentUUID: "d1",
		PollInterval:   time.Millisecond,
	})
	got := collect(t, events)
	last := got[len(got)-1]
	if last.Kind != deploy.KindError || last.Err == nil {
		t.Fatalf("expected terminal error, got %+v", last)
	}
}
