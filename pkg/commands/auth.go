package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

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
	Instance  string   `json:"instance"`
	Team      TeamView `json:"team"`
	Profile   string   `json:"profile"`
	IsDefault bool     `json:"is_default"`
}

func (v LoginDataView) WriteText(w io.Writer) {
	fmt.Fprintf(w, "✓ stored token for team %q (id %d) on %s\n", v.Team.Name, v.Team.Id, v.Instance)
	fmt.Fprintf(w, "  profile %q", v.Profile)
	if v.IsDefault {
		fmt.Fprint(w, " set as default")
	}
	fmt.Fprintln(w)
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

type ProfileView struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	TeamID    int    `json:"team_id"`
	TeamName  string `json:"team_name"`
	IsDefault bool   `json:"is_default"`
}

type AuthListDataView struct {
	Profiles  []ProfileView  `json:"profiles"`
	Instances []InstanceView `json:"instances"`
}

func (v AuthListDataView) WriteText(w io.Writer) {
	if len(v.Profiles) == 0 && len(v.Instances) == 0 {
		fmt.Fprintln(w, "no profiles or tokens stored")
		return
	}
	if len(v.Profiles) > 0 {
		fmt.Fprintln(w, "profiles:")
		for _, p := range v.Profiles {
			marker := " "
			if p.IsDefault {
				marker = "*"
			}
			fmt.Fprintf(w, "  %s %-24s %-40s %s (id %d)\n", marker, p.Name, p.URL, p.TeamName, p.TeamID)
		}
		fmt.Fprintln(w, "")
	}
	for _, inst := range v.Instances {
		fmt.Fprintf(w, "%s\n", inst.URL)
		for _, t := range inst.Teams {
			fmt.Fprintf(w, "  [%d] %-20s token %s\n", t.ID, t.Name, t.Token)
		}
	}
}

func NewAuthCommand() *cli.Command {
	auth := cli.NewCommand("auth", "manage stored coolify credentials")
	auth.AddCommand(newAuthLoginCommand())
	auth.AddCommand(newAuthListCommand())
	auth.AddCommand(newAuthDefaultCommand())
	auth.AddCommand(newAuthWhoamiCommand())
	auth.AddCommand(newAuthLogoutCommand())
	return auth
}

func newAuthLoginCommand() *cli.Command {
	cmd := cli.NewCommand("login", "store a coolify token as a named profile")
	cmd.Long = `Verifies the token against Coolify (GET /teams/current) and stores it under
the team it belongs to, as a named profile — an instance and team pair you
select with --profile.

The profile name is prompted with a suggestion derived from the team name;
--as names it directly. Plain login never changes the default profile:
--default does, and the first profile you create becomes it.`
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	urlFlag := fs.String("url", "", "coolify instance url (or BERTH_URL)")
	tokenStdin := fs.Bool("token-stdin", false, "read the token from stdin instead of prompting")
	asFlag := fs.String("as", "", "profile name (skips the name prompt)")
	defaultFlag := fs.Bool("default", false, "make this profile the default")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthLogin(*urlFlag, *tokenStdin, *asFlag, *defaultFlag, ctx)
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

func runAuthLogin(urlFlag string, tokenStdin bool, asName string, setDefault bool, ctx *cli.CmdContext) (any, error) {
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

	token, err := readToken(ctx, tokenStdin)
	if err != nil {
		return nil, err
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

	userPath, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		return nil, err
	}

	name := asName
	if name == "" {
		slug := config.ProfileSlug(team.Name)
		name, err = term.PromptDefault("profile name?", slug)
		if err != nil {
			return nil, output.Usage(fmt.Errorf("profile name: %w", err))
		}
		if name == "" {
			name = slug
		}
	}
	if user.ProfileConflict(name, url, team.Id) {
		return nil, output.Usage(fmt.Errorf("a profile named %q already exists for a different instance or team — pass --as to choose another name", name))
	}

	user.UpsertProfile(config.Profile{Name: name, URL: config.NormalizeURL(url), Team: &config.TeamRef{ID: team.Id, Name: team.Name}})
	isDefault := setDefault || user.Default == ""
	if isDefault {
		user.Default = name
	}
	if err := config.SaveUserConfig(userPath, user); err != nil {
		return nil, err
	}

	return LoginDataView{
		Instance:  config.NormalizeURL(url),
		Team:      TeamView{Id: team.Id, Name: team.Name},
		Profile:   name,
		IsDefault: isDefault,
	}, nil
}

// readToken takes the token from stdin when asked, else prompts. Tokens are
// never a flag: it leaks into shell history and process lists.
func readToken(ctx *cli.CmdContext, fromStdin bool) (string, error) {
	if fromStdin {
		b, err := io.ReadAll(ctx.Stdin)
		if err != nil {
			return "", output.Usage(fmt.Errorf("token: read stdin: %w", err))
		}
		token := strings.TrimSpace(string(b))
		if token == "" {
			return "", output.Usage(errors.New("no token on stdin — pipe it in, e.g. `berth auth login --url <url> --token-stdin < token.txt`"))
		}
		return token, nil
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)
	token, err := term.PromptPassword("token?")
	if err != nil {
		return "", output.Usage(fmt.Errorf("token: %w", err))
	}
	return token, nil
}

func runAuthList() (any, error) {
	userPath, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		return nil, err
	}
	path, err := config.CredentialsPath()
	if err != nil {
		return nil, err
	}
	creds, err := config.LoadCredentials(path)
	if err != nil {
		return nil, err
	}
	return AuthListDataView{
		Profiles:  profileViews(user),
		Instances: instanceViews(creds),
	}, nil
}

func profileViews(user config.UserConfig) []ProfileView {
	views := make([]ProfileView, 0, len(user.Profiles))
	for _, p := range user.Profiles {
		v := ProfileView{Name: p.Name, URL: p.URL, IsDefault: p.Name == user.Default}
		if p.Team != nil {
			v.TeamID, v.TeamName = p.Team.ID, p.Team.Name
		}
		views = append(views, v)
	}
	return views
}

func instanceViews(creds config.Credentials) []InstanceView {
	views := make([]InstanceView, 0, len(creds.Instances))
	for _, inst := range creds.Instances {
		iv := InstanceView{URL: inst.URL, Teams: make([]TeamTokenView, 0, len(inst.Teams))}
		for _, t := range inst.Teams {
			iv.Teams = append(iv.Teams, TeamTokenView{ID: t.ID, Name: t.Name, Token: config.TruncateToken(t.Token)})
		}
		views = append(views, iv)
	}
	return views
}

type DefaultDataView struct {
	Set      bool          `json:"set,omitempty"`
	Default  string        `json:"default,omitempty"`
	Profiles []ProfileView `json:"profiles"`
}

func (v DefaultDataView) WriteText(w io.Writer) {
	if v.Set {
		fmt.Fprintf(w, "✓ default profile is now %q\n", v.Default)
	}
	if len(v.Profiles) == 0 {
		fmt.Fprintln(w, "no profiles yet — run berth auth login")
		return
	}
	for _, p := range v.Profiles {
		marker := " "
		if p.IsDefault {
			marker = "*"
		}
		fmt.Fprintf(w, "%s %-24s %-40s %s (id %d)\n", marker, p.Name, p.URL, p.TeamName, p.TeamID)
	}
}

func newAuthDefaultCommand() *cli.Command {
	cmd := cli.NewCommand("default", "show or set the default profile")
	cmd.Long = `Without an argument, lists profiles and marks the default. With a profile
name, makes that profile the default without re-entering a token.`
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthDefault(args)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runAuthDefault(args []string) (any, error) {
	userPath, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	user, err := config.LoadUserConfig(userPath)
	if err != nil {
		return nil, err
	}
	if len(args) > 1 {
		return nil, output.Usage(fmt.Errorf("expected at most one profile name, got %v", args))
	}
	set := false
	if len(args) == 1 {
		if err := user.SetDefault(args[0]); err != nil {
			return nil, output.Usage(err)
		}
		if err := config.SaveUserConfig(userPath, user); err != nil {
			return nil, err
		}
		set = true
	}
	return DefaultDataView{Set: set, Default: user.Default, Profiles: profileViews(user)}, nil
}

type WhoamiDataView struct {
	Instance   string    `json:"instance"`
	Team       TeamView  `json:"team"`
	Configured *TeamView `json:"configured_team,omitempty"`
}

func (v WhoamiDataView) WriteText(w io.Writer) {
	fmt.Fprintf(w, "✓ acting as team %q (id %d) on %s\n", v.Team.Name, v.Team.Id, v.Instance)
	if v.Configured != nil && v.Configured.Id != v.Team.Id {
		fmt.Fprintf(w, "⚠ the configured team is %q (id %d), but the token belongs to %q (id %d)\n",
			v.Configured.Name, v.Configured.Id, v.Team.Name, v.Team.Id)
	}
}

func newAuthWhoamiCommand() *cli.Command {
	cmd := cli.NewCommand("whoami", "show which team the token acts as")
	cmd.Long = `Validates a token against Coolify via GET /teams/current and reports the
team and instance it acts as, warning when that differs from the configured
default team.

The URL comes from --url, BERTH_URL, the repository config, or the default
set by berth auth login. The token comes from BERTH_TOKEN or stored
credentials.`
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	urlFlag := fs.String("url", "", "coolify instance url (or BERTH_URL)")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthWhoami(*urlFlag, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

type LogoutDataView struct {
	Instance string    `json:"instance"`
	Team     *TeamView `json:"team,omitempty"`
	Removed  bool      `json:"removed"`
}

func (v LogoutDataView) WriteText(w io.Writer) {
	if v.Removed {
		switch {
		case v.Team != nil && v.Team.Name != "":
			fmt.Fprintf(w, "✓ removed the token for team %q (id %d) on %s\n", v.Team.Name, v.Team.Id, v.Instance)
		case v.Team != nil:
			fmt.Fprintf(w, "✓ removed the token for team id %d on %s\n", v.Team.Id, v.Instance)
		default:
			fmt.Fprintf(w, "✓ removed the token on %s\n", v.Instance)
		}
		return
	}
	if v.Team != nil {
		fmt.Fprintf(w, "no token stored for team %q (id %d) on %s\n", v.Team.Name, v.Team.Id, v.Instance)
		return
	}
	fmt.Fprintf(w, "no tokens stored for %s\n", v.Instance)
}

func newAuthLogoutCommand() *cli.Command {
	cmd := cli.NewCommand("logout", "remove a stored token")
	cmd.Long = `Removes the stored token for an instance and team. The instance comes from
--url, BERTH_URL, the repository config, or the default set by
berth auth login. The team comes from --team, BERTH_TEAM, or the default
team; with no team resolved and exactly one token stored, that token is
removed.`
	fs := flag.NewFlagSet("logout", flag.ContinueOnError)
	urlFlag := fs.String("url", "", "coolify instance url (or BERTH_URL)")
	teamFlag := fs.Int("team", -1, "team id whose token to remove (or BERTH_TEAM)")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runAuthLogout(*urlFlag, *teamFlag, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runAuthWhoami(urlFlag string, ctx *cli.CmdContext) (any, error) {
	sess, err := Open(ctx, SessionOptions{URL: urlFlag, OptionalTeam: true})
	if err != nil {
		return nil, err
	}
	team, err := sess.Client.CurrentTeam(context.Background())
	if err != nil {
		return nil, output.Auth(err)
	}

	v := WhoamiDataView{Instance: sess.Placement.URL, Team: TeamView{Id: team.Id, Name: team.Name}}
	if sess.Placement.Team != nil && sess.Placement.Team.ID != team.Id {
		v.Configured = &TeamView{Id: sess.Placement.Team.ID, Name: sess.Placement.Team.Name}
	}
	return v, nil
}

func runAuthLogout(urlFlag string, teamFlag int, ctx *cli.CmdContext) (any, error) {
	opts := SessionOptions{URL: urlFlag, OptionalTeam: true, PlacementOnly: true}
	if teamFlag >= 0 {
		opts.Team = strconv.Itoa(teamFlag)
	}
	sess, err := Open(ctx, opts)
	if err != nil {
		return nil, err
	}

	credPath, err := config.CredentialsPath()
	if err != nil {
		return nil, err
	}
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return nil, err
	}
	tokens := config.TokensFor(creds, sess.Placement.URL)

	var team *TeamView
	if sess.Placement.Team != nil {
		team = &TeamView{Id: sess.Placement.Team.ID, Name: sess.Placement.Team.Name}
	}
	if team == nil {
		switch len(tokens) {
		case 0:
			return LogoutDataView{Instance: sess.Placement.URL}, nil
		case 1:
			team = &TeamView{Id: tokens[0].ID, Name: tokens[0].Name}
		default:
			return nil, output.Usage(fmt.Errorf("several tokens are stored for %s (%s) — pass --team to choose which to remove", sess.Placement.URL, teamIDList(tokens)))
		}
	} else if team.Name == "" {
		team.Name = teamNameForID(tokens, team.Id)
	}

	removed := config.RemoveToken(&creds, sess.Placement.URL, team.Id)
	if removed {
		if err := config.SaveCredentials(credPath, creds); err != nil {
			return nil, err
		}
	}
	return LogoutDataView{Instance: sess.Placement.URL, Team: team, Removed: removed}, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
