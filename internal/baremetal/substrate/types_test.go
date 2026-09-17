package substrate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	octypes "github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestApplyDefaults(t *testing.T) {
	s := LaunchSpec{ClusterName: "c1"}
	applyDefaults(&s)
	assert.Equal(t, defaultInstanceType, s.InstanceType)
	assert.Equal(t, defaultAMIOwner, s.AMIOwner)
	assert.Equal(t, defaultFedoraRelease, s.FedoraRelease)
	assert.Equal(t, defaultHostVolumeGB, s.HostVolumeGB)
	assert.Equal(t, defaultUser, s.User)

	// Explicit values are preserved.
	s2 := LaunchSpec{ClusterName: "c1", InstanceType: "m8i.24xlarge", HostVolumeGB: 500, User: "cloud"}
	applyDefaults(&s2)
	assert.Equal(t, "m8i.24xlarge", s2.InstanceType)
	assert.Equal(t, 500, s2.HostVolumeGB)
	assert.Equal(t, "cloud", s2.User)
}

func TestBuildTags(t *testing.T) {
	when := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	spec := LaunchSpec{ClusterID: "id-123", ClusterName: "mycluster", CreatedAt: when}
	tags := buildTags(spec)

	// Provenance tags present.
	for k, v := range octypes.ProvenanceTags("id-123", "mycluster", when) {
		assert.Equal(t, v, tags[k], "provenance tag %s", k)
	}
	// ocpctl janitor/orphan tags present.
	assert.Equal(t, "ocpctl", tags["ManagedBy"])
	assert.Equal(t, "mycluster", tags["ClusterName"])
	assert.Equal(t, "mycluster", tags["Name"])
}

func TestFQDNs(t *testing.T) {
	assert.Equal(t, "api.mycluster.example.com", apiFQDN("mycluster", "example.com"))
	assert.Equal(t, "*.apps.mycluster.example.com", appsFQDN("mycluster", "example.com"))
	// Trailing dot on base domain tolerated.
	assert.Equal(t, "api.mycluster.example.com", apiFQDN("mycluster", "example.com."))
	require.NotEmpty(t, keypairName("mycluster"))
	assert.Equal(t, "mycluster-key", keypairName("mycluster"))
	assert.Equal(t, "mycluster-sg", sgName("mycluster"))
}
