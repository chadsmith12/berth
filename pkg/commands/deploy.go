package commands

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/deploy"
	"github.com/chadsmith12/berth/pkg/output"
)

type DeployDataView struct {
	ApplicationUUID string   `json:"application_uuid,omitempty"`
	DeploymentUUID  string   `json:"deployment_uuid"`
	Status          string   `json:"status"`
	Triggered       bool     `json:"triggered"`
	Commit          string   `json:"commit,omitempty"`
	DurationSeconds int      `json:"duration_seconds,omitempty"`
	LogsURL         string   `json:"logs_url,omitempty"`
	Tail            []string `json:"log_tail,omitempty"`
	Streamed        bool     `json:"streamed,omitempty"`
}

func (v DeployDataView) WriteText(w io.Writer) {
	switch {
	case v.Status == "detached":
		fmt.Fprintf(w, "▶ deployment %s queued\n", v.DeploymentUUID)
		fmt.Fprintf(w, "\nfollow it: berth deploy --deployment %s\n", v.DeploymentUUID)
	case v.Status == "finished":
		fmt.Fprintf(w, "✓ deployed in %s", humanSeconds(v.DurationSeconds))
		if v.Commit != "" {
			fmt.Fprintf(w, " (commit %s)", shortCommit(v.Commit))
		}
		fmt.Fprint(w, "\n")
		if v.LogsURL != "" {
			fmt.Fprintf(w, "  logs: %s\n", v.LogsURL)
		}
	default:
		fmt.Fprintf(w, "✗ deployment %s in %s", v.Status, humanSeconds(v.DurationSeconds))
		if v.Commit != "" {
			fmt.Fprintf(w, " (commit %s)", shortCommit(v.Commit))
		}
		fmt.Fprint(w, "\n")
		if !v.Streamed {
			for _, line := range v.Tail {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
		if v.LogsURL != "" {
			fmt.Fprintf(w, "  full logs: %s\n", v.LogsURL)
		}
	}
}

type deployInput struct {
	appUUID string
	depUUID string
	detach  bool
	timeout time.Duration
}

func NewDeployCommand() *cli.Command {
	cmd := cli.NewCommand("deploy", "trigger a deployment and follow it")
	cmd.Long = `Triggers a deployment and follows it to completion. deploy creates
nothing and changes no Coolify configuration — a uuid and a token are all it
needs, which is what makes it safe in a pipeline:

  BERTH_TOKEN=... BERTH_UUID=... berth deploy

The application uuid comes from --uuid, BERTH_UUID, or the repository's
environment registry (recorded by berth launch). Following streams status
transitions; build output lives on the Coolify deployment page, which is
printed for you.`
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	appUUID := fs.String("uuid", "", "the application uuid (or BERTH_UUID, or the registry)")
	depUUID := fs.String("deployment", "", "follow an existing deployment instead of triggering one")
	detach := fs.Bool("detach", false, "trigger and return without following")
	timeout := fs.Duration("timeout", 20*time.Minute, "give up on the deployment after this long")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		in := deployInput{appUUID: *appUUID, depUUID: *depUUID, detach: *detach, timeout: *timeout}
		data, err := runDeploy(in, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runDeploy(in deployInput, ctx *cli.CmdContext) (any, error) {
	sess, err := Open(ctx, SessionOptions{VerifyTeam: true})
	if err != nil {
		return nil, err
	}
	envName := sessionEnv(sess)

	// Attaching to an existing deployment needs no application uuid.
	appUUID := ""
	if in.depUUID == "" {
		appUUID, err = resolveAppUUID(sess, in.appUUID, envName)
		if err != nil {
			return nil, err
		}
	}

	depUUID := in.depUUID
	triggered := false
	if depUUID == "" {
		depUUID, err = sess.Client.DeployApplication(context.Background(), appUUID)
		if err != nil {
			return nil, classifyAPIError(err)
		}
		triggered = true
		if in.detach {
			return DeployDataView{
				ApplicationUUID: appUUID,
				DeploymentUUID:  depUUID,
				Status:          "detached",
				Triggered:       true,
			}, nil
		}
	}

	events := deploy.Follow(context.Background(), sess.Client, deploy.Options{
		DeploymentUUID: depUUID,
		Timeout:        in.timeout,
	})
	// --json is one final envelope, never an event stream: the live dots and
	// log lines are suppressed (the tail rides in the envelope instead).
	stream := ctx.Stdout
	if ctx.Globals.JSON {
		stream = io.Discard
	}
	start := time.Now()
	var terminal deploy.Event
	for e := range events {
		switch e.Kind {
		case deploy.KindStatus:
			fmt.Fprintf(stream, "● %s\n", e.Status)
		case deploy.KindLog:
			for _, line := range e.Logs {
				fmt.Fprintf(stream, "  %s\n", strings.TrimRight(line, "\r"))
			}
		case deploy.KindDone, deploy.KindFailed, deploy.KindError:
			terminal = e
		}
	}
	if terminal.Kind == deploy.KindError {
		return nil, terminal.Err
	}

	detail, err := sess.Client.Deployment(context.Background(), depUUID)
	if err != nil {
		return nil, err
	}
	duration := int(time.Since(start).Seconds())
	if d, ok := recordDuration(detail); ok {
		duration = d
	}

	view := DeployDataView{
		ApplicationUUID: appUUID,
		DeploymentUUID:  depUUID,
		Status:          detail.Status,
		Triggered:       triggered,
		Commit:          detail.Commit,
		DurationSeconds: duration,
		LogsURL:         logsURL(sess, detail),
		Tail:            logTail(detail, 20),
		Streamed:        stream == ctx.Stdout,
	}
	if detail.Status != "finished" {
		view.Tail = logTail(detail, 20)
		return view, fmt.Errorf("deployment %s — logs: %s", detail.Status, view.LogsURL)
	}
	return view, nil
}

// logTail returns the last n lines of the visible build output — the
// deployment record's hidden entries are Coolify's internal commands, not
// build output.
func logTail(d coolify.Deployment, n int) []string {
	visible := d.VisibleLogs()
	if len(visible) == 0 {
		return nil
	}
	if len(visible) > n {
		visible = visible[len(visible)-n:]
	}
	return visible
}

func logsURL(sess *Session, d coolify.Deployment) string {
	if d.DeploymentURL == "" {
		return ""
	}
	return strings.TrimRight(sess.Placement.URL, "/") + d.DeploymentURL
}

// recordDuration prefers the record's own timestamps so attaching to a
// finished deployment reports its real duration, not the follow time.
func recordDuration(d coolify.Deployment) (int, bool) {
	created, err1 := time.Parse(time.RFC3339, d.CreatedAt)
	finished, err2 := time.Parse(time.RFC3339, d.FinishedAt)
	if err1 != nil || err2 != nil || finished.Before(created) {
		return 0, false
	}
	return int(finished.Sub(created).Seconds()), true
}

func shortCommit(c string) string {
	if len(c) > 7 {
		return c[:7]
	}
	return c
}

func humanSeconds(s int) string {
	switch {
	case s >= 60:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	default:
		return fmt.Sprintf("%ds", s)
	}
}
