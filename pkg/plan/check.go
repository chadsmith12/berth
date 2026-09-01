package plan

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

type Result struct {
	Name     string
	Severity Severity
	OK       bool
	Detail   string
	Fixable  bool
	Fix      func() error
}

func (r Result) CanFix() bool {
	return !r.OK && r.Fixable && r.Fix != nil
}

type Report struct {
	Results []Result
}

func (p *Plan) Check() Report {
	return Report{
		Results: []Result{
			p.checkSsr(),
		},
	}
}

func (r Report) HasBlockingFailure() bool {
	for _, res := range r.Results {
		if !res.OK && res.Severity == SeverityError {
			return true
		}
	}
	return false
}

func (r Report) HasFixable() bool {
	for _, res := range r.Results {
		if !res.OK && res.Fixable {
			return true
		}
	}
	return false
}

func (p *Plan) checkSsr() Result {
	const name = "SSR Config"

	if !p.Project.HasSsr {
		return Result{
			Name:     name,
			Severity: SeverityInfo,
			OK:       true,
			Detail:   "SSR not enabled, skipping",
		}
	}

	configPath := filepath.Join(p.Project.Path, "config", "inertia.php")
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Result{
				Name:     name,
				Severity: SeverityError,
				OK:       false,
				Detail:   "SSR enabled but config/inertia.php not found",
			}
		}
		return Result{
			Name:     name,
			Severity: SeverityError,
			OK:       false,
			Detail:   fmt.Sprintf("failed to read config/inertia.php: %v", err),
		}
	}

	content := string(data)

	envRe := regexp.MustCompile(`['"]url['"]\s*=>\s*env\(\s*['"]INERTIA_SSR_URL['"]`)
	if envRe.MatchString(content) {
		return Result{
			Name:     name,
			Severity: SeverityInfo,
			OK:       true,
			Detail:   "SSR url correctly uses env('INERTIA_SSR_URL')",
		}
	}

	if strings.Contains(content, "INERTIA_SSR_URL") {
		return Result{
			Name:     name,
			Severity: SeverityWarning,
			OK:       false,
			Detail:   "config/inertia.php references INERTIA_SSR_URL but not in expected env('INERTIA_SSR_URL', ...) form for ssr.url",
		}
	}

	hardcodedRe := regexp.MustCompile(`['"]url['"]\s*=>\s*['"][^'"]+['"]`)
	if !hardcodedRe.MatchString(content) {
		return Result{
			Name:     name,
			Severity: SeverityWarning,
			OK:       false,
			Detail:   "SSR enabled but ssr.url not found in config/inertia.php; ensure config/inertia.php sets ssr.url to env('INERTIA_SSR_URL', 'http://127.0.0.1:13714')",
		}
	}

	r := Result{
		Name:     name,
		Severity: SeverityError,
		OK:       false,
		Detail:   "SSR url is hardcoded in config/inertia.php; should use env('INERTIA_SSR_URL', 'http://127.0.0.1:13714') to avoid silent deployment failures",
		Fixable:  true,
		Fix:      ssrFix(configPath),
	}
	return r
}

func ssrFix(configPath string) func() error {
	return func() error {
		b, err := os.ReadFile(configPath)
		if err != nil {
			return err
		}
		c := string(b)
		re := regexp.MustCompile(`['"]url['"]\s*=>\s*['"]([^'"]+)['"]`)
		if !re.MatchString(c) {
			return fmt.Errorf("could not find hardcoded ssr.url to fix")
		}
		fixed := re.ReplaceAllStringFunc(c, func(m string) string {
			sub := re.FindStringSubmatch(m)
			if len(sub) != 2 {
				return m
			}
			return fmt.Sprintf("'url' => env('INERTIA_SSR_URL', '%s')", sub[1])
		})
		if fixed == c {
			return fmt.Errorf("no changes made")
		}
		return os.WriteFile(configPath, []byte(fixed), 0644)
	}
}
