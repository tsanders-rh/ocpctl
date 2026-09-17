package rhwa

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeRunner struct {
	runs     []string
	captures []string
	capture  func(cmd string) (string, error)
}

func (f *fakeRunner) Run(_ context.Context, script string) error {
	f.runs = append(f.runs, script)
	return nil
}

func (f *fakeRunner) RunCapture(_ context.Context, cmd string) (string, error) {
	f.captures = append(f.captures, cmd)
	if f.capture != nil {
		return f.capture(cmd)
	}
	return "", nil
}

func (f *fakeRunner) ranContaining(sub string) int {
	n := 0
	for _, s := range f.runs {
		if strings.Contains(s, sub) {
			n++
		}
	}
	return n
}

func TestInstallOperators_AppliesThenPollsCSVs(t *testing.T) {
	f := &fakeRunner{capture: func(string) (string, error) { return "Succeeded", nil }}
	require.NoError(t, installOperators(context.Background(), f, testSpec(), func(time.Duration) {}))

	assert.Equal(t, 1, f.ranContaining("kind: Subscription"))
	csvPolls := 0
	for _, c := range f.captures {
		if strings.Contains(c, "get csv") {
			csvPolls++
		}
	}
	assert.Equal(t, len(testOperators()), csvPolls)
}

func TestConfigureFencing_Applies(t *testing.T) {
	f := &fakeRunner{}
	require.NoError(t, ConfigureFencing(context.Background(), f, testSpec()))
	assert.Equal(t, 1, f.ranContaining("kind: FenceAgentsRemediationTemplate"))
	assert.Equal(t, 1, f.ranContaining("kind: NodeHealthCheck"))
}
