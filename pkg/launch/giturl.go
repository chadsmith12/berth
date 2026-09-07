package launch

import (
	"errors"
	"fmt"
	"strings"
)

// NormalizeGitRepo converts a git remote URL into Coolify's scp-style
// git@host:port/path form. Coolify rejects ssh:// URLs; its convention
// carries the port after the host colon.
func NormalizeGitRepo(remote string) (string, error) {
	remote = strings.TrimSpace(remote)
	switch {
	case strings.HasPrefix(remote, "git@"):
		rest := strings.TrimPrefix(remote, "git@")
		host, tail, ok := strings.Cut(rest, ":")
		if !ok || host == "" {
			return "", fmt.Errorf("cannot parse git remote %q", remote)
		}
		port, path := "22", tail
		if seg, rest2, found := strings.Cut(tail, "/"); found && isDigits(seg) && seg != "" {
			port, path = seg, "/"+rest2
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return fmt.Sprintf("git@%s:%s%s", host, port, path), nil
	case strings.HasPrefix(remote, "ssh://"):
		rest := strings.TrimPrefix(remote, "ssh://")
		userHost, path, ok := strings.Cut(rest, "/")
		if !ok {
			return "", fmt.Errorf("cannot parse git remote %q", remote)
		}
		if at := strings.LastIndex(userHost, "@"); at >= 0 {
			userHost = userHost[at+1:]
		}
		host, port := userHost, "22"
		if h, p, found := strings.Cut(userHost, ":"); found {
			host, port = h, p
		}
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return fmt.Sprintf("git@%s:%s%s", host, port, path), nil
	case strings.HasPrefix(remote, "http://"), strings.HasPrefix(remote, "https://"):
		return "", errors.New("https remotes need the public-repo flow, which berth does not have yet — use an ssh remote (git@host:path)")
	default:
		return "", fmt.Errorf("cannot parse git remote %q — expected git@host:path or ssh://git@host:port/path", remote)
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}
