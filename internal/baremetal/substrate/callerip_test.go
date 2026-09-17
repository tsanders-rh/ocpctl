package substrate

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectCallerCIDRs_OK(t *testing.T) {
	get := func(string) (*http.Response, error) {
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("203.0.113.7\n")),
		}, nil
	}
	assert.Equal(t, []string{"203.0.113.7/32"}, detectCallerCIDRs(get))
}

func TestDetectCallerCIDRs_FailsSafe(t *testing.T) {
	get := func(string) (*http.Response, error) { return nil, errors.New("network down") }
	assert.Equal(t, []string{"0.0.0.0/32"}, detectCallerCIDRs(get))
}
