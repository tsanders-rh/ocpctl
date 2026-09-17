package substrate

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"

	"golang.org/x/crypto/ssh"
)

// generateKeypair creates a fresh ed25519 keypair. It returns the public key in
// OpenSSH authorized-keys format (for ec2.ImportKeyPair), an ssh.Signer over the
// private key, and the private key in OpenSSH PEM format. The PEM is written to
// the host (for host->node/VM ssh) and the cluster work dir (for operator
// debugging); it never leaves the operator's own machines.
func generateKeypair() (pub []byte, signer ssh.Signer, privPEM []byte, err error) {
	pubKey, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("generate ed25519 key: %w", err)
	}
	sshPub, err := ssh.NewPublicKey(pubKey)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("wrap public key: %w", err)
	}
	signer, err = ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build signer: %w", err)
	}
	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	return ssh.MarshalAuthorizedKey(sshPub), signer, pem.EncodeToMemory(block), nil
}
