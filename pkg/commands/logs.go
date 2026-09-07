package commands

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/output"
)

type LogsDataView struct {
	ApplicationUUID string `json:"application_uuid"`
	Logs            string `json:"logs"`
}

func (v LogsDataView) WriteText(w io.Writer) {
	if strings.TrimSpace(v.Logs) == "" {
		fmt.Fprintln(w, "no logs yet")
		return
	}
	fmt.Fprintln(w, strings.TrimRight(v.Logs, "\n"))
}

type logsInput struct {
	appUUID string
}

func NewLogsCommand() *cli.Command {
	cmd := cli.NewCommand("logs", "the running application's container logs")
	cmd.Long = `Prints the running application's recent container logs. logs reads
only: it writes nothing to the repository and changes nothing in Coolify.

The application uuid comes from --uuid, BERTH_UUID, or the repository's
environment registry (recorded by berth launch).`
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	appUUID := fs.String("uuid", "", "the application uuid (or BERTH_UUID, or the registry)")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runLogs(logsInput{appUUID: *appUUID}, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runLogs(in logsInput, ctx *cli.CmdContext) (any, error) {
	sess, err := Open(ctx, SessionOptions{VerifyTeam: true})
	if err != nil {
		return nil, err
	}
	uuid, err := resolveAppUUID(sess, in.appUUID, sessionEnv(sess))
	if err != nil {
		return nil, err
	}

	c := context.Background()
	logs, err := sess.Client.ApplicationLogs(c, uuid)
	if err != nil {
		// Coolify refuses the request while the application is not
		// running; explain with its current state so the error names the fix.
		if app, derr := sess.Client.Application(c, uuid); derr == nil && app.Status != "" && appState(app.Status) != "running" {
			return nil, fmt.Errorf("application is %s — run berth deploy to start it, then read logs again", appState(app.Status))
		}
		return nil, classifyAPIError(err)
	}
	return LogsDataView{ApplicationUUID: uuid, Logs: logs}, nil
}
