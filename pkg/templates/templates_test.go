package templates_test

import (
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/plan"
	"github.com/chadsmith12/berth/pkg/templates"
)

// TestDockerfileNoVariableFrom guards against BuildKit's rule that COPY
// --from cannot expand variables — the failure that surfaced in a real
// deployment ("variable expansion is not supported for --from").
func TestDockerfileNoVariableFrom(t *testing.T) {
	projects := []plan.Project{
		{PackageManager: plan.PM_NPM, HasSsr: true, SsrScript: "build:ssr", NodeVersion: "22", PhpVersion: "8.4"},
		{PackageManager: plan.PM_BUN, HasSsr: true, SsrScript: "build:ssr", NodeVersion: "22", PhpVersion: "8.4"},
		{PackageManager: plan.PM_NPM, HasWayFinder: true, NodeVersion: "20", PhpVersion: "8.3"},
		{}, // php-only
	}
	for i, p := range projects {
		out, err := templates.RenderDockerfile(plan.Plan{Project: p})
		if err != nil {
			t.Fatalf("case %d: render: %v", i, err)
		}
		for n, line := range strings.Split(out, "\n") {
			if idx := strings.Index(line, "--from="); idx >= 0 && strings.Contains(line[idx:], "${") {
				t.Errorf("case %d line %d: variable expansion in --from is not supported by BuildKit:\n%s", i, n+1, line)
			}
		}
	}
}
