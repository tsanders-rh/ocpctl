package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// toProfileResponse is a hand-written field-by-field copy, so any field it
// forgets is dropped from the API silently — the response is still valid JSON,
// just missing a key. That is exactly how profile metadata went missing: the
// YAML parsed it (notes, at least), the database round-tripped it, and then the
// DTO left it behind, so /api/v1/profiles/<name> returned no metadata at all.
func TestToProfileResponseIncludesMetadata(t *testing.T) {
	prof := &profile.Profile{
		Name:        "baremetal-rhwa-lab",
		DisplayName: "Emulated Bare Metal RHWA Lab",
		Metadata: &profile.MetadataConfig{
			Notes:    []string{"agent-based install"},
			Warnings: []string{"Hibernate/resume and pooling are not supported for bare-metal clusters"},
		},
	}

	resp := toProfileResponse(prof)

	require.NotNil(t, resp.Metadata, "metadata dropped by toProfileResponse")
	assert.Equal(t, prof.Metadata.Warnings, resp.Metadata.Warnings)
	assert.Equal(t, prof.Metadata.Notes, resp.Metadata.Notes)

	// The web client reads metadata.warnings, so assert the serialized shape
	// rather than only the struct field.
	encoded, err := json.Marshal(resp)
	require.NoError(t, err)

	var asMap map[string]any
	require.NoError(t, json.Unmarshal(encoded, &asMap))

	metadata, ok := asMap["metadata"].(map[string]any)
	require.True(t, ok, `response must carry a "metadata" object; got: %s`, encoded)
	assert.Len(t, metadata["warnings"], 1)
}

// A profile without a metadata block must not grow an empty object in the
// response; the web client keys its warning banner off the field's presence.
func TestToProfileResponseOmitsAbsentMetadata(t *testing.T) {
	resp := toProfileResponse(&profile.Profile{Name: "aws-sno-ga"})
	assert.Nil(t, resp.Metadata)

	encoded, err := json.Marshal(resp)
	require.NoError(t, err)

	var asMap map[string]any
	require.NoError(t, json.Unmarshal(encoded, &asMap))
	assert.NotContains(t, asMap, "metadata")
}
