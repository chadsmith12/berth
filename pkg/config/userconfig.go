package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const userFilePerm = 0644

// UserConfig is the user-level profile registry: named instance+team
// identities and which one is the default. Readable, unlike
// credentials.json, because it holds no secrets.
type UserConfig struct {
	Default  string    `json:"default,omitempty"`
	Profiles []Profile `json:"profiles,omitempty"`
}

// UserConfigPath is ~/.config/berth/config.json, next to credentials.json.
func UserConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve config directory: %w", err)
	}
	return filepath.Join(dir, "berth", "config.json"), nil
}

// LoadUserConfig reads the user config. A missing file is an empty config.
func LoadUserConfig(path string) (UserConfig, error) {
	var c UserConfig
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
	if err := c.validate(); err != nil {
		return c, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// SaveUserConfig writes the user config with readable permissions (0644).
func SaveUserConfig(path string, c UserConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, userFilePerm)
}
