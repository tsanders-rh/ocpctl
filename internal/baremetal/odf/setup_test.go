package odf

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeExec struct {
	runs        []string
	nodeRuns    []string
	uploads     map[string][]byte
	capture     func(cmd string) (string, error)
	nodeCapture func(cmd string) (string, error)
}

func newFakeExec() *fakeExec { return &fakeExec{uploads: map[string][]byte{}} }

func (f *fakeExec) Run(_ context.Context, script string) error {
	f.runs = append(f.runs, script)
	return nil
}
func (f *fakeExec) RunCapture(_ context.Context, cmd string) (string, error) {
	if f.capture != nil {
		return f.capture(cmd)
	}
	return "", nil
}
func (f *fakeExec) Upload(_ context.Context, content io.Reader, remotePath string, _ os.FileMode) error {
	b, _ := io.ReadAll(content)
	f.uploads[remotePath] = b
	return nil
}
func (f *fakeExec) RunNode(_ context.Context, _, _, script string) error {
	f.nodeRuns = append(f.nodeRuns, script)
	return nil
}
func (f *fakeExec) RunNodeCapture(_ context.Context, _, _, cmd string) (string, error) {
	if f.nodeCapture != nil {
		return f.nodeCapture(cmd)
	}
	return "", nil
}
func (f *fakeExec) ranContains(sub string) bool     { return anyContains(f.runs, sub) }
func (f *fakeExec) nodeRanContains(sub string) bool { return anyContains(f.nodeRuns, sub) }

func anyContains(ss []string, sub string) bool {
	for _, s := range ss {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func TestSetup_HappyPath(t *testing.T) {
	f := newFakeExec()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "domstate"):
			return "", nil // VM missing -> build fresh
		case strings.Contains(cmd, "packagemanifest"):
			return "stable-4.22|stable-4.20,stable-4.22,", nil
		case strings.Contains(cmd, "get csv"):
			return "Succeeded", nil
		case strings.Contains(cmd, "storagecluster"):
			return "Ready", nil
		}
		return "", nil
	}
	f.nodeCapture = func(string) (string, error) { return exporterJSON, nil }

	spec := testSpec()
	spec.ODFChannel = "stable-4.22"
	require.NoError(t, setup(context.Background(), f, spec, func(time.Duration) {}))

	// VM built fresh + seed uploaded.
	assert.True(t, f.ranContains("virt-install"), "ceph VM defined")
	assert.Contains(t, string(f.uploads["/tmp/rhwa-lab-ceph-0-seed/network-config"]), "192.168.126.10/24")
	// Bootstrap ran on the VM.
	assert.True(t, f.nodeRanContains("cephadm"), "ceph bootstrap on VM")
	// Operator subscribed on the resolved channel.
	assert.True(t, f.ranContains("channel: stable-4.22"))
	assert.True(t, f.ranContains("name: odf-operator"))
	// External details imported (objects list + blob secret).
	assert.True(t, f.ranContains("rook-ceph-external-cluster-details"))
	assert.True(t, f.ranContains(`"kind":"List"`))
	// External StorageCluster created.
	assert.True(t, f.ranContains("ocs-external-storagecluster"))
}

func TestSetup_ChannelFallsBackWhenDesiredAbsent(t *testing.T) {
	f := newFakeExec()
	f.capture = func(cmd string) (string, error) {
		switch {
		case strings.Contains(cmd, "domstate"):
			return "running", nil // VM already up -> not rebuilt
		case strings.Contains(cmd, "packagemanifest"):
			return "stable-4.22|stable-4.22,", nil // desired stable-4.20 absent
		case strings.Contains(cmd, "get csv"):
			return "Succeeded", nil
		case strings.Contains(cmd, "storagecluster"):
			return "Ready", nil
		}
		return "", nil
	}
	f.nodeCapture = func(string) (string, error) { return exporterJSON, nil }

	spec := testSpec()
	spec.ODFChannel = "stable-4.20" // not offered by the catalog
	require.NoError(t, setup(context.Background(), f, spec, func(time.Duration) {}))

	assert.False(t, f.ranContains("virt-install"), "running VM must not be rebuilt")
	assert.True(t, f.ranContains("channel: stable-4.22"), "fell back to the catalog default")
}

func TestDefineCephVM_StoppedIsStartedNotRebuilt(t *testing.T) {
	f := newFakeExec()
	f.capture = func(cmd string) (string, error) {
		if strings.Contains(cmd, "domstate") {
			return "shut off", nil
		}
		return "", nil
	}
	require.NoError(t, DefineCephVM(context.Background(), f, testSpec()))
	assert.True(t, f.ranContains("virsh start"), "stopped VM started")
	assert.False(t, f.ranContains("virt-install"), "existing VM never rebuilt")
	assert.Empty(t, f.uploads, "no seed rebuild for an existing VM")
}
