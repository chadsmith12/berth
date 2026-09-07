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
	"github.com/chadsmith12/berth/pkg/output"
)

type StatusDataView struct {
	ApplicationUUID string              `json:"application_uuid"`
	Name            string              `json:"name,omitempty"`
	Status          string              `json:"status,omitempty"`
	GitRepository   string              `json:"git_repository,omitempty"`
	GitBranch       string              `json:"git_branch,omitempty"`
	Domains         []string            `json:"domains,omitempty"`
	ComposeLocation string              `json:"compose_location,omitempty"`
	LastDeployment  *LastDeploymentView `json:"last_deployment,omitempty"`
}

type LastDeploymentView struct {
	DeploymentUUID  string `json:"deployment_uuid"`
	Status          string `json:"status"`
	Commit          string `json:"commit,omitempty"`
	DurationSeconds int    `json:"duration_seconds,omitempty"`
	CreatedAt       string `json:"created_at,omitempty"`
	FinishedAt      string `json:"finished_at,omitempty"`
	LogsURL         string `json:"logs_url,omitempty"`
}

func (v StatusDataView) WriteText(w io.Writer) {
	name := v.Name
	if name == "" {
		name = "application"
	}
	fmt.Fprintf(w, "%s (%s)\n", name, v.ApplicationUUID)
	if v.Status != "" {
		fmt.Fprintf(w, "  status     %s\n", appState(v.Status))
	}
	if v.GitBranch != "" {
		fmt.Fprintf(w, "  branch     %s\n", v.GitBranch)
	}
	if v.GitRepository != "" {
		fmt.Fprintf(w, "  repo       %s\n", v.GitRepository)
	}
	if len(v.Domains) > 0 {
		fmt.Fprintf(w, "  domain     %s\n", strings.Join(v.Domains, ", "))
	}
	if v.ComposeLocation != "" {
		fmt.Fprintf(w, "  compose    %s\n", v.ComposeLocation)
	}
	if v.LastDeployment == nil {
		fmt.Fprintln(w, "\nno deployments yet")
		return
	}
	d := v.LastDeployment
	fmt.Fprintf(w, "\nlast deployment  %s\n", d.DeploymentUUID)
	fmt.Fprintf(w, "  %s", d.Status)
	if d.DurationSeconds > 0 {
		fmt.Fprintf(w, " · %s", humanSeconds(d.DurationSeconds))
	}
	if d.Commit != "" {
		fmt.Fprintf(w, " · commit %s", shortCommit(d.Commit))
	}
	fmt.Fprint(w, "\n")
	if started := shortTime(d.CreatedAt); started != "" {
		fmt.Fprintf(w, "  started %s", started)
		if finished := shortTime(d.FinishedAt); finished != "" {
			fmt.Fprintf(w, " · finished %s", finished)
		}
		fmt.Fprint(w, "\n")
	}
	if d.LogsURL != "" {
		fmt.Fprintf(w, "  full logs: %s\n", d.LogsURL)
	}
}

type statusInput struct {
	appUUID string
}

func NewStatusCommand() *cli.Command {
	cmd := cli.NewCommand("status", "what is configured, and the last deployment")
	cmd.Long = `Shows the linked application's configuration — status, branch,
repository, domains, compose file — and its most recent deployment. status
reads only: it writes nothing to the repository and changes nothing in
Coolify.

The application uuid comes from --uuid, BERTH_UUID, or the repository's
environment registry (recorded by berth launch).`
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	appUUID := fs.String("uuid", "", "the application uuid (or BERTH_UUID, or the registry)")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runStatus(statusInput{appUUID: *appUUID}, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runStatus(in statusInput, ctx *cli.CmdContext) (any, error) {
	sess, err := Open(ctx, SessionOptions{VerifyTeam: true})
	if err != nil {
		return nil, err
	}
	uuid, err := resolveAppUUID(sess, in.appUUID, sessionEnv(sess))
	if err != nil {
		return nil, err
	}

	c := context.Background()
	app, err := sess.Client.Application(c, uuid)
	if err != nil {
		return nil, classifyAPIError(err)
	}

	domains := app.ComposeDomains()
	if len(domains) == 0 && app.Fqdn != "" {
		domains = []string{app.Fqdn}
	}
	view := StatusDataView{
		ApplicationUUID: app.UUID,
		Name:            app.Name,
		Status:          app.Status,
		GitRepository:   app.GitRepository,
		GitBranch:       app.GitBranch,
		Domains:         domains,
		ComposeLocation: app.DockerComposeLocation,
	}

	deployments, err := sess.Client.ApplicationDeployments(c, uuid)
	if err != nil {
		return nil, classifyAPIError(err)
	}
	if len(deployments) > 0 {
		view.LastDeployment = lastDeploymentView(sess, deployments[0])
	}
	return view, nil
}

// lastDeploymentView models the newest record from the application's
// deployment list.
func lastDeploymentView(sess *Session, d coolify.Deployment) *LastDeploymentView {
	v := &LastDeploymentView{
		DeploymentUUID: d.DeploymentUUID,
		Status:         d.Status,
		Commit:         d.Commit,
		CreatedAt:      d.CreatedAt,
		FinishedAt:     d.FinishedAt,
		LogsURL:        logsURL(sess, d),
	}
	if secs, ok := recordDuration(d); ok {
		v.DurationSeconds = secs
	}
	return v
}

func shortTime(ts string) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	return t.UTC().Format("2006-01-02 15:04")
}
