package detect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/chadsmith12/berth/pkg/plan"
)

var ErrInvalidFramework = errors.New("invalid project detected (no laravel/framework in composer.json)")

type composerJson struct {
	Require map[string]string `json:"require"`
}

type packageJson struct {
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	Engines         map[string]string `json:"engines"`
	Scripts         map[string]string `json:"scripts"`
}

func (p *packageJson) hasDependency(dependency string) bool {
	if _, ok := p.Dependencies[dependency]; ok {
		return true
	}
	if _, ok := p.DevDependencies[dependency]; ok {
		return true
	}

	return false
}

func (p *packageJson) ssr() (string, bool) {
	for name, script := range p.Scripts {
		if strings.Contains(script, "--ssr") {
			return name, true
		}
	}

	return "", false
}

func Scan(path string) (plan.Plan, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return plan.Plan{}, err
	}

	plan := plan.Plan{}
	plan.Project.Path = abs
	plan.Project.Name = filepath.Base(abs)

	composer, err := readComposer(path)
	if err != nil {
		return plan, err
	}
	if _, ok := composer.Require["laravel/framework"]; !ok {
		return plan, ErrInvalidFramework
	}
	if _, ok := composer.Require["php"]; ok {
		if err := readPhpVersion(composer.Require["php"], &plan); err != nil {
			return plan, err
		}
	}
	// The composer.json floor is not enough: a lock resolved on a newer PHP
	// can contain packages whose own requirements exceed it. The lock is the
	// source of truth for reproducible installs, so it raises the version.
	if lockVersion, ok := phpVersionFromLock(filepath.Join(path, "composer.lock"), &plan); ok && versionGreater(lockVersion, plan.Project.PhpVersion) {
		plan.Note(fmt.Sprintf("php: raising to %s — composer.lock contains packages that require it", lockVersion))
		plan.Project.PhpVersion = lockVersion
	}
	if _, ok := composer.Require["laravel/horizon"]; ok {
		plan.Project.HasHorizon = true
		plan.Note("composer requires laravel/horizon -> queue workers run horizon (redis)")
	}

	packageJson, hasPackage := readPackage(path)
	if hasPackage {
		plan.Project.PackageManager = packageManager(path, &plan)
		plan.Project.HasWayFinder = packageJson.hasDependency("@laravel/vite-plugin-wayfinder")
		plan.Project.NodeVersion = nodeVersion(packageJson, &plan)
		ssr, ok := packageJson.ssr()
		if ok {
			plan.Note(fmt.Sprintf("found %s script -> SSR Enabled", ssr))
			plan.Project.HasSsr = true
			plan.Project.SsrScript = ssr
		}
	} else {
		plan.Project.NodeVersion = defaultNodeVersion
		plan.Note(fmt.Sprintf("node: no package.json engines.node -> default %s", defaultNodeVersion))
	}

	if plan.Project.PhpVersion == "" {
		plan.Project.PhpVersion = defaultPhpVersion
		plan.Note(fmt.Sprintf("php: no constraint in composer.json -> default %s", defaultPhpVersion))
	}

	return plan, nil
}

func readComposer(path string) (composerJson, error) {
	var c composerJson
	fullPath := filepath.Join(path, "composer.json")
	file, err := os.ReadFile(fullPath)
	if err != nil {
		return c, err
	}

	if err := json.Unmarshal(file, &c); err != nil {
		return c, err
	}
	return c, nil
}

type composerLock struct {
	Platform struct {
		Php string `json:"php"`
	} `json:"platform"`
	Packages []struct {
		Name    string            `json:"name"`
		Require map[string]string `json:"require"`
	} `json:"packages"`
}

// phpVersionFromLock derives the minimum PHP that can install the lock:
// the highest lower bound among the locked packages' own php requirements
// and the platform constraint. Dev packages are skipped — composer install
// --no-dev removes them.
func phpVersionFromLock(path string, plan *plan.Plan) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	var lock composerLock
	if err := json.Unmarshal(data, &lock); err != nil {
		plan.Note(fmt.Sprintf("php: composer.lock unparseable (%v) -> ignoring", err))
		return "", false
	}

	best := ""
	consider := func(constraint string) {
		if v := minSatisfyingPhp(constraint); versionGreater(v, best) {
			best = v
		}
	}
	if lock.Platform.Php != "" {
		consider(lock.Platform.Php)
	}
	for _, p := range lock.Packages {
		if c, ok := p.Require["php"]; ok {
			consider(c)
		}
	}
	if best == "" {
		return "", false
	}
	plan.Note(fmt.Sprintf("php: composer.lock requires at least %s", best))
	return best, true
}

// minSatisfyingPhp returns the lowest X.Y version that satisfies a composer
// constraint. Alternatives ("a || b") take the lowest branch; conjunctions
// ("a b") take the highest; upper bounds ("<x") are ignored.
func minSatisfyingPhp(constraint string) string {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" || constraint == "*" {
		return ""
	}
	best := ""
	for _, branch := range strings.FieldsFunc(constraint, func(r rune) bool { return r == '|' }) {
		branchMin := ""
		for _, tok := range strings.FieldsFunc(branch, func(r rune) bool { return r == ' ' || r == ',' }) {
			tok = strings.TrimSpace(tok)
			if tok == "" || strings.HasPrefix(tok, "<") || strings.HasPrefix(tok, "!") {
				continue
			}
			if v := lowerBound(tok); versionGreater(v, branchMin) {
				branchMin = v
			}
		}
		if branchMin == "" {
			continue
		}
		if best == "" || versionLess(branchMin, best) {
			best = branchMin
		}
	}
	return best
}

func lowerBound(tok string) string {
	m := regexp.MustCompile(`^[\^~>=]*v?(\d+)\.(\d+)`).FindStringSubmatch(strings.TrimSpace(tok))
	if m == nil {
		return ""
	}
	return m[1] + "." + m[2]
}

// versionGreater compares X.Y versions numerically; "" loses to everything.
func versionGreater(a, b string) bool {
	if a == "" {
		return false
	}
	if b == "" {
		return true
	}
	am, aj, _ := strings.Cut(a, ".")
	bm, bj, _ := strings.Cut(b, ".")
	ami, _ := strconv.Atoi(am)
	ami2, _ := strconv.Atoi(aj)
	bmi, _ := strconv.Atoi(bm)
	bmi2, _ := strconv.Atoi(bj)
	if ami != bmi {
		return ami > bmi
	}
	return ami2 > bmi2
}

func versionLess(a, b string) bool {
	return a != b && !versionGreater(a, b)
}

func readPackage(path string) (packageJson, bool) {
	var p packageJson
	fullPath := filepath.Join(path, "package.json")
	file, err := os.ReadFile(fullPath)
	if err != nil {
		return p, false
	}
	if err := json.Unmarshal(file, &p); err != nil {
		return p, false
	}
	return p, true
}

func packageManager(path string, p *plan.Plan) plan.PackageManager {
	for _, pm := range plan.SupportedPackageManagers {
		if _, ok := os.Stat(filepath.Join(path, pm.File)); ok == nil {
			p.Note(fmt.Sprintf("package manager: found %s -> %s", pm.File, pm.Pm))
			return pm.Pm
		}
	}

	p.Note("package manager: no lock file found. Builds will not be reproducible. Assuming npm")
	return plan.PM_NPM
}

func readPhpVersion(constraint string, plan *plan.Plan) error {
	re := regexp.MustCompile(`(\d+)\.(\d+)`)
	match := re.FindStringSubmatch(constraint)
	if len(match) != 3 {
		return errors.New("invalid php version constraint")
	}
	version := match[1] + "." + match[2]
	note := fmt.Sprintf("php: %q in composer.json - using php version: %s", constraint, version)
	plan.Note(note)
	plan.Project.PhpVersion = version
	return nil
}

const defaultPhpVersion = "8.4"
const defaultNodeVersion = "22"

func nodeVersion(p packageJson, plan *plan.Plan) string {
	if p.Engines != nil {
		if constraint, ok := p.Engines["node"]; ok {
			re := regexp.MustCompile(`(\d+)\.(\d+)|(\d+)`)
			m := re.FindStringSubmatch(constraint)
			if len(m) > 0 {
				ver := m[0]
				if strings.Count(ver, ".") > 0 {
					parts := strings.Split(ver, ".")
					ver = parts[0]
				}
				plan.Note(fmt.Sprintf("node: %q in package.json engines -> %s", constraint, ver))
				return ver
			}
		}
	}
	plan.Note(fmt.Sprintf("node: no engines.node constraint -> default %s", defaultNodeVersion))
	return defaultNodeVersion
}
