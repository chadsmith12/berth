package commands

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/config"
	"github.com/chadsmith12/berth/pkg/coolify"
	"github.com/chadsmith12/berth/pkg/output"
)

// Session is everything a command needs to act against Coolify, resolved
// once per invocation: the placement, the token, a client with the team
// pre-set so 404s name it, and the repository's environment registry.
type Session struct {
	Placement config.Placement
	Token     string
	Client    *coolify.Client
	Env       string
	Repo      *config.RepoConfig
	RepoRoot  string
}

// SessionOptions are the per-command inputs to Open. Everything global
// (--profile, -c, --env) is bound automatically from the command context.
type SessionOptions struct {
	URL           string // --url flag; overrides the identity's instance
	Team          string // --team flag (a team id); overrides the identity's team
	Project       string // --project flag; overrides the repo's project
	RepoRoot      string // project root for the repository config; defaults to "."
	OptionalTeam  bool   // leave the team unresolved instead of erroring
	VerifyTeam    bool   // refuse when the token acts as a different team
	PlacementOnly bool   // resolve the identity without a token or client (logout)
}

// Open resolves the placement and the token, and builds the Coolify client.
// It is the single resolution point for commands: the placement chain, the
// token chain, and — for acting commands — the team drift check all happen
// here, with exit codes attached. Resolution and token errors are usage
// errors; API errors are auth errors. A configured team is verified when
// asked; with no team configured (pipeline mode) the token alone identifies
// the actor.
func Open(ctx *cli.CmdContext, opts SessionOptions) (*Session, error) {
	repoRoot := opts.RepoRoot
	if repoRoot == "" {
		repoRoot = "."
	}
	repo, err := config.LoadRepoFor(repoRoot, ctx.Globals.Config)
	if err != nil {
		return nil, output.Usage(err)
	}

	overrides := config.Overrides{
		URL:     opts.URL,
		Project: opts.Project,
	}
	if opts.Team != "" {
		id, err := strconv.Atoi(strings.TrimSpace(opts.Team))
		if err != nil {
			return nil, output.Usage(fmt.Errorf("--team must be a team id (an integer), got %q", opts.Team))
		}
		overrides.TeamID = id
	}
	if ctx.Globals.Profile != "" {
		overrides.Profile = ctx.Globals.Profile
	}

	envToken := strings.TrimSpace(os.Getenv("BERTH_TOKEN"))

	// Pipeline mode supplies identity via BERTH_TOKEN, so no configured team
	// is required; VerifyTeam checks the team only when one is configured.
	placement, err := config.Resolve(config.ResolveOptions{
		Overrides:    overrides,
		ConfigPath:   ctx.Globals.Config,
		Repo:         repo,
		OptionalTeam: opts.OptionalTeam || opts.VerifyTeam || envToken != "",
	})
	if err != nil {
		return nil, output.Usage(err)
	}

	sess := &Session{Placement: placement, Env: ctx.Globals.Env, Repo: repo, RepoRoot: repoRoot}
	if opts.PlacementOnly {
		return sess, nil
	}

	token, err := openToken(placement, envToken, opts.OptionalTeam)
	if err != nil {
		return nil, err
	}
	if !coolify.ValidTokenFormat(token) {
		return nil, output.Usage(errors.New("invalid token format — expected \"<id>|<secret>\" like 12|abc123 (both parts are required)"))
	}

	client := coolify.New(placement.URL, token)
	if placement.Team != nil {
		client.SetTeam(placement.Team.Name, placement.Team.ID)
	}
	if opts.VerifyTeam && placement.Team != nil {
		if _, err := client.VerifyTeam(context.Background(), placement.Team.ID); err != nil {
			return nil, output.Auth(err)
		}
	}

	sess.Token = token
	sess.Client = client
	return sess, nil
}

// openToken takes BERTH_TOKEN first, then the stored token for the resolved
// team. When the team is optional — diagnostic commands — it falls back to
// the instance's single stored token.
func openToken(placement config.Placement, envToken string, optionalTeam bool) (string, error) {
	if envToken != "" {
		return envToken, nil
	}
	credPath, err := config.CredentialsPath()
	if err != nil {
		return "", err
	}
	creds, err := config.LoadCredentials(credPath)
	if err != nil {
		return "", err
	}
	if placement.Team != nil {
		if tok, ok := config.FindToken(creds, placement.URL, placement.Team.ID); ok {
			return tok, nil
		}
		if !optionalTeam {
			return "", output.Usage(fmt.Errorf("no token stored for team %s (id %d) on %s — run berth auth login or set BERTH_TOKEN", placement.Team.Name, placement.Team.ID, placement.URL))
		}
	}
	tokens := config.TokensFor(creds, placement.URL)
	switch len(tokens) {
	case 1:
		return tokens[0].Token, nil
	case 0:
		return "", output.Usage(fmt.Errorf("no token stored for %s — run berth auth login or set BERTH_TOKEN", placement.URL))
	default:
		return "", output.Usage(fmt.Errorf("several tokens are stored for %s (%s) — set BERTH_TOKEN or select one with --profile", placement.URL, teamIDList(tokens)))
	}
}

func teamNameForID(tokens []config.TeamToken, teamID int) string {
	for _, t := range tokens {
		if t.ID == teamID {
			return t.Name
		}
	}
	return ""
}

func teamIDList(tokens []config.TeamToken) string {
	parts := make([]string, 0, len(tokens))
	for _, t := range tokens {
		parts = append(parts, fmt.Sprintf("%d (%s)", t.ID, t.Name))
	}
	return strings.Join(parts, ", ")
}
