package commands

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/chadsmith12/berth/pkg/cli"
	"github.com/chadsmith12/berth/pkg/detect"
	"github.com/chadsmith12/berth/pkg/generate"
	"github.com/chadsmith12/berth/pkg/input"
	"github.com/chadsmith12/berth/pkg/output"
	"github.com/chadsmith12/berth/pkg/plan"
)

type ProjectView struct {
	Name           string `json:"name"`
	BaseDir        string `json:"base_dir,omitempty"`
	PhpVersion     string `json:"php_version"`
	NodeVersion    string `json:"node_version"`
	PackageManager string `json:"package_manager"`
	HasWayFinder   bool   `json:"wayfinder"`
	HasSsr         bool   `json:"ssr"`
	SsrScript      string `json:"ssr_script,omitempty"`
	HasHorizon     bool   `json:"horizon"`
	Workers        int    `json:"workers"`
	Scheduler      bool   `json:"scheduler"`
	Port           int    `json:"port"`
	Env            string `json:"env"`
}

type ResultView struct {
	Name     string `json:"name"`
	Severity string `json:"severity"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail"`
	Fixable  bool   `json:"fixable"`
}

type ReportView struct {
	Results []ResultView `json:"results"`
}

type FixView struct {
	Name    string `json:"name"`
	Applied bool   `json:"applied"`
	DryRun  bool   `json:"dry_run,omitempty"`
	Error   string `json:"error,omitempty"`
}

type FileView struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Diff   string `json:"diff,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type GenerateDataView struct {
	Verbose bool `json:"-"`
	DryRun  bool `json:"dry_run,omitempty"`

	Project ProjectView `json:"project"`
	Notes   []string    `json:"notes,omitempty"`
	Report  ReportView  `json:"report"`
	Fixes   []FixView   `json:"fixes,omitempty"`
	Files   []FileView  `json:"files,omitempty"`

	Hint string `json:"-"`
}

type generateInput struct {
	path         string
	workers      int
	scheduler    bool
	schedulerSet bool
	port         int
	verbose      bool
	fix          bool
	dryRun       bool
	force        bool
}

func NewGenerateCommand() *cli.Command {
	cmd := cli.NewCommand("generate", "scaffold the deployment files")
	cmd.Long = `Detect the project's facts, ask for the decisions detection cannot
infer, check the project's prerequisites, and write the deployment files.

Stateless: reads no configuration and contacts no network. --env names the
compose file, so each environment gets its own service topology.`
	fs := flag.NewFlagSet("generate", flag.ContinueOnError)
	path := fs.String("path", ".", "the path to the project directory")
	workers := fs.Int("workers", -1, "number of queue workers (0 = none; ignored when horizon is detected)")
	scheduler := fs.Bool("scheduler", false, "run a scheduler container (asked when omitted)")
	port := fs.Int("port", 0, "the port the app container serves on, e.g. 8080 (asked when omitted)")
	verbose := fs.Bool("v", false, "shows the detection reasoning")
	fix := fs.Bool("fix", false, "applies the fixable issues")
	dryRun := fs.Bool("dry-run", false, "preview fixes, diffs and writes without touching disk")
	force := fs.Bool("force", false, "overwrite existing generated files that differ")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		in := generateInput{
			path: *path, workers: *workers, scheduler: *scheduler, port: *port,
			verbose: *verbose, fix: *fix, dryRun: *dryRun, force: *force,
		}
		fs.Visit(func(f *flag.Flag) {
			if f.Name == "scheduler" {
				in.schedulerSet = true
			}
		})
		data, err := runGenerate(in, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runGenerate(in generateInput, ctx *cli.CmdContext) (any, error) {
	detected, err := detect.Scan(in.path)
	if err != nil {
		return nil, err
	}

	env := ctx.Globals.Env
	if env == "" {
		env = "production"
	}
	detected.Project.Env = env

	if detected.Project.HasHorizon {
		if in.workers > 0 {
			detected.Note("--workers ignored: horizon supervises its own queue workers")
		}
		detected.Project.Workers = 0
	} else {
		workers, err := resolveWorkers(ctx, in.workers)
		if err != nil {
			return nil, err
		}
		detected.Project.Workers = workers
	}

	scheduler, err := resolveScheduler(ctx, in.scheduler, in.schedulerSet)
	if err != nil {
		return nil, err
	}
	detected.Project.Scheduler = scheduler

	port, err := resolvePort(ctx, in.port)
	if err != nil {
		return nil, err
	}
	detected.Project.Port = port

	report := detected.Check()
	data := newGenerateDataView(detected, report, in.verbose)
	data.DryRun = in.dryRun

	if in.fix {
		data.Fixes = applyFixes(report.Results, in.dryRun)
		if anyApplied(data.Fixes) && !in.dryRun {
			report = detected.Check()
			data.Report = newReportView(report)
		}
	}

	if report.HasBlockingFailure() {
		if !in.fix && report.HasFixable() {
			data.Hint = "run with --fix to automatically fix fixable issues"
		}
		return data, output.Preflight(fmt.Errorf("checks failed: %s", strings.Join(failingNames(report), ", ")))
	}

	res, err := generate.Generate(detected, generate.Options{Env: env, Force: in.force, DryRun: in.dryRun})
	if err != nil {
		return data, err
	}
	data.Files = newFileViews(res)
	return data, nil
}

func (v GenerateDataView) WriteText(w io.Writer) {
	writeProject(w, v.Project)
	if v.Verbose {
		writeNotes(w, v.Notes)
	}
	writeReport(w, v.Report)
	writeFixes(w, v.Fixes)
	if v.Hint != "" {
		fmt.Fprintf(w, "\n%s\n", v.Hint)
	}
	writeFiles(w, v)
}

func writeProject(w io.Writer, p ProjectView) {
	fmt.Fprintf(w, "%-16s %s\n", "project", p.Name)
	if p.BaseDir != "" {
		fmt.Fprintf(w, "%-16s /%s\n", "base dir", p.BaseDir)
	} else {
		fmt.Fprintf(w, "%-16s /\n", "base dir")
	}
	fmt.Fprintf(w, "%-16s %s\n", "env", p.Env)
	fmt.Fprintf(w, "%-16s %s\n", "php", p.PhpVersion)
	fmt.Fprintf(w, "%-16s %s\n", "node", p.NodeVersion)
	fmt.Fprintf(w, "%-16s %s\n", "package manager", p.PackageManager)
	fmt.Fprintf(w, "%-16s %v\n", "ssr", p.HasSsr)
	fmt.Fprintf(w, "%-16s %v\n", "wayfinder", p.HasWayFinder)
	fmt.Fprintf(w, "%-16s %v\n", "horizon", p.HasHorizon)
	fmt.Fprintf(w, "%-16s %d\n", "workers", p.Workers)
	fmt.Fprintf(w, "%-16s %v\n", "scheduler", p.Scheduler)
	fmt.Fprintf(w, "%-16s %d\n", "port", p.Port)
}

func writeNotes(w io.Writer, notes []string) {
	fmt.Fprintln(w, "\nreasoning:")
	for _, n := range notes {
		fmt.Fprintf(w, "  - %s\n", n)
	}
}

func writeReport(w io.Writer, report ReportView) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "checks:")
	for _, r := range report.Results {
		status := "OK"
		if !r.OK {
			status = "FAIL"
		}
		fixable := ""
		if r.Fixable {
			fixable = " [fixable]"
		}
		fmt.Fprintf(w, "  [%s] %s (%s)%s: %s\n", status, r.Name, r.Severity, fixable, r.Detail)
	}
}

func writeFixes(w io.Writer, fixes []FixView) {
	if len(fixes) == 0 {
		return
	}
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "fixes:")
	for _, f := range fixes {
		switch {
		case f.DryRun:
			fmt.Fprintf(w, "  [would fix] %s\n", f.Name)
		case f.Error != "":
			fmt.Fprintf(w, "  [failed] %s: %s\n", f.Name, f.Error)
		default:
			fmt.Fprintf(w, "  [fixed] %s\n", f.Name)
		}
	}
}

func writeFiles(w io.Writer, v GenerateDataView) {
	if len(v.Files) == 0 {
		return
	}
	fmt.Fprintln(w, "")
	if v.DryRun {
		fmt.Fprintln(w, "generate (dry-run):")
	} else {
		fmt.Fprintln(w, "generate:")
	}
	for _, f := range v.Files {
		fmt.Fprintf(w, "  [%s] %s\n", f.Status, f.Path)
		if f.Reason != "" {
			fmt.Fprintf(w, "        %s\n", f.Reason)
		}
		if f.Diff != "" {
			for _, line := range strings.Split(strings.TrimSuffix(f.Diff, "\n"), "\n") {
				fmt.Fprintf(w, "  %s\n", line)
			}
		}
	}
}

func newGenerateDataView(p plan.Plan, r plan.Report, verbose bool) GenerateDataView {
	return GenerateDataView{
		Verbose: verbose,
		Project: newProjectView(p.Project),
		Notes:   p.Notes,
		Report:  newReportView(r),
	}
}

func newProjectView(p plan.Project) ProjectView {
	return ProjectView{
		Name:           p.Name,
		BaseDir:        p.BaseDir,
		PhpVersion:     p.PhpVersion,
		NodeVersion:    p.NodeVersion,
		PackageManager: string(p.PackageManager),
		HasWayFinder:   p.HasWayFinder,
		HasSsr:         p.HasSsr,
		SsrScript:      p.SsrScript,
		HasHorizon:     p.HasHorizon,
		Workers:        p.Workers,
		Scheduler:      p.Scheduler,
		Port:           p.Port,
		Env:            p.Env,
	}
}

func newReportView(r plan.Report) ReportView {
	v := ReportView{
		Results: make([]ResultView, len(r.Results)),
	}
	for i, res := range r.Results {
		v.Results[i] = ResultView{
			Name:     res.Name,
			Severity: string(res.Severity),
			OK:       res.OK,
			Detail:   res.Detail,
			Fixable:  res.Fixable,
		}
	}
	return v
}

func newFileViews(res generate.Result) []FileView {
	views := make([]FileView, 0, len(res.Files))
	for _, f := range res.Files {
		views = append(views, FileView{
			Path:   f.Path,
			Status: f.Status,
			Diff:   f.Diff,
			Reason: f.Reason,
		})
	}
	return views
}

func applyFixes(results []plan.Result, dryRun bool) []FixView {
	fixes := []FixView{}
	for _, r := range results {
		if !r.CanFix() {
			continue
		}
		fix := FixView{Name: r.Name, DryRun: dryRun}
		if dryRun {
			fixes = append(fixes, fix)
			continue
		}
		if err := r.Fix(); err != nil {
			fix.Error = err.Error()
			fixes = append(fixes, fix)
			continue
		}
		fix.Applied = true
		fixes = append(fixes, fix)
	}
	return fixes
}

func anyApplied(fixes []FixView) bool {
	for _, f := range fixes {
		if f.Applied {
			return true
		}
	}
	return false
}

func failingNames(r plan.Report) []string {
	var names []string
	for _, res := range r.Results {
		if !res.OK && res.Severity == plan.SeverityError {
			names = append(names, res.Name)
		}
	}
	return names
}

func resolveWorkers(ctx *cli.CmdContext, flag int) (int, error) {
	if flag >= 0 {
		return flag, nil
	}
	if !ctx.Interactive {
		return 0, output.Usage(errors.New("missing --workers — number of queue workers (0 = none)"))
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)
	n, err := term.PromptInt("workers [0-5] (0 = none)?", nonNegative)
	if err != nil {
		return 0, output.Usage(fmt.Errorf("--workers: %w", err))
	}
	return n, nil
}

func resolveScheduler(ctx *cli.CmdContext, flagValue bool, flagSet bool) (bool, error) {
	if flagSet {
		return flagValue, nil
	}
	if !ctx.Interactive {
		return false, output.Usage(errors.New("missing --scheduler — pass --scheduler or --scheduler=false"))
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)
	b, err := term.PromptYesNo("run a scheduler container?", true)
	if err != nil {
		return false, output.Usage(fmt.Errorf("--scheduler: %w", err))
	}
	return b, nil
}

func resolvePort(ctx *cli.CmdContext, flagValue int) (int, error) {
	if flagValue > 0 {
		if err := validPort(flagValue); err != nil {
			return 0, output.Usage(fmt.Errorf("--port: %w", err))
		}
		return flagValue, nil
	}
	if flagValue < 0 {
		return 0, output.Usage(fmt.Errorf("--port: %d is not a valid port", flagValue))
	}
	if !ctx.Interactive {
		return 0, output.Usage(errors.New("missing --port — the port the app container serves on (e.g. --port 8080)"))
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)
	n, err := term.PromptDefaultInt("application port?", 8080, func(n int) error { return validPort(n) })
	if err != nil {
		return 0, output.Usage(fmt.Errorf("--port: %w", err))
	}
	return n, nil
}

func validPort(n int) error {
	if n < 1 || n > 65535 {
		return errors.New("must be between 1 and 65535")
	}
	return nil
}

func nonNegative(n int) error {
	if n < 0 {
		return errors.New("must be 0 or greater")
	}
	return nil
}
