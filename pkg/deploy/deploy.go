package deploy

import (
	"context"
	"strings"
	"time"

	"github.com/chadsmith12/berth/pkg/coolify"
)

// EventKind labels what a stream event carries.
type EventKind string

const (
	KindQueued EventKind = "queued"
	KindStatus EventKind = "status"
	KindLog    EventKind = "log"
	KindDone   EventKind = "done"
	KindFailed EventKind = "failed"
	KindError  EventKind = "error"
)

// Event is one update from a deployment follow. Coolify has no log stream,
// so the producer polls and diffs; that is invisible behind this signature.
type Event struct {
	Kind    EventKind
	Status  string
	Logs    []string
	Message string
	Err     error
}

// Client is the slice of the Coolify API following needs.
type Client interface {
	Deployment(ctx context.Context, uuid string) (coolify.Deployment, error)
}

// Options configures a follow.
type Options struct {
	DeploymentUUID string
	PollInterval   time.Duration // default 2s
	Timeout        time.Duration // 0 = no timeout
}

// Terminal statuses. Anything else keeps polling — forward compatible with
// statuses this version has not seen. Any status mentioning "cancel" is a
// failure (Coolify uses cancelled_by_user).
const (
	statusFinished = "finished"
	statusFailed   = "failed"
)

// Follow streams a deployment's progress until it reaches a terminal state,
// the timeout expires, or ctx is cancelled. The channel is closed at the
// end; the last event is always terminal (Done, Failed, or Error).
func Follow(ctx context.Context, client Client, opts Options) <-chan Event {
	events := make(chan Event)
	interval := opts.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}

	go func() {
		defer close(events)
		watchCtx := ctx
		var cancel context.CancelFunc
		if opts.Timeout > 0 {
			watchCtx, cancel = context.WithTimeout(ctx, opts.Timeout)
			defer cancel()
		}

		// Terminal events select on the parent ctx: after a timeout the
		// watch ctx is already done, and the final event must still reach
		// the consumer.
		emit := func(e Event) bool {
			select {
			case events <- e:
				return true
			case <-ctx.Done():
				return false
			}
		}

		terminal := func(kind EventKind, d coolify.Deployment, msg string, err error) bool {
			return emit(Event{Kind: kind, Status: d.Status, Message: msg, Err: err})
		}

		var lastStatus, lastBlob string
		failures := 0
		tick := time.NewTicker(interval)
		defer tick.Stop()

		blob := func(d coolify.Deployment) string {
			return strings.Join(d.VisibleLogs(), "\n")
		}

		poll := func() (coolify.Deployment, error) {
			return client.Deployment(watchCtx, opts.DeploymentUUID)
		}

		first, err := poll()
		if err != nil {
			terminal(KindError, coolify.Deployment{}, "", err)
			return
		}
		lastStatus, lastBlob = first.Status, blob(first)
		emit(Event{Kind: KindQueued, Status: first.Status})
		emitLogDelta(events, ctx, lastBlob, "")

		for {
			select {
			case <-watchCtx.Done():
				if ctx.Err() == nil {
					terminal(KindFailed, coolify.Deployment{Status: lastStatus},
						"timed out after "+opts.Timeout.String(), nil)
				}
				return
			case <-tick.C:
			}

			d, err := poll()
			if err != nil {
				failures++
				if failures > 3 {
					terminal(KindError, coolify.Deployment{}, "", err)
					return
				}
				continue
			}
			failures = 0

			if d.Status != lastStatus {
				lastStatus = d.Status
				if !emit(Event{Kind: KindStatus, Status: d.Status}) {
					return
				}
			}
			if !emitLogDelta(events, ctx, blob(d), lastBlob) {
				return
			}
			lastBlob = blob(d)

			switch {
			case d.Status == statusFinished:
				terminal(KindDone, d, "", nil)
				return
			case d.Status == statusFailed || strings.Contains(d.Status, "cancel"):
				terminal(KindFailed, d, "", nil)
				return
			}
		}
	}()

	return events
}

// emitLogDelta emits the lines appended since prev. Coolify appends to the
// log blob; a record without logs (4.3.14) simply never emits Log events.
func emitLogDelta(events chan Event, ctx context.Context, current, prev string) bool {
	if current == "" || current == prev {
		return true
	}
	lines := strings.Split(strings.TrimRight(current, "\n"), "\n")
	prevCount := 0
	if prev != "" {
		prevCount = len(strings.Split(strings.TrimRight(prev, "\n"), "\n"))
	}
	if len(lines) <= prevCount {
		return true
	}
	select {
	case events <- Event{Kind: KindLog, Logs: lines[prevCount:]}:
		return true
	case <-ctx.Done():
		return false
	}
}
