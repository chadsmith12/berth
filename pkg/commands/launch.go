package commands

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/config"
	"github.com/chadsmith12/berth/pkg/input"
	"github.com/chadsmith12/berth/pkg/launch"
	"github.com/chadsmith12/berth/pkg/output"
)

type StepView struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Created bool   `json:"created"`
}

type LaunchDataView struct {
	Adopted     bool     `json:"adopted,omitempty"`
	Project     StepView `json:"project"`
	Environment StepView `json:"environment"`
	Server      StepView `json:"server"`
	DeployKey   StepView `json:"deploy_key"`
	Application StepView `json:"application"`
	EnvVars     []string `json:"env_vars,omitempty"`
	PublicKey   string   `json:"public_key,omitempty"`
	Checklist   []string `json:"checklist,omitempty"`
}

func (v LaunchDataView) WriteText(w io.Writer) {
	if v.Adopted {
		fmt.Fprintf(w, "✓ linked application %s\n", v.Application.UUID)
		fmt.Fprintln(w, "\nnext: berth deploy")
		return
	}
	fmt.Fprintf(w, "✓ launched %s/%s\n", v.Project.Name, v.Environment.Name)
	for _, l := range []struct {
		label string
		s     StepView
	}{
		{"project", v.Project},
		{"environment", v.Environment},
		{"server", v.Server},
		{"deploy key", v.DeployKey},
		{"application", v.Application},
	} {
		what := "found"
		if l.s.Created {
			what = "created"
		}
		fmt.Fprintf(w, "  %-12s [%s] %s (%s)\n", l.label, what, l.s.Name, l.s.UUID)
	}
	if len(v.EnvVars) > 0 {
		fmt.Fprintf(w, "  %-12s %s\n", "env vars", strings.Join(v.EnvVars, ", "))
	}
	if len(v.Checklist) > 0 {
		fmt.Fprintln(w, "\nbefore you deploy:")
		for _, c := range v.Checklist {
			fmt.Fprintf(w, "  %s\n", c)
		}
	}
	fmt.Fprintln(w, "\nnext: berth deploy")
}

type launchInput struct {
	path     string
	project  string
	server   string
	branch   string
	domain   string
	database string
	dbName   string
	redis    string
	key      string
	uuid     string
	force    bool
}

func NewLaunchCommand() *cli.Command {
	cmd := cli.NewCommand("launch", "create the Coolify infrastructure for an environment")
	cmd.Long = `Creates the project, environment and application for one environment and
records the application uuid in the repository config. Each step is
find-or-create, so re-running after a partial failure resumes rather than
duplicating. launch never deploys — run berth deploy afterwards.

Every missing value is asked for on a terminal; non-interactively each one is
a usage error naming its flag.`
	fs := flag.NewFlagSet("launch", flag.ContinueOnError)
	path := fs.String("path", ".", "the path to the project directory")
	project := fs.String("project", "", "coolify project name (find by name, else create)")
	server := fs.String("server", "", "coolify server uuid")
	branch := fs.String("branch", "", "git branch to deploy")
	domain := fs.String("domain", "", "the domain for the app service; the port comes from the compose file")
	database := fs.String("database", "", "database uuid to link, or 'none'")
	dbName := fs.String("db-name", "", "the database name to use on that postgres (asked when omitted)")
	redis := fs.String("redis", "", "redis uuid to link (required by horizon stacks)")
	key := fs.String("key", "", "existing deploy key uuid; omitted means pick or create one")
	uuid := fs.String("uuid", "", "adopt an existing application instead of creating one")
	force := fs.Bool("force", false, "overwrite an already recorded application uuid")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		in := launchInput{
			path: *path, project: *project, server: *server, branch: *branch,
			domain: *domain, database: *database, dbName: *dbName, redis: *redis,
			key: *key, uuid: *uuid, force: *force,
		}
		data, err := runLaunch(in, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runLaunch(in launchInput, ctx *cli.CmdContext) (any, error) {
	sess, err := Open(ctx, SessionOptions{VerifyTeam: true, RepoRoot: in.path})
	if err != nil {
		return nil, err
	}
	client := sess.Client
	envName := sess.Env
	if envName == "" {
		envName = "production"
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)

	composePath := filepath.Join(in.path, "docker-compose."+envName+".yml")
	composeInfo, composeErr := launch.ReadCompose(composePath)
	if composeErr != nil {
		if os.IsNotExist(fmt.Errorf("read %s: %w", composePath, composeErr)) || strings.Contains(composeErr.Error(), "no such file") {
			return nil, output.Usage(fmt.Errorf("%s does not exist — run berth generate --env %s first", composePath, envName))
		}
		return nil, output.Usage(composeErr)
	}

	if existing, ok := linkedUUID(sess, envName); ok {
		if in.uuid == "" || in.uuid != existing {
			if in.uuid == existing {
				return adoptView(existing), nil
			}
			if !in.force {
				return nil, output.Usage(fmt.Errorf("environment %q is already linked to application %s — pass --uuid %s to adopt it, or --force to relink", envName, existing, existing))
			}
		}
	}

	if in.uuid != "" {
		return runLaunchAdopt(sess, in, envName)
	}

	repoURL, err := gitRemote(in.path)
	if err != nil {
		return nil, output.Usage(err)
	}
	gitRepo, err := launch.NormalizeGitRepo(repoURL)
	if err != nil {
		return nil, output.Usage(err)
	}
	branch := in.branch
	if branch == "" {
		branch, err = resolveLaunchBranch(ctx, term, in.path)
		if err != nil {
			return nil, err
		}
	}

	projectName, err := resolveLaunchProject(ctx, client, term, in.project)
	if err != nil {
		return nil, err
	}
	serverUUID, err := resolveLaunchServer(ctx, client, term, in.server)
	if err != nil {
		return nil, err
	}
	keyChoice, err := resolveLaunchKey(ctx, client, term, in.key, projectName+"-"+envName+"-deploy-key")
	if err != nil {
		return nil, err
	}

	databaseUUID, err := resolveLaunchDatabase(ctx, client, term, in.database)
	if err != nil {
		return nil, err
	}
	dbName := in.dbName
	if databaseUUID != "" && dbName == "" {
		dbName, err = resolveLaunchDBName(ctx, client, term, databaseUUID)
		if err != nil {
			return nil, err
		}
	}
	redisUUID, err := resolveLaunchRedis(ctx, client, term, in.redis, composeInfo.Horizon)
	if err != nil {
		return nil, err
	}
	domain := in.domain
	if domain == "" {
		domain, err = resolveLaunchDomain(ctx, term)
		if err != nil {
			return nil, err
		}
	}

	res, err := launch.Execute(context.Background(), client, launch.Inputs{
		ProjectName:     projectName,
		EnvironmentName: envName,
		ServerUUID:      serverUUID,
		GitRepository:   gitRepo,
		Branch:          branch,
		PrivateKeyUUID:  keyChoice.UUID,
		Domain:          domain,
		AppPort:         composeInfo.Port,
		DatabaseUUID:    databaseUUID,
		DatabaseName:    dbName,
		RedisUUID:       redisUUID,
		EnvFileName:     "docker-compose." + envName + ".yml",
	})
	if err != nil {
		return nil, err
	}

	if err := recordEnvironmentUUID(sess, envName, res.Application.UUID, in.force); err != nil {
		return nil, err
	}

	return LaunchDataView{
		Project:     stepView(res.Project),
		Environment: stepView(res.Environment),
		Server:      stepView(res.Server),
		DeployKey:   stepView(res.DeployKey),
		Application: stepView(res.Application),
		EnvVars:     res.EnvVars,
		PublicKey:   keyChoice.PublicKey,
		Checklist:   launchChecklist(in.path, branch, keyChoice, res),
	}, nil
}

// launchChecklist collects everything standing between this launch and a
// green deploy: the deploy key that was just created, database notices, and
// the git state of the branch that will be deployed.
func launchChecklist(path, branch string, keyChoice launch.DeployKeyChoice, res launch.Result) []string {
	var list []string
	if keyChoice.Created {
		list = append(list, "add this deploy key to your git host with read access:")
		list = append(list, "    "+keyChoice.PublicKey)
		list = append(list, "    (the first deploy cannot clone until it is added)")
	}
	list = append(list, res.Notices...)

	dirty, unpushed, upstream := gitState(path)
	var parts []string
	if dirty > 0 {
		parts = append(parts, fmt.Sprintf("%d uncommitted file(s)", dirty))
	}
	if unpushed > 0 {
		parts = append(parts, fmt.Sprintf("%d unpushed commit(s)", unpushed))
	}
	if !upstream {
		parts = append(parts, "no upstream yet (push creates it)")
	}
	if len(parts) > 0 {
		list = append(list, fmt.Sprintf("commit and push branch %q — %s", branch, strings.Join(parts, ", ")))
	}
	return list
}

// gitState counts uncommitted files and unpushed commits. No upstream is not
// an error: the push will create it.
func gitState(path string) (dirty, unpushed int, upstream bool) {
	out, err := exec.Command("git", "-C", path, "status", "--porcelain").Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if strings.TrimSpace(line) != "" {
				dirty++
			}
		}
	}
	up, err := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").Output()
	if err != nil {
		return dirty, 0, false
	}
	upstream = true
	if n, err := exec.Command("git", "-C", path, "rev-list", "--count", strings.TrimSpace(string(up))+"..HEAD").Output(); err == nil {
		unpushed, _ = strconv.Atoi(strings.TrimSpace(string(n)))
	}
	return dirty, unpushed, upstream
}

func runLaunchAdopt(sess *Session, in launchInput, envName string) (any, error) {
	app, err := sess.Client.Application(context.Background(), in.uuid)
	if err != nil {
		return nil, output.Auth(err)
	}
	if err := recordEnvironmentUUID(sess, envName, app.UUID, in.force); err != nil {
		return nil, err
	}
	return LaunchDataView{
		Adopted:     true,
		Application: StepView{Name: app.Name, UUID: app.UUID},
	}, nil
}

func adoptView(uuid string) any {
	return LaunchDataView{Adopted: true, Application: StepView{UUID: uuid}}
}

// linkedUUID reads the recorded uuid for the environment, if any. Errors are
// swallowed deliberately: a missing or unreadable repo config simply means
// nothing is linked yet.
func linkedUUID(sess *Session, envName string) (string, bool) {
	if sess.Repo == nil {
		return "", false
	}
	env, ok := sess.Repo.EnvironmentFor(envName)
	if !ok || env.UUID == "" {
		return "", false
	}
	return env.UUID, true
}

func recordEnvironmentUUID(sess *Session, envName, appUUID string, force bool) error {
	root := sess.RepoRoot
	if root == "" {
		root = "."
	}
	repoPath, found, err := config.FindRepoConfig(root)
	if err != nil {
		return err
	}
	if !found {
		repoPath = filepath.Join(root, ".config", "berth.json")
	}
	cfg := config.RepoConfig{}
	if found {
		cfg, err = config.LoadRepoConfig(repoPath)
		if err != nil {
			return err
		}
	}
	if existing, ok := cfg.EnvironmentFor(envName); ok && existing.UUID != "" && existing.UUID != appUUID && !force {
		return output.Usage(fmt.Errorf("environment %q is already linked to application %s — pass --force to relink", envName, existing.UUID))
	}
	cfg.UpsertEnvironment(config.Environment{Name: envName, UUID: appUUID})
	return config.SaveRepoConfig(repoPath, cfg)
}

func stepView(s launch.Step) StepView {
	return StepView{Name: s.Name, UUID: s.UUID, Created: s.Created}
}

func gitRemote(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "remote", "get-url", "origin").Output()
	if err != nil {
		return "", errors.New("no git remote named origin — add one: git remote add origin <url>")
	}
	return strings.TrimSpace(string(out)), nil
}

func currentBranch(path string) (string, error) {
	out, err := exec.Command("git", "-C", path, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		return "", errors.New("cannot determine the current git branch")
	}
	return strings.TrimSpace(string(out)), nil
}

func resolveLaunchBranch(ctx *cli.CmdContext, term *input.Terminal, path string) (string, error) {
	head, _ := currentBranch(path)
	if !ctx.Interactive {
		if head != "" {
			return "", output.Usage(fmt.Errorf("missing --branch — current branch is %q; pass it explicitly", head))
		}
		return "", output.Usage(errors.New("missing --branch — the git branch to deploy"))
	}
	branch, err := term.PromptDefault("git branch?", defaultStr(head, "main"))
	if err != nil {
		return "", output.Usage(fmt.Errorf("--branch: %w", err))
	}
	return branch, nil
}

func resolveLaunchProject(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	projects, err := client.Projects(context.Background())
	if err != nil {
		return "", err
	}
	if len(projects) == 0 {
		if !ctx.Interactive {
			return "", output.Usage(errors.New("missing --project — no projects exist yet, pass the name to create"))
		}
		name, err := term.Prompt("new project name?")
		if err != nil || name == "" {
			return "", output.Usage(errors.New("missing --project — pass the project name to create"))
		}
		return name, nil
	}
	if !ctx.Interactive {
		return "", output.Usage(fmt.Errorf("missing --project — existing projects: %s", nameList(len(projects), func(i int) string { return projects[i].Name })))
	}
	opts := make([]string, 0, len(projects)+1)
	for _, p := range projects {
		opts = append(opts, p.Name)
	}
	opts = append(opts, "Create a new project…")
	idx, err := term.Select("project?", opts)
	if err != nil {
		return "", output.Usage(fmt.Errorf("--project: %w", err))
	}
	if idx == len(projects) {
		name, err := term.Prompt("new project name?")
		if err != nil || name == "" {
			return "", output.Usage(errors.New("missing --project — pass the project name to create"))
		}
		return name, nil
	}
	return projects[idx].Name, nil
}

func resolveLaunchServer(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	servers, err := client.Servers(context.Background())
	if err != nil {
		return "", err
	}
	if len(servers) == 1 {
		return servers[0].UUID, nil
	}
	if !ctx.Interactive {
		return "", output.Usage(fmt.Errorf("missing --server — servers: %s", nameList(len(servers), func(i int) string {
			return fmt.Sprintf("%s (%s)", servers[i].Name, servers[i].UUID)
		})))
	}
	opts := make([]string, 0, len(servers))
	for _, s := range servers {
		opts = append(opts, fmt.Sprintf("%s (%s)", s.Name, s.IP))
	}
	idx, err := term.Select("server?", opts)
	if err != nil {
		return "", output.Usage(fmt.Errorf("--server: %w", err))
	}
	return servers[idx].UUID, nil
}

func resolveLaunchDatabase(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, flagValue string) (string, error) {
	if flagValue != "" {
		if strings.EqualFold(flagValue, "none") {
			return "", nil
		}
		return flagValue, nil
	}
	dbs, err := client.Databases(context.Background())
	if err != nil {
		return "", err
	}
	if !ctx.Interactive {
		return "", output.Usage(fmt.Errorf("missing --database — pass a database uuid, or --database none (available: %s)", nameList(len(dbs), func(i int) string {
			return fmt.Sprintf("%s [%s]", dbs[i].Name, dbs[i].Type)
		})))
	}
	opts := make([]string, 0, len(dbs)+1)
	for _, d := range dbs {
		opts = append(opts, fmt.Sprintf("%s [%s]", d.Name, d.Type))
	}
	opts = append(opts, "None")
	idx, err := term.Select("database?", opts)
	if err != nil {
		return "", output.Usage(fmt.Errorf("--database: %w", err))
	}
	if idx == len(dbs) {
		return "", nil
	}
	return dbs[idx].UUID, nil
}

func resolveLaunchRedis(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, flagValue string, horizon bool) (string, error) {
	if !horizon {
		return "", nil
	}
	if flagValue != "" {
		return flagValue, nil
	}
	dbs, err := client.Databases(context.Background())
	if err != nil {
		return "", err
	}
	var redis []struct {
		UUID, Name string
	}
	for _, d := range dbs {
		if strings.Contains(d.Type, "redis") {
			redis = append(redis, struct {
				UUID, Name string
			}{d.UUID, d.Name})
		}
	}
	if !ctx.Interactive {
		return "", output.Usage(fmt.Errorf("missing --redis — the horizon stack needs a redis resource (available: %s)", nameList(len(redis), func(i int) string { return redis[i].Name })))
	}
	opts := make([]string, 0, len(redis))
	for _, r := range redis {
		opts = append(opts, r.Name)
	}
	idx, err := term.Select("redis (required by horizon)?", opts)
	if err != nil {
		return "", output.Usage(fmt.Errorf("--redis: %w", err))
	}
	return redis[idx].UUID, nil
}

// resolveLaunchKey asks or flags the deploy key decision: an explicit uuid is
// validated, an interactive terminal picks from the list or creates a new
// keypair, and a non-interactive run without --key is a usage error.
func resolveLaunchKey(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, flagValue, newName string) (launch.DeployKeyChoice, error) {
	if flagValue != "" {
		return launch.LookupDeployKey(context.Background(), client, flagValue)
	}
	keys, err := client.PrivateKeys(context.Background())
	if err != nil {
		return launch.DeployKeyChoice{}, err
	}
	if !ctx.Interactive {
		return launch.DeployKeyChoice{}, output.Usage(fmt.Errorf("missing --key — pass an existing deploy key uuid, or pick interactively to create one (available: %s)", nameList(len(keys), func(i int) string {
			return fmt.Sprintf("%s (%s)", keys[i].Name, keys[i].UUID)
		})))
	}
	opts := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		opts = append(opts, k.Name)
	}
	opts = append(opts, "Create a new deploy key…")
	idx, err := term.Select("deploy key?", opts)
	if err != nil {
		return launch.DeployKeyChoice{}, output.Usage(fmt.Errorf("--key: %w", err))
	}
	if idx < len(keys) {
		return launch.LookupDeployKey(context.Background(), client, keys[idx].UUID)
	}
	return launch.CreateDeployKey(context.Background(), client, newName)
}

// resolveLaunchDBName asks which logical database on the selected postgres
// to connect to. The resource's own database name is offered as a suggestion
// (accepting it is explicit); non-interactively the value is required.
func resolveLaunchDBName(ctx *cli.CmdContext, client launch.Client, term *input.Terminal, databaseUUID string) (string, error) {
	db, err := client.Database(context.Background(), databaseUUID)
	if err != nil {
		return "", err
	}
	if !ctx.Interactive {
		return "", output.Usage(fmt.Errorf("missing --db-name — the database to use on %s (currently %q)", db.Name, db.PostgresDB))
	}
	name, err := term.PromptDefault("database name?", db.PostgresDB)
	if err != nil {
		return "", output.Usage(fmt.Errorf("--db-name: %w", err))
	}
	if name == "" {
		return "", output.Usage(fmt.Errorf("missing --db-name — the database to use on %s", db.Name))
	}
	return name, nil
}

func resolveLaunchDomain(ctx *cli.CmdContext, term *input.Terminal) (string, error) {
	if !ctx.Interactive {
		return "", output.Usage(errors.New("missing --domain — the domain for the app service, e.g. http://app.example.com (the port comes from the compose file)"))
	}
	domain, err := term.Prompt("domain? (port is added automatically)")
	if err != nil || domain == "" {
		return "", output.Usage(errors.New("missing --domain — the domain for the app service"))
	}
	return domain, nil
}

func nameList(n int, at func(int) string) string {
	parts := make([]string, 0, n)
	for i := range n {
		parts = append(parts, at(i))
	}
	if n == 0 {
		return "(none)"
	}
	return strings.Join(parts, ", ")
}

func defaultStr(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
