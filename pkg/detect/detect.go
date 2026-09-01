package detect

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

	packageJson, hasPackage := readPackage(path)
	if hasPackage {
		plan.Project.PackageManager = packageManager(path, &plan)
		plan.Project.HasWayFinder = packageJson.hasDependency("@laravel/vite-plugin-wayfinder")
		plan.Project.NodeVersion = nodeVersion(packageJson, &plan)
		ssr, ok := packageJson.ssr()
		if ok {
			plan.Note(fmt.Sprintf("found %s script -> SSR Enabled", ssr))
			plan.Project.HasSsr = true
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
