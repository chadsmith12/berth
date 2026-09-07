package launch

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ComposeInfo is what launch needs to know about the generated compose file:
// the port the app service exposes (Coolify's proxy routes by it) and
// whether the stack runs Horizon (which requires a redis resource).
type ComposeInfo struct {
	Port    int
	Horizon bool
}

// ReadCompose parses a generated compose file for launch's needs. A missing
// app service or exposed port is an error — the domain cannot be derived
// without the port.
func ReadCompose(path string) (ComposeInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ComposeInfo{}, fmt.Errorf("read %s: %w", path, err)
	}
	var cf struct {
		Services map[string]struct {
			Expose []any `yaml:"expose"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return ComposeInfo{}, fmt.Errorf("parse %s: %w", path, err)
	}
	app, ok := cf.Services["app"]
	if !ok {
		return ComposeInfo{}, fmt.Errorf("%s: no app service — run berth generate --env <env> first", path)
	}
	info := ComposeInfo{}
	for _, e := range app.Expose {
		if p, ok := portOf(e); ok {
			info.Port = p
			break
		}
	}
	if info.Port == 0 {
		return ComposeInfo{}, fmt.Errorf("%s: the app service exposes no port", path)
	}
	if _, ok := cf.Services["horizon"]; ok {
		info.Horizon = true
	}
	return info, nil
}

func portOf(v any) (int, bool) {
	switch e := v.(type) {
	case int:
		return e, true
	case string:
		s, _, _ := strings.Cut(e, "/")
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			return n, true
		}
	case float64:
		return int(e), true
	}
	return 0, false
}
