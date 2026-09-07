package launch_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chadsmith12/berth/pkg/launch"
)

func TestGenerateKeyPairFormat(t *testing.T) {
	pair, err := launch.GenerateKeyPair("probe")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if !strings.Contains(pair.PrivateKey, "-----BEGIN OPENSSH PRIVATE KEY-----") {
		t.Fatalf("private key not OpenSSH PEM:\n%s", pair.PrivateKey)
	}
	if !strings.HasPrefix(pair.PublicKey, "ssh-ed25519 ") || !strings.HasSuffix(pair.PublicKey, " probe") {
		t.Fatalf("public key %q", pair.PublicKey)
	}
}

// TestGenerateKeyPairAcceptedBySSH asks ssh-keygen to parse and re-derive the
// public key — the strongest available check that the hand-rolled OpenSSH
// container is correct.
func TestGenerateKeyPairAcceptedBySSH(t *testing.T) {
	sshKeygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen not available")
	}
	pair, err := launch.GenerateKeyPair("probe")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, []byte(pair.PrivateKey), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(sshKeygen, "-y", "-f", path).Output()
	if err != nil {
		t.Fatalf("ssh-keygen rejected the key: %v", err)
	}
	got := strings.Fields(string(out))
	want := strings.Fields(pair.PublicKey)
	if len(got) < 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("public keys differ:\n got %v\nwant %v", got, want)
	}
}
