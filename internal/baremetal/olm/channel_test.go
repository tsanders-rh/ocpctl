package olm

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	out string
	err error
}

func (f *fakeRunner) RunCapture(context.Context, string) (string, error) {
	return f.out, f.err
}

func resolve(t *testing.T, out, desired string) (string, error) {
	t.Helper()
	return resolveChannel(context.Background(), &fakeRunner{out: out}, func(time.Duration) {}, "oc", "odf-operator", desired)
}

func TestResolveChannel_DesiredPresent(t *testing.T) {
	got, err := resolve(t, "stable-4.22|stable-4.18,stable-4.19,stable-4.22,", "stable-4.19")
	require.NoError(t, err)
	assert.Equal(t, "stable-4.19", got)
}

func TestResolveChannel_DesiredAbsent_UsesDefault(t *testing.T) {
	// The pre-release case: desired stable-4.20 isn't offered; catalog default is stable-4.22.
	got, err := resolve(t, "stable-4.22|stable-4.22,", "stable-4.20")
	require.NoError(t, err)
	assert.Equal(t, "stable-4.22", got)
}

func TestResolveChannel_NoDefault_UsesNewestStable(t *testing.T) {
	got, err := resolve(t, "|stable-4.18,stable-4.22,stable-4.19,", "stable-4.30")
	require.NoError(t, err)
	assert.Equal(t, "stable-4.22", got)
}

func TestResolveChannel_NotFound(t *testing.T) {
	// Empty output every poll -> error (no-op sleep keeps the test fast).
	_, err := resolveChannel(context.Background(), &fakeRunner{out: ""}, func(time.Duration) {}, "oc", "odf-operator", "stable-4.22")
	require.Error(t, err)
}

func TestResolveChannel_ErrorThenAbsent(t *testing.T) {
	_, err := resolveChannel(context.Background(), &fakeRunner{err: errors.New("boom")}, func(time.Duration) {}, "oc", "odf-operator", "stable-4.22")
	require.Error(t, err)
}
