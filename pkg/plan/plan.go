package plan

type PackageManager string

const (
	PM_NPM PackageManager = "npm"
	PM_BUN PackageManager = "bun"
)

type SupportedPackageManager struct {
	File string
	Pm   PackageManager
}

var SupportedPackageManagers = []SupportedPackageManager{
	{File: "package-lock.json", Pm: PM_NPM},
	{File: "bun.lock", Pm: PM_BUN},
	{File: "bun.lockb", Pm: PM_BUN},
}

type Plan struct {
	Project Project
	Notes   []string
}

type Project struct {
	Path           string
	Name           string
	BaseDir        string
	PhpVersion     string
	NodeVersion    string
	PackageManager PackageManager
	HasWayFinder   bool
	HasSsr         bool
	SsrScript      string
	HasHorizon     bool
	Workers        int
	Scheduler      bool
	Port           int
	Env            string
}

func (p *Plan) Note(note string) {
	p.Notes = append(p.Notes, note)
}
