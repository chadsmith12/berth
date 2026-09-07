package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	SchemaV1 = "https://berth.sh/schema/v1.json"

	repoDirConfig  = ".config/berth.json"
	repoRootConfig = "berth.json"

	repoFilePerm = 0644
)

// RepoConfig is the committed per-repository configuration: the environment
// registry and the optional Coolify placement block.
type RepoConfig struct {
	Schema       string        `json:"$schema,omitempty"`
	Environments []Environment `json:"environments"`
	Coolify      *Coolify      `json:"coolify,omitempty"`
}

// Environment links a Coolify environment name to the application uuid berth
// launched for it. The uuid is not a secret; it is safe to commit.
type Environment struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

// Coolify is the optional placement block. A repository may omit it to avoid
// disclosing internal hostnames and team structure; the user-level default
// covers it.
type Coolify struct {
	URL     string   `json:"url,omitempty"`
	Team    *TeamRef `json:"team,omitempty"`
	Project string   `json:"project,omitempty"`
}

// TeamRef identifies a team by id. The name is display only: it makes diffs
// readable and drift detectable, the id is authoritative.
type TeamRef struct {
	ID   int    `json:"id"`
	Name string `json:"name,omitempty"`
}

// FindRepoConfig returns the repository config path, checking
// .config/berth.json then berth.json at the repository root. Both present is
// an error: silently preferring one means somebody edits the wrong file.
func FindRepoConfig(root string) (string, bool, error) {
	dirPath := filepath.Join(root, repoDirConfig)
	rootPath := filepath.Join(root, repoRootConfig)
	_, dirErr := os.Stat(dirPath)
	_, rootErr := os.Stat(rootPath)
	haveDir := dirErr == nil
	haveRoot := rootErr == nil
	if haveDir && haveRoot {
		return "", false, fmt.Errorf("both %s and %s exist — remove one, berth will not choose between them", repoDirConfig, repoRootConfig)
	}
	if haveDir {
		return dirPath, true, nil
	}
	if haveRoot {
		return rootPath, true, nil
	}
	return "", false, nil
}

// LoadRepoFor finds and loads the repository config, honoring an explicit
// path. A missing file is a nil config; both files present is an error.
func LoadRepoFor(root, configPath string) (*RepoConfig, error) {
	if configPath != "" {
		cfg, err := LoadRepoConfig(configPath)
		if err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if root == "" {
		return nil, nil
	}
	found, ok, err := FindRepoConfig(root)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	cfg, err := LoadRepoConfig(found)
	if err != nil {
		return nil, err
	}
	return &cfg, nil
}

// LoadRepoConfig reads and validates a repository config file. A missing file
// is an error; callers check FindRepoConfig first unless the path is explicit.
func LoadRepoConfig(path string) (RepoConfig, error) {
	var c RepoConfig
	data, err := os.ReadFile(path)
	if err != nil {
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	if err := c.validate(); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// SaveRepoConfig writes the repository config, defaulting $schema when empty.
func SaveRepoConfig(path string, c RepoConfig) error {
	if c.Schema == "" {
		c.Schema = SchemaV1
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, repoFilePerm)
}

// EnvironmentFor returns the environment registered under name.
func (c RepoConfig) EnvironmentFor(name string) (Environment, bool) {
	for _, e := range c.Environments {
		if e.Name == name {
			return e, true
		}
	}
	return Environment{}, false
}

// UpsertEnvironment inserts or replaces the environment with the same name.
func (c *RepoConfig) UpsertEnvironment(e Environment) {
	for i := range c.Environments {
		if c.Environments[i].Name == e.Name {
			c.Environments[i] = e
			return
		}
	}
	c.Environments = append(c.Environments, e)
}

func (c RepoConfig) validate() error {
	seen := map[string]bool{}
	for _, e := range c.Environments {
		if e.Name == "" {
			return fmt.Errorf("environment name must not be empty")
		}
		if seen[e.Name] {
			return fmt.Errorf("environment %q is listed more than once", e.Name)
		}
		seen[e.Name] = true
	}
	if c.Coolify != nil && c.Coolify.Team != nil && c.Coolify.Team.ID == 0 {
		return fmt.Errorf("coolify.team.id is required when coolify.team is set")
	}
	return nil
}
