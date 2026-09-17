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

	appPath, err := absAppDir(abs)
	if err != nil {
		return plan.Plan{}, err
	}

	plan := plan.Plan{}
	plan.Project.Path = appPath
	plan.Project.Name = filepath.Base(appPath)
	if appPath != abs {
		plan.Note(fmt.Sprintf("monorepo — repository root has no composer.json, using the app in %q", filepath.Base(appPath)))
	}
	resolveBaseDir(&plan, appPath)
	return scanApp(plan, appPath)
}

// absAppDir resolves the effective application directory for a given path. A
// path pointing at the Laravel app itself (composer.json present) is used as
// is. A path pointing at the git repository root is descended into the single
// Laravel app there, or fails when none or several are present. Anything else
// is returned unchanged so downstream validation reports the real problem.
func absAppDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if hasComposer(abs) || !isRepoRoot(abs) {
		return abs, nil
	}
	apps, err := laravelApps(abs)
	if err != nil {
		return "", err
	}
	switch len(apps) {
	case 1:
		return apps[0], nil
	case 0:
		return "", fmt.Errorf("%w — no composer.json for a laravel app found under the repository root %s", ErrInvalidFramework, abs)
	default:
		return "", fmt.Errorf("repository %s contains %d laravel apps (%s) — pass --path <app dir> for the one you want", abs, len(apps), strings.Join(apps, ", "))
	}
}

func scanApp(plan plan.Plan, appPath string) (plan.Plan, error) {
	composer, err := readComposer(appPath)
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
	if lockVersion, ok := phpVersionFromLock(filepath.Join(appPath, "composer.lock"), &plan); ok && versionGreater(lockVersion, plan.Project.PhpVersion) {
		plan.Note(fmt.Sprintf("php: raising to %s — composer.lock contains packages that require it", lockVersion))
		plan.Project.PhpVersion = lockVersion
	}
	if _, ok := composer.Require["laravel/horizon"]; ok {
		plan.Project.HasHorizon = true
		plan.Note("composer requires laravel/horizon -> queue workers run horizon (redis)")
	}

	packageJson, hasPackage := readPackage(appPath)
	if hasPackage {
		plan.Project.PackageManager = packageManager(appPath, &plan)
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

// resolveBaseDir computes the app's base directory relative to the git repo
// root and records it on the plan. No git repo is not an error: the app is
// treated as living at the repo root ("") so the rest of the flow is
// unchanged for non-monorepo, non-git layouts.
func resolveBaseDir(plan *plan.Plan, appPath string) {
	repoRoot, found := RepoRoot(appPath)
	if !found {
		plan.Note("no git repository found — assuming the app lives at the repository root")
		return
	}
	base, err := filepath.Rel(repoRoot, appPath)
	if err != nil || base == "." || base == "" {
		plan.Note("app is at the repository root (base directory /)")
		return
	}
	base = filepath.ToSlash(base)
	plan.Project.BaseDir = base
	plan.Note(fmt.Sprintf("monorepo — relative to the repository root the app is in %q with base directory /%s", base, base))
}

// RepoRoot walks up from start to the git repository root, identified by a
// .git entry (a directory in a normal clone, a file in a worktree). It
// returns the root and true, or false when the path is inside no git repo.
func RepoRoot(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func isRepoRoot(p string) bool {
	_, err := os.Stat(filepath.Join(p, ".git"))
	return err == nil
}

func hasComposer(p string) bool {
	_, err := os.Stat(filepath.Join(p, "composer.json"))
	return err == nil
}

// laravelApps lists the depth-1 subdirectories of root whose composer.json
// requires laravel/framework. It is the monorepo hint used when a user points
// --path at the repository root.
func laravelApps(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var apps []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		c, err := readComposer(dir)
		if err != nil {
			continue
		}
		if _, ok := c.Require["laravel/framework"]; ok {
			apps = append(apps, dir)
		}
	}
	return apps, nil
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
