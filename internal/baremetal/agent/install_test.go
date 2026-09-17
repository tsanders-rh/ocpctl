package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeHost struct {
	runScripts []string
	captures   []string
	uploads    map[string][]byte
	// capture returns canned stdout for a command; matched by substring.
	capture func(cmd string) (string, error)
	// run returns an error for a script; matched by substring.
	run func(script string) error
}

func newFakeHost() *fakeHost { return &fakeHost{uploads: map[string][]byte{}} }

func (f *fakeHost) Run(_ context.Context, script string) error {
	f.runScripts = append(f.runScripts, script)
	if f.run != nil {
		return f.run(script)
	}
	return nil
}

func (f *fakeHost) RunCapture(_ context.Context, cmd string) (string, error) {
	f.captures = append(f.captures, cmd)
	if f.capture != nil {
		return f.capture(cmd)
	}
	return "", nil
}

func (f *fakeHost) Upload(_ context.Context, content io.Reader, remotePath string, _ os.FileMode) error {
	b, _ := io.ReadAll(content)
	f.uploads[remotePath] = b
	return nil
}

func (f *fakeHost) ran(sub string) bool {
	for _, s := range f.runScripts {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestInstall_HappyPath_BuildsAndFetches(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "REUSE"):
			return "BUILD", nil // ISO not present -> build
		case strings.Contains(cmd, "domstate"):
			return "shut off", nil // not running -> start
		case strings.Contains(cmd, "auth/kubeconfig"):
			return "apiVersion: v1\nkind: Config", nil
		case strings.Contains(cmd, "kubeadmin-password"):
			return "hunter2", nil
		}
		return "", nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("apiVersion: v1\n# install-config")

	res, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.NoError(t, err)

	// Configs uploaded.
	assert.Contains(t, string(f.uploads["/opt/ocpctl-agent/orig/install-config.yaml"]), "install-config")
	assert.Contains(t, string(f.uploads["/opt/ocpctl-agent/orig/agent-config.yaml"]), "kind: AgentConfig")
	// Tools fetched, ISO built, masters started, wait-for run.
	assert.True(t, f.ran("openshift-install-linux.tar.gz"))
	assert.True(t, f.ran("agent create image"))
	assert.True(t, f.ran("virsh start"))
	assert.True(t, f.ran("wait-for bootstrap-complete"))
	assert.True(t, f.ran("wait-for install-complete"))
	// Creds returned, URLs assembled.
	assert.False(t, res.Recovered)
	assert.Contains(t, string(res.Kubeconfig), "kind: Config")
	assert.Equal(t, []byte("hunter2"), bytes.TrimSpace(res.KubeadminPassword))
	assert.Equal(t, "https://api.rhwa-lab.example.com:6443", res.APIURL)
	assert.Equal(t, "https://console-openshift-console.apps.rhwa-lab.example.com", res.ConsoleURL)
}

func TestInstall_ReusesExistingISO(t *testing.T) {
	f := newFakeHost()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "REUSE"):
			return "REUSE", nil
		case strings.Contains(cmd, "domstate"):
			return "running", nil // already running -> no start
		case strings.Contains(cmd, "auth/kubeconfig"):
			return "kind: Config", nil
		}
		return "", nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")
	_, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.NoError(t, err)
	assert.False(t, f.ran("agent create image"), "must not rebuild the ISO")
	assert.False(t, f.ran("virsh start"), "must not start already-running masters")
}

func TestInstall_WaitReattach(t *testing.T) {
	f := newFakeHost()
	installCompleteCalls := 0
	f.capture = func(cmd string) (string, error) {
		if strings.Contains(cmd, "REUSE") {
			return "REUSE", nil
		}
		if strings.Contains(cmd, "domstate") {
			return "running", nil
		}
		if strings.Contains(cmd, "auth/kubeconfig") {
			return "kind: Config", nil
		}
		return "", nil
	}
	f.run = func(s string) error {
		if strings.Contains(s, "wait-for install-complete") {
			installCompleteCalls++
			if installCompleteCalls < 2 {
				return assertErr("wait-for returned early")
			}
		}
		return nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")
	_, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, installCompleteCalls, 2, "install-complete wait re-attached past the early return")
}

func TestInstall_KubeconfigUnverified_NoRecovery_Errors(t *testing.T) {
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
			return assertErr("kubeconfig invalid") // verify fails
		}
		return nil
	}
	spec := testInstallSpec()
	spec.InstallConfig = []byte("x")
	_, err := install(context.Background(), f, nil, func(time.Duration) {}, spec)
	require.Error(t, err) // no nodeConnector -> cannot recover
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
