package substrate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestGenerateKeypair(t *testing.T) {
	pubMaterial, signer, privPEM, err := generateKeypair()
	require.NoError(t, err)
	require.NotNil(t, signer)

	// pubMaterial parses as an authorized_keys line...
	parsed, _, _, _, err := ssh.ParseAuthorizedKey(pubMaterial)
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", parsed.Type())

	// ...and matches the signer's public key.
	assert.Equal(t, parsed.Marshal(), signer.PublicKey().Marshal())

	// The private key is a parseable OpenSSH PEM matching the signer.
	assert.Contains(t, string(privPEM), "OPENSSH PRIVATE KEY")
	priv, err := ssh.ParsePrivateKey(privPEM)
	require.NoError(t, err)
	assert.Equal(t, signer.PublicKey().Marshal(), priv.PublicKey().Marshal())

	// Two calls produce distinct keys.
	pub2, _, _, err := generateKeypair()
	require.NoError(t, err)
	assert.NotEqual(t, pubMaterial, pub2)
}
