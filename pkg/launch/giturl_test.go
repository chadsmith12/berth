package launch_test

import (
	"testing"

	"github.com/chadsmith12/berth/pkg/launch"
)

func TestNormalizeGitRepo(t *testing.T) {
	cases := map[string]struct {
		in   string
		want string
	}{
		"scp with port":    {"git@git.example.com:2222/me/app.git", "git@git.example.com:2222/me/app.git"},
		"scp without port": {"git@github.com:me/app.git", "git@github.com:22/me/app.git"},
		"ssh with port":    {"ssh://git@git.example.com:2222/me/app.git", "git@git.example.com:2222/me/app.git"},
		"ssh without port": {"ssh://git@github.com/me/app.git", "git@github.com:22/me/app.git"},
		"ssh no git@ user": {"ssh://gitlab@git.example.com:2222/me/app.git", "git@git.example.com:2222/me/app.git"},
	}
	for name, c := range cases {
		got, err := launch.NormalizeGitRepo(c.in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", name, got, c.want)
		}
	}
}

func TestNormalizeGitRepoRejectsHTTP(t *testing.T) {
	if _, err := launch.NormalizeGitRepo("https://github.com/me/app.git"); err == nil {
		t.Fatal("https remotes must be refused in v1")
	}
}

func TestNormalizeGitRepoGarbage(t *testing.T) {
	if _, err := launch.NormalizeGitRepo("not a url"); err == nil {
		t.Fatal("garbage must error")
	}
}
