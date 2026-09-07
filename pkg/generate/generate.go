package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chadsmith12/berth/pkg/plan"
	"github.com/chadsmith12/berth/pkg/templates"
)

// File statuses reported for each generated artifact.
const (
	StatusWritten     = "written"
	StatusUnchanged   = "unchanged"
	StatusChanged     = "changed"
	StatusWouldWrite  = "would-write"
	StatusWouldChange = "would-change"
)

type Options struct {
	Env    string
	Force  bool
	DryRun bool
}

type FileResult struct {
	Path   string
	Status string
	Diff   string
	Reason string
}

type Result struct {
	Files  []FileResult
	DryRun bool
}

func Generate(p plan.Plan, opts Options) (Result, error) {
	var res Result
	res.DryRun = opts.DryRun

	if p.Project.Port <= 0 || p.Project.Port > 65535 {
		return res, fmt.Errorf("application port must be set (1-65535) — pass --port")
	}
	if !p.Project.HasHorizon && p.Project.Workers < 0 {
		return res, fmt.Errorf("worker count must be set (0 or more) — pass --workers")
	}
	if p.Project.HasSsr && p.Project.SsrScript == "" {
		return res, fmt.Errorf("ssr build script must be set when ssr is enabled")
	}
	env := opts.Env
	if env == "" {
		env = "production"
	}
	p.Project.Env = env

	files := []struct {
		rel    string
		render func(plan.Plan) (string, error)
		mode   os.FileMode
	}{
		{"docker/app/Dockerfile", templates.RenderDockerfile, 0644},
		{"docker/app/entrypoint.sh", templates.RenderEntrypoint, 0755},
		{".dockerignore", templates.RenderDockerignore, 0644},
		{"docker-compose." + env + ".yml", templates.RenderCompose, 0644},
	}

	for _, f := range files {
		content, err := f.render(p)
		if err != nil {
			return res, fmt.Errorf("render %s: %w", f.rel, err)
		}
		if len(content) > 0 && content[len(content)-1] != '\n' {
			content += "\n"
		}
		abs := filepath.Join(p.Project.Path, f.rel)

		existing, rerr := os.ReadFile(abs)
		switch {
		case os.IsNotExist(rerr):
			if opts.DryRun {
				res.Files = append(res.Files, FileResult{Path: f.rel, Status: StatusWouldWrite})
				continue
			}
			if err := writeFile(abs, content, f.mode); err != nil {
				return res, err
			}
			res.Files = append(res.Files, FileResult{Path: f.rel, Status: StatusWritten})
		case rerr != nil:
			return res, fmt.Errorf("read %s: %w", f.rel, rerr)
		default:
			before := string(existing)
			if before == content {
				res.Files = append(res.Files, FileResult{Path: f.rel, Status: StatusUnchanged})
				continue
			}
			diff := unifiedDiff(f.rel, before, content)
			switch {
			case opts.DryRun:
				res.Files = append(res.Files, FileResult{Path: f.rel, Status: StatusWouldChange, Diff: diff})
			case !opts.Force:
				res.Files = append(res.Files, FileResult{
					Path:   f.rel,
					Status: StatusChanged,
					Diff:   diff,
					Reason: "refusing to overwrite without --force",
				})
			default:
				if err := writeFile(abs, content, f.mode); err != nil {
					return res, err
				}
				res.Files = append(res.Files, FileResult{Path: f.rel, Status: StatusWritten, Diff: diff})
			}
		}
	}

	return res, nil
}

func writeFile(abs, content string, mode os.FileMode) error {
	dir := filepath.Dir(abs)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(abs, []byte(content), mode); err != nil {
		return fmt.Errorf("write %s: %w", abs, err)
	}
	if mode&0111 != 0 {
		_ = os.Chmod(abs, mode)
	}
	return nil
}
