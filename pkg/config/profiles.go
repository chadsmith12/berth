package config

import (
	"fmt"
	"sort"
	"strings"
)

// Profile is a named identity: an instance and team pair under a name you
// chose. Tokens stay keyed by instance and team; a profile is a handle for
// them.
type Profile struct {
	Name string   `json:"name"`
	URL  string   `json:"url"`
	Team *TeamRef `json:"team"`
}

// ProfileFor returns the profile registered under name.
func (c UserConfig) ProfileFor(name string) (Profile, bool) {
	for _, p := range c.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

// DefaultProfile returns the profile the default pointer names.
func (c UserConfig) DefaultProfile() (Profile, bool) {
	if c.Default == "" {
		return Profile{}, false
	}
	return c.ProfileFor(c.Default)
}

// UpsertProfile inserts or replaces the profile with the same name.
func (c *UserConfig) UpsertProfile(p Profile) {
	for i := range c.Profiles {
		if c.Profiles[i].Name == p.Name {
			c.Profiles[i] = p
			return
		}
	}
	c.Profiles = append(c.Profiles, p)
}

// ProfileConflict reports whether name already refers to a different
// instance or team. Re-login to the same identity upserts; a name pointing
// elsewhere is refused rather than silently disambiguated.
func (c UserConfig) ProfileConflict(name, url string, teamID int) bool {
	p, ok := c.ProfileFor(name)
	if !ok {
		return false
	}
	return p.URL != NormalizeURL(url) || p.Team == nil || p.Team.ID != teamID
}

// SetDefault points the default at a known profile.
func (c *UserConfig) SetDefault(name string) error {
	if _, ok := c.ProfileFor(name); !ok {
		return fmt.Errorf("no profile named %q — known profiles: %s", name, ProfileNames(c.Profiles))
	}
	c.Default = name
	return nil
}

// ProfileNames returns the sorted profile names.
func ProfileNames(profiles []Profile) string {
	names := make([]string, 0, len(profiles))
	for _, p := range profiles {
		names = append(names, p.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// ProfileSlug derives a profile name from a team name: lowercased, with
// runs of non-alphanumeric characters collapsed to single dashes.
func ProfileSlug(teamName string) string {
	var b strings.Builder
	prevDash := true
	for _, r := range strings.ToLower(teamName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func (c UserConfig) validate() error {
	seen := map[string]bool{}
	for _, p := range c.Profiles {
		if p.Name == "" {
			return fmt.Errorf("profile name must not be empty")
		}
		if p.URL == "" {
			return fmt.Errorf("profile %q has no instance url", p.Name)
		}
		if p.Team == nil {
			return fmt.Errorf("profile %q has no team", p.Name)
		}
		if seen[p.Name] {
			return fmt.Errorf("profile %q is listed more than once", p.Name)
		}
		seen[p.Name] = true
	}
	if c.Default != "" {
		if _, ok := seen[c.Default]; !ok {
			return fmt.Errorf("default profile %q does not exist", c.Default)
		}
	}
	return nil
}
