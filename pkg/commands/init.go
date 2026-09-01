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
	PhpVersion     string `json:"php_version"`
	NodeVersion    string `json:"node_version"`
	PackageManager string `json:"package_manager"`
	HasWayFinder   bool   `json:"wayfinder"`
	HasSsr         bool   `json:"ssr"`
	Workers        int    `json:"workers"`
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
	Path    string `json:"path"`
	Wrote   bool   `json:"wrote"`
	Skipped bool   `json:"skipped"`
	Reason  string `json:"reason,omitempty"`
}

type InitDataView struct {
	Verbose bool `json:"-"`
	DryRun  bool `json:"dry_run,omitempty"`

	Project ProjectView `json:"project"`
	Notes   []string    `json:"notes,omitempty"`
	Report  ReportView  `json:"report"`
	Fixes   []FixView   `json:"fixes,omitempty"`
	Files   []FileView  `json:"files,omitempty"`

	Hint string `json:"-"`
}

func NewInitCommand() *cli.Command {
	cmd := cli.NewCommand("init", "scaffold files + berth.json")
	cmd.Long = "Detect project type and generate Dockerfiles and related files"
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("path", ".", "the path to the project directory")
	workers := fs.Int("workers", -1, "number of queue workers (0 = none)")
	verbose := fs.Bool("v", false, "shows the detection reasoning")
	fix := fs.Bool("fix", false, "applies the fixable issues")
	dryRun := fs.Bool("dry-run", false, "dry run (no changes)")
	force := fs.Bool("force", false, "overwrite existing generated files")
	cmd.Flags = fs
	cmd.Run = func(ctx *cli.CmdContext, args []string) int {
		data, err := runInit(*path, *workers, *verbose, *fix, *dryRun, *force, ctx)
		return output.Emit(ctx, data, err)
	}
	return cmd
}

func runInit(path string, workersFlag int, verbose, fix, dryRun, force bool, ctx *cli.CmdContext) (any, error) {
	detected, err := detect.Scan(path)
	if err != nil {
		return nil, err
	}
	workers, err := resolveWorkers(ctx, workersFlag)
	if err != nil {
		return nil, err
	}
	detected.Project.Workers = workers

	report := detected.Check()
	data := newInitDataView(detected, report, verbose)
	data.DryRun = dryRun

	if fix {
		data.Fixes = applyFixes(report.Results, dryRun)
		if anyApplied(data.Fixes) && !dryRun {
			report = detected.Check()
			data.Report = newReportView(report)
		}
	}

	if report.HasBlockingFailure() {
		if !fix && report.HasFixable() {
			data.Hint = "run with --fix to automatically fix fixable issues"
		}
		return data, output.Preflight(fmt.Errorf("checks failed: %s", strings.Join(failingNames(report), ", ")))
	}

	res, err := generate.Generate(detected, generate.Options{Force: force, DryRun: dryRun})
	if err != nil {
		return data, err
	}
	data.Files = newFileViews(res)
	return data, nil
}

func (v InitDataView) WriteText(w io.Writer) {
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
	fmt.Fprintf(w, "%-16s %s\n", "php", p.PhpVersion)
	fmt.Fprintf(w, "%-16s %s\n", "node", p.NodeVersion)
	fmt.Fprintf(w, "%-16s %s\n", "package manager", p.PackageManager)
	fmt.Fprintf(w, "%-16s %v\n", "ssr", p.HasSsr)
	fmt.Fprintf(w, "%-16s %v\n", "wayfinder", p.HasWayFinder)
	fmt.Fprintf(w, "%-16s %d\n", "workers", p.Workers)
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

func writeFiles(w io.Writer, v InitDataView) {
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
		if f.Skipped {
			fmt.Fprintf(w, "  [skip] %s: %s\n", f.Path, f.Reason)
			continue
		}
		status := "wrote"
		if v.DryRun {
			status = "would write"
		}
		fmt.Fprintf(w, "  [%s] %s\n", status, f.Path)
	}
}

func newInitDataView(p plan.Plan, r plan.Report, verbose bool) InitDataView {
	return InitDataView{
		Verbose: verbose,
		Project: newProjectView(p.Project),
		Notes:   p.Notes,
		Report:  newReportView(r),
	}
}

func newProjectView(p plan.Project) ProjectView {
	return ProjectView{
		Name:           p.Name,
		PhpVersion:     p.PhpVersion,
		NodeVersion:    p.NodeVersion,
		PackageManager: string(p.PackageManager),
		HasWayFinder:   p.HasWayFinder,
		HasSsr:         p.HasSsr,
		Workers:        p.Workers,
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
			Path:    f.Path,
			Wrote:   !f.Skipped && !res.DryRun,
			Skipped: f.Skipped,
			Reason:  f.Reason,
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
	if !ctx.Interactive || ctx.Globals.Yes {
		return 0, output.Usage(errors.New("missing --workers (required in non-interactive mode)"))
	}
	term := input.New(ctx.Stdin, ctx.Stderr, ctx.Interactive)
	n, err := term.PromptInt("workers [0-5] (0 = none) ?", nonNegative)
	if err != nil {
		return 0, output.Usage(fmt.Errorf("--workers: %w", err))
	}
	return n, nil
}

func nonNegative(n int) error {
	if n < 0 {
		return errors.New("must be 0 or greater")
	}
	return nil
}
