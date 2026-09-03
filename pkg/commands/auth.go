package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/config"
	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/input"
	"github.com/chadsmith12/berth/pkg/output"
)

type TeamView struct {
	Id   int    `json:"id"`
	Name string `json:"name"`
}

type LoginDataView struct {
	Instance string   `json:"instance"`
	Team     TeamView `json:"team"`
}

func (v LoginDataView) WriteText(w io.Writer) {
	fmt.Fprintf(w, "✓ stored token for team %q (id %d) on %s\n", v.Team.Name, v.Team.Id, v.Instance)
}

type TeamTokenView struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

type InstanceView struct {
	URL   string          `json:"url"`
	Teams []TeamTokenView `json:"teams"`
}

type AuthListDataView struct {
	Instances []InstanceView `json:"instances"`
}

func (v AuthListDataView) WriteText(w io.Writer) {
	if len(v.Instances) == 0 {
		fmt.Fprintln(w, "no tokens stored")
		return
	}
	for _, inst := range v.Instances {
		fmt.Fprintf(w, "\n%s\n", inst.URL)
		for _, t := range inst.Teams {
			fmt.Fprintf(w, "  [%d] %-20s token %s\n", t.ID, t.Name, t.Token)
		}
	}
}

func NewAuthCommand() *cli.Command {
	auth := cli.NewCommand("auth", "manage stored coolify credentials")
	auth.AddCommand(newAuthLoginCommand())
	auth.AddCommand(newAuthListCommand())
	return auth
}

func newAuthLoginCommand() *cli.Command {
	cmd := cli.NewCommand("login", "store a coolify token for a team")
	cmd.Long = `Prompt for an instance URL and API token, verify the token against
Coolify, and file it under the team it belongs to.

The URL comes from --url or BERTH_URL; the token is never a flag,
so it cannot leak into process lists or CI logs.`
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	urlFlag := fs.String("url", "", "coolify instance url (or BERTH_URL)")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthLogin(*urlFlag, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func newAuthListCommand() *cli.Command {
	cmd := cli.NewCommand("list", "list stored tokens by instance and team")
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthList()
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runAuthLogin(urlFlag string, ctx *cli.CmdContext) (any, error) {
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)

	url := firstNonEmpty(urlFlag, os.Getenv("BERTH_URL"))
	if url == "" && !ctx.Interactive {
		return nil, output.Usage(errors.New("missing instance url — pass --url or set BERTH_URL"))
	}
	if url == "" {
		answered, err := term.Prompt("instance url?")
		if err != nil {
			return nil, output.Usage(fmt.Errorf("--url: %w", err))
		}
		url = answered
	}
	if url == "" {
		return nil, output.Usage(errors.New("missing instance url — pass --url or set BERTH_URL"))
	}

	token, err := term.PromptPassword("token?")
	if err != nil {
		return nil, output.Usage(fmt.Errorf("token: %w", err))
	}
	if !coolify.ValidTokenFormat(token) {
		return nil, output.Usage(errors.New("invalid token format — expected \"<id>|<secret>\" like 12|abc123 (both parts are required)"))
	}

	client := coolify.New(url, token)
	team, err := client.CurrentTeam(context.Background())
	if err != nil {
		return nil, output.Auth(err)
	}

	path, err := config.CredentialsPath()
	if err != nil {
		return nil, err
	}
	creds, err := config.LoadCredentials(path)
	if err != nil {
		return nil, err
	}
	config.UpsertToken(&creds, url, config.TeamToken{ID: team.Id, Name: team.Name, Token: token})
	if err := config.SaveCredentials(path, creds); err != nil {
		return nil, err
	}

	return LoginDataView{Instance: config.NormalizeURL(url), Team: TeamView{Id: team.Id, Name: team.Name}}, nil
}

func runAuthList() (any, error) {
	path, err := config.CredentialsPath()
	if err != nil {
		return nil, err
	}
	creds, err := config.LoadCredentials(path)
	if err != nil {
		return nil, err
	}
	v := AuthListDataView{Instances: make([]InstanceView, 0, len(creds.Instances))}
	for _, inst := range creds.Instances {
		iv := InstanceView{URL: inst.URL, Teams: make([]TeamTokenView, 0, len(inst.Teams))}
		for _, t := range inst.Teams {
			iv.Teams = append(iv.Teams, TeamTokenView{ID: t.ID, Name: t.Name, Token: config.TruncateToken(t.Token)})
		}
		v.Instances = append(v.Instances, iv)
	}
	return v, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
