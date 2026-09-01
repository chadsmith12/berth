package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/chadsmith12/berth/pkg/plan"
	"github.com/chadsmith12/berth/pkg/templates"
)

type Options struct {
	Force  bool
	DryRun bool
}

type FileResult struct {
	Path    string
	Skipped bool
	Reason  string
}

type Result struct {
	Files  []FileResult
	DryRun bool
}

func Generate(p plan.Plan, opts Options) (Result, error) {
	if p.Project.Workers < 0 {
		p.Project.Workers = 0
	}

	files := []struct {
		rel    string
		render func(plan.Plan) (string, error)
		mode   os.FileMode
	}{
		{"docker/app/Dockerfile", templates.RenderDockerfile, 0644},
		{"docker/app/entrypoint.sh", templates.RenderEntrypoint, 0755},
		{".dockerignore", templates.RenderDockerignore, 0644},
		{"docker-compose.yml", templates.RenderCompose, 0644},
	}

	var res Result
	res.DryRun = opts.DryRun

	for _, f := range files {
		content, err := f.render(p)
		if err != nil {
			return res, fmt.Errorf("render %s: %w", f.rel, err)
		}
		if !opts.DryRun && len(content) > 0 && content[len(content)-1] != '\n' {
			content += "\n"
		}

		abs := filepath.Join(p.Project.Path, f.rel)

		if _, err := os.Stat(abs); err == nil && !opts.Force {
			res.Files = append(res.Files, FileResult{Path: f.rel, Skipped: true, Reason: "exists (use --force to overwrite)"})
			continue
		}

		res.Files = append(res.Files, FileResult{Path: f.rel})

		if opts.DryRun {
			continue
		}

		if err := os.MkdirAll(filepath.Dir(abs), 0755); err != nil {
			return res, fmt.Errorf("mkdir %s: %w", filepath.Dir(abs), err)
		}
		if err := os.WriteFile(abs, []byte(content), f.mode); err != nil {
			return res, fmt.Errorf("write %s: %w", f.rel, err)
		}
		if f.mode&0111 != 0 {
			_ = os.Chmod(abs, f.mode)
		}
	}

	return res, nil
}
