package profile_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

// Guards the plumbing for metadata.warnings, the caveats shown in the create
// flow before a user commits to a cluster.
//
// Every hop here has already failed silently at least once. MetadataConfig had
// no Warnings field, so four profiles shipped a metadata.warnings block that
// YAML decoding dropped without error — the warning existed in git and reached
// nobody. Because the failure mode is a *silently* discarded field rather than
// an error, nothing in the build or the UI flags a regression; only an assertion
// on a parsed value does.
func TestProfileMetadataWarningsSurvivesYAML(t *testing.T) {
	loader := profile.NewLoader("definitions")

	t.Run("warnings are parsed, not silently dropped", func(t *testing.T) {
		prof, err := loader.Load("baremetal-rhwa-lab")
		require.NoError(t, err)
		require.NotNil(t, prof.Metadata, "metadata block must be parsed")

		require.NotEmpty(t, prof.Metadata.Warnings,
			"baremetal-rhwa-lab declares metadata.warnings; an empty slice here means "+
				"the YAML key is being discarded (check the yaml tag on MetadataConfig.Warnings)")

		// The hibernation caveat is load-bearing: the API rejects hibernate and
		// resume for bare metal, and TTL is the only cost control, so a user who
		// does not see this can only discover it after a ~60-90 minute create.
		assert.Contains(t, joined(prof.Metadata.Warnings), "Hibernate",
			"the hibernate/resume caveat must reach the user")
	})

	t.Run("every profile declaring warnings parses them", func(t *testing.T) {
		profiles, err := loader.LoadAll()
		require.NoError(t, err)
		require.NotEmpty(t, profiles)

		// Profiles that ship a warnings block today. Listed explicitly so that
		// deleting a warning is a deliberate test change rather than a silent
		// loss of a user-facing caveat.
		withWarnings := []string{
			"aws-rhwa-lab",
			"aws-rhwa-lab-prerelease",
			"baremetal-rhwa-lab",
			"baremetal-rhwa-lab-prerelease",
			"baremetal-rhwa-lab-with-odf",
			"baremetal-rhwa-lab-with-odf-prerelease",
		}

		byName := map[string]*profile.Profile{}
		for _, p := range profiles {
			byName[p.Name] = p
		}

		for _, name := range withWarnings {
			p, ok := byName[name]
			require.True(t, ok, "profile %s not found", name)
			require.NotNil(t, p.Metadata, "profile %s: metadata not parsed", name)
			assert.NotEmpty(t, p.Metadata.Warnings,
				"profile %s: metadata.warnings parsed as empty", name)
		}
	})

	// Profiles reach the API through the database, which stores the whole struct
	// as JSON (store.UpsertProfile marshals, store.GetProfile unmarshals). A
	// missing json tag would drop warnings on that hop instead.
	t.Run("warnings survive the JSON round-trip used by the profiles table", func(t *testing.T) {
		original := &profile.Profile{
			Name: "round-trip",
			Metadata: &profile.MetadataConfig{
				Notes:    []string{"a note"},
				Warnings: []string{"cannot be hibernated"},
			},
		}

		encoded, err := json.Marshal(original)
		require.NoError(t, err)

		var decoded profile.Profile
		require.NoError(t, json.Unmarshal(encoded, &decoded))

		require.NotNil(t, decoded.Metadata, "metadata lost in JSON round-trip")
		assert.Equal(t, []string{"cannot be hibernated"}, decoded.Metadata.Warnings)
		assert.Equal(t, []string{"a note"}, decoded.Metadata.Notes)
	})

	// Rows written before Profile.Metadata had a json tag used the Go default
	// key "Metadata". encoding/json falls back to a case-insensitive match, so
	// those rows still decode; assert it rather than trust it, since the startup
	// sync only rewrites rows for profiles that still exist on disk.
	t.Run("pre-existing rows keyed Metadata still decode", func(t *testing.T) {
		var decoded profile.Profile
		require.NoError(t, json.Unmarshal(
			[]byte(`{"Name":"legacy","Metadata":{"warnings":["legacy warning"]}}`),
			&decoded))

		require.NotNil(t, decoded.Metadata)
		assert.Equal(t, []string{"legacy warning"}, decoded.Metadata.Warnings)
	})
}

func joined(ss []string) string {
	out := ""
	for _, s := range ss {
		out += s + "\n"
	}
	return out
}
