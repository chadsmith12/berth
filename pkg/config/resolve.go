package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Overrides are values supplied by flags or the process environment. Highest
// precedence in placement resolution. TeamID is nil when unset — team 0 is a
// real team id.
type Overrides struct {
	Profile string
	URL     string
	TeamID  *int
	Project string
}

// Placement is where berth acts: one instance, one team, optionally one
// project. Team is nil when no team could be resolved and the caller marked
// the team optional.
type Placement struct {
	URL     string
	Team    *TeamRef
	Project string
}

// ResolveOptions supplies the inputs for placement resolution.
type ResolveOptions struct {
	Overrides    Overrides
	RepoRoot     string      // project root scanned for .config/berth.json / berth.json
	ConfigPath   string      // -c: explicit repo config path; skips the scan
	Repo         *RepoConfig // preloaded repository config; skips the scan
	UserPath     string      // user config path; defaults to UserConfigPath()
	OptionalTeam bool        // leave Team nil instead of erroring when unresolved
}

// Resolve applies the placement precedence. The identity — which instance and
// team — is picked first: a named profile (--profile / BERTH_PROFILE), then
// the repository coolify block, then the default profile. Flags and BERTH_*
// variables then override individual fields for the run. A value that cannot
// be resolved is an error naming every place it could be set.
func Resolve(opts ResolveOptions) (Placement, error) {
	repo, repoDesc, err := loadRepoForResolve(opts)
	if err != nil {
		return Placement{}, err
	}

	userPath := opts.UserPath
	if userPath == "" {
		userPath, err = UserConfigPath()
		if err != nil {
			return Placement{}, err
		}
	}
	user, err := LoadUserConfig(userPath)
	if err != nil {
		return Placement{}, err
	}

	profileName := firstNonEmpty(opts.Overrides.Profile, envString("BERTH_PROFILE"))
	var profile, defaultProfile Profile
	if profileName != "" {
		p, ok := user.ProfileFor(profileName)
		if !ok {
			return Placement{}, fmt.Errorf("no profile named %q — known profiles: %s", profileName, ProfileNames(user.Profiles))
		}
		profile = p
	} else {
		defaultProfile, _ = user.DefaultProfile()
	}

	if v := envString("BERTH_TEAM"); v != "" {
		if _, err := strconv.Atoi(v); err != nil {
			return Placement{}, fmt.Errorf("BERTH_TEAM must be a team id (an integer), got %q", v)
		}
	}

	url := firstNonEmpty(
		opts.Overrides.URL,
		envString("BERTH_URL"),
		profile.URL,
		coolifyURL(repo),
		defaultProfile.URL,
	)
	if url == "" {
		return Placement{}, fmt.Errorf("cannot resolve the Coolify instance url — set one of: --url, BERTH_URL, --profile, \"coolify\": {\"url\"} in %s, or run berth auth login", repoDesc)
	}

	teamID, teamFound := 0, false
	switch {
	case opts.Overrides.TeamID != nil:
		teamID, teamFound = *opts.Overrides.TeamID, true
	case envString("BERTH_TEAM") != "":
		teamID, _ = strconv.Atoi(envString("BERTH_TEAM"))
		teamFound = true
	default:
		for _, ref := range []*TeamRef{profile.Team, teamRef(repo), defaultProfile.Team} {
			if ref != nil {
				teamID, teamFound = ref.ID, true
				break
			}
		}
	}
	if !teamFound && !opts.OptionalTeam {
		return Placement{}, fmt.Errorf("cannot resolve the Coolify team — set one of: --team, BERTH_TEAM, --profile, \"coolify\": {\"team\": {\"id\"}} in %s, or run berth auth login", repoDesc)
	}

	project := firstNonEmpty(opts.Overrides.Project, envString("BERTH_PROJECT"), coolifyProject(repo))

	p := Placement{
		URL:     NormalizeURL(url),
		Project: project,
	}
	if teamFound {
		p.Team = &TeamRef{ID: teamID, Name: teamNameFor(teamID, profile.Team, teamRef(repo), defaultProfile.Team)}
	}
	return p, nil
}

func loadRepoForResolve(opts ResolveOptions) (*RepoConfig, string, error) {
	if opts.Repo != nil {
		return opts.Repo, repoDescFor(opts), nil
	}
	if opts.ConfigPath != "" {
		cfg, err := LoadRepoConfig(opts.ConfigPath)
		if err != nil {
			return nil, "", err
		}
		return &cfg, opts.ConfigPath, nil
	}
	if opts.RepoRoot == "" {
		return nil, repoDirConfig, nil
	}
	found, ok, err := FindRepoConfig(opts.RepoRoot)
	if err != nil {
		return nil, "", err
	}
	if !ok {
		return nil, repoDirConfig, nil
	}
	cfg, err := LoadRepoConfig(found)
	if err != nil {
		return nil, "", err
	}
	return &cfg, found, nil
}

func repoDescFor(opts ResolveOptions) string {
	if opts.ConfigPath != "" {
		return opts.ConfigPath
	}
	return repoDirConfig
}

func coolifyURL(c *RepoConfig) string {
	if c == nil || c.Coolify == nil {
		return ""
	}
	return c.Coolify.URL
}

func coolifyProject(c *RepoConfig) string {
	if c == nil || c.Coolify == nil {
		return ""
	}
	return c.Coolify.Project
}

func teamRef(c *RepoConfig) *TeamRef {
	if c == nil || c.Coolify == nil {
		return nil
	}
	return c.Coolify.Team
}

func teamNameFor(id int, refs ...*TeamRef) string {
	for _, r := range refs {
		if r != nil && r.ID == id {
			return r.Name
		}
	}
	return ""
}

func envString(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
