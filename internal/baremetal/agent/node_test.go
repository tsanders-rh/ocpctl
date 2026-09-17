package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeNode struct {
	out map[string]string // substring(cmd) -> stdout
}

func (f *fakeNode) runOnNode(_ context.Context, _ string, cmd string) (string, error) {
	for k, v := range f.out {
		if strings.Contains(cmd, k) {
			return v, nil
		}
	}
	return "", nil
}

func TestRewriteRecoveryKubeconfig(t *testing.T) {
	raw := "apiVersion: v1\n" +
		"clusters:\n- cluster:\n    certificate-authority-data: AAAA\n    server: https://localhost:6443\n" +
		"    tls-server-name: api-int.local\n"
	out := rewriteRecoveryKubeconfig(raw, "api.rhwa-lab.example.com")
	assert.Contains(t, out, "server: https://api.rhwa-lab.example.com:6443")
	assert.Contains(t, out, "insecure-skip-tls-verify: true")
	assert.NotContains(t, out, "certificate-authority-data")
	assert.NotContains(t, out, "tls-server-name")
}

func TestClusterAvailableViaRecovery(t *testing.T) {
	nc := &fakeNode{out: map[string]string{"get clusterversion": "True\n"}}
	assert.True(t, clusterAvailableViaRecovery(context.Background(), nc, "192.168.126.11"))
	nc2 := &fakeNode{out: map[string]string{"get clusterversion": "False"}}
	assert.False(t, clusterAvailableViaRecovery(context.Background(), nc2, "192.168.126.11"))
	// A nil connector is always false.
	assert.False(t, clusterAvailableViaRecovery(context.Background(), nil, "192.168.126.11"))
}

func TestRecoverKubeconfig(t *testing.T) {
	nc := &fakeNode{out: map[string]string{"cat ": "server: https://localhost:6443\n    certificate-authority-data: X\n"}}
	kc, err := recoverKubeconfig(context.Background(), nc, "192.168.126.11", "api.rhwa-lab.example.com")
	require.NoError(t, err)
	assert.Contains(t, string(kc), "api.rhwa-lab.example.com:6443")
	assert.Contains(t, string(kc), "insecure-skip-tls-verify: true")
}

// TestInstall_RecoversViaNode exercises the self-healing path: the installer
// kubeconfig fails to authenticate, and a node connector recovers a working one.
func TestInstall_RecoversViaNode(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		if strings.Contains(cmd, "REUSE") {
			return "REUSE", nil
		}
		if strings.Contains(cmd, "domstate") {
			return "running", nil
		}
		return "", nil
	}
	f.run = func(s string) error {
		if strings.Contains(s, "oc get clusterversion") {
			return assertErr("orphaned kubeconfig")
		}
		return nil
	}
	nc := &fakeNode{out: map[string]string{
		"cat /etc": "server: https://localhost:6443\n    certificate-authority-data: X\n",
	}}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")

	res, err := install(context.Background(), f, nc, func(time.Duration) {}, spec)
	require.NoError(t, err)
	assert.True(t, res.Recovered)
	assert.Contains(t, string(res.Kubeconfig), "api.rhwa-lab.example.com:6443")
	assert.Contains(t, string(res.KubeadminPassword), "recovered kubeconfig")
}
