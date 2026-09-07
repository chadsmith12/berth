package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Credentials struct {
	Instances []Instance `json:"instances"`
}

type Instance struct {
	URL   string      `json:"url"`
	Teams []TeamToken `json:"teams"`
}

type TeamToken struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Token string `json:"token"`
}

const (
	dirPerm  = 0700
	filePerm = 0600
)

func CredentialsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve config directory: %w", err)
	}
	return filepath.Join(dir, "berth", "credentials.json"), nil
}

func LoadCredentials(path string) (Credentials, error) {
	var c Credentials
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, nil
		}
		return c, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, &c); err != nil {
		return c, fmt.Errorf("parse %s: %w", path, err)
	}
	return c, nil
}

func SaveCredentials(path string, c Credentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, filePerm)
}

func NormalizeURL(url string) string {
	return strings.TrimRight(strings.TrimSpace(url), "/")
}

func UpsertToken(c *Credentials, url string, t TeamToken) {
	normalized := NormalizeURL(url)
	for i := range c.Instances {
		if c.Instances[i].URL != normalized {
			continue
		}
		for j := range c.Instances[i].Teams {
			if c.Instances[i].Teams[j].ID == t.ID {
				c.Instances[i].Teams[j] = t
				return
			}
		}
		c.Instances[i].Teams = append(c.Instances[i].Teams, t)
		return
	}
	c.Instances = append(c.Instances, Instance{URL: normalized, Teams: []TeamToken{t}})
}

func FindToken(c Credentials, url string, teamID int) (string, bool) {
	normalized := NormalizeURL(url)
	for _, inst := range c.Instances {
		if inst.URL != normalized {
			continue
		}
		for _, t := range inst.Teams {
			if t.ID == teamID {
				return t.Token, true
			}
		}
	}
	return "", false
}

// TokensFor returns the tokens stored for an instance URL.
func TokensFor(c Credentials, url string) []TeamToken {
	normalized := NormalizeURL(url)
	for _, inst := range c.Instances {
		if inst.URL == normalized {
			return inst.Teams
		}
	}
	return nil
}

// RemoveToken deletes the token stored for an instance and team, dropping the
// instance when it holds no tokens left. It reports whether a token was removed.
func RemoveToken(c *Credentials, url string, teamID int) bool {
	normalized := NormalizeURL(url)
	for i := range c.Instances {
		if c.Instances[i].URL != normalized {
			continue
		}
		for j, t := range c.Instances[i].Teams {
			if t.ID != teamID {
				continue
			}
			c.Instances[i].Teams = append(c.Instances[i].Teams[:j], c.Instances[i].Teams[j+1:]...)
			if len(c.Instances[i].Teams) == 0 {
				c.Instances = append(c.Instances[:i], c.Instances[i+1:]...)
			}
			return true
		}
		return false
	}
	return false
}

func TruncateToken(token string) string {
	if id, _, ok := strings.Cut(token, "|"); ok {
		return id + "|..."
	}
	return "..."
}
