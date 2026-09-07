package launch

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"fmt"
)

// KeyPair is a generated deploy key: the private half goes to Coolify, the
// public half goes to the git host as a deploy key.
type KeyPair struct {
	Name       string
	PrivateKey string
	PublicKey  string
}

// GenerateKeyPair creates an unencrypted ed25519 SSH keypair in OpenSSH
// format, without shelling out to ssh-keygen.
func GenerateKeyPair(name string) (KeyPair, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate ed25519 key: %w", err)
	}

	check := make([]byte, 4)
	if _, err := rand.Read(check); err != nil {
		return KeyPair{}, fmt.Errorf("generate checkint: %w", err)
	}

	pubBlob := appendString(nil, []byte("ssh-ed25519"))
	pubBlob = appendString(pubBlob, pub)

	// ed25519.PrivateKey is seed||pub, exactly the 64-byte sk OpenSSH expects.
	privBlob := append([]byte{}, check...)
	privBlob = append(privBlob, check...)
	privBlob = appendString(privBlob, []byte("ssh-ed25519"))
	privBlob = appendString(privBlob, pub)
	privBlob = appendString(privBlob, priv)
	privBlob = appendString(privBlob, []byte(name))
	for i := byte(1); len(privBlob)%8 != 0; i++ {
		privBlob = append(privBlob, i)
	}

	var body []byte
	body = append(body, []byte("openssh-key-v1\x00")...)
	body = appendString(body, []byte("none"))
	body = appendString(body, []byte("none"))
	body = appendString(body, nil)
	body = binary.BigEndian.AppendUint32(body, 1)
	body = appendString(body, pubBlob)
	body = appendString(body, privBlob)

	block := &pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body}
	return KeyPair{
		Name:       name,
		PrivateKey: string(pem.EncodeToMemory(block)),
		PublicKey:  fmt.Sprintf("ssh-ed25519 %s %s", base64.StdEncoding.EncodeToString(pubBlob), name),
	}, nil
}

func appendString(dst, v []byte) []byte {
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(v)))
	return append(dst, v...)
}
