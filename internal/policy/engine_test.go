package policy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tsanders-rh/ocpctl/internal/policy"
	"github.com/tsanders-rh/ocpctl/internal/profile"
)

func setupPolicyEngine(t *testing.T) *policy.Engine {
	loader := profile.NewLoader("../profile/definitions")
	registry, err := profile.NewRegistry(loader)
	require.NoError(t, err)

	return policy.NewEngine(registry)
}

func TestEngine_ValidateCreateRequest(t *testing.T) {
	engine := setupPolicyEngine(t)

	t.Run("validates valid request", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
			ExtraTags: map[string]string{
				"Purpose": "testing",
			},
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.True(t, result.Valid)
		assert.Empty(t, result.Errors)
		assert.NotEmpty(t, result.MergedTags)
		assert.NotEmpty(t, result.DestroyAt)
	})

	t.Run("rejects invalid cluster name", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "INVALID_NAME", // Uppercase not allowed
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.False(t, result.Valid)
		assert.NotEmpty(t, result.Errors)

		found := false
		for _, e := range result.Errors {
			if e.Field == "name" {
				found = true
				break
			}
		}
		assert.True(t, found, "should have name validation error")
	})

	t.Run("rejects version not in allowlist", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.19.0", // Not in allowlist
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.False(t, result.Valid)

		found := false
		for _, e := range result.Errors {
			if e.Field == "version" {
				found = true
				break
			}
		}
		assert.True(t, found, "should have version validation error")
	})

	t.Run("rejects region not in allowlist", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "ap-south-1", // Not in allowlist
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.False(t, result.Valid)

		found := false
		for _, e := range result.Errors {
			if e.Field == "region" {
				found = true
				break
			}
		}
		assert.True(t, found, "should have region validation error")
	})

	t.Run("rejects TTL exceeding max", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   1000, // Exceeds max 72 hours
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.False(t, result.Valid)

		found := false
		for _, e := range result.Errors {
			if e.Field == "ttlHours" {
				found = true
				assert.Contains(t, e.Message, "exceeds")
				break
			}
		}
		assert.True(t, found, "should have TTL validation error")
	})

	t.Run("prevents reserved tag override", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "test-user",
			Team:       "platform-team",
			CostCenter: "engineering",
			TTLHours:   24,
			ExtraTags: map[string]string{
				"ManagedBy": "hacker", // Reserved key!
			},
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.False(t, result.Valid)

		found := false
		for _, e := range result.Errors {
			if e.Field == "extraTags" {
				found = true
				assert.Contains(t, e.Message, "reserved")
				break
			}
		}
		assert.True(t, found, "should prevent reserved tag override")
	})

	t.Run("merges tags correctly", func(t *testing.T) {
		req := &policy.CreateClusterRequest{
			Name:       "test-cluster-01",
			Platform:   "aws",
			Version:    "4.20.3",
			Profile:    "aws-minimal-test",
			Region:     "us-east-1",
			BaseDomain: "labs.example.com",
			Owner:      "alice",
			Team:       "platform",
			CostCenter: "eng",
			TTLHours:   24,
			ExtraTags: map[string]string{
				"CustomTag": "custom-value",
			},
		}

		result, err := engine.ValidateCreateRequest(req)
		require.NoError(t, err)
		assert.True(t, result.Valid)

		// Check system tags are present
		assert.Equal(t, "cluster-control-plane", result.MergedTags["ManagedBy"])
		assert.Equal(t, "test-cluster-01", result.MergedTags["ClusterName"])
		assert.Equal(t, "alice", result.MergedTags["Owner"])
		assert.Equal(t, "platform", result.MergedTags["Team"])
		assert.Equal(t, "eng", result.MergedTags["CostCenter"])

		// Check user tag is present
		assert.Equal(t, "custom-value", result.MergedTags["CustomTag"])

		// Check profile required tags
		assert.Equal(t, "test", result.MergedTags["Environment"])
	})
}

func TestEngine_GetDefaults(t *testing.T) {
	engine := setupPolicyEngine(t)

	t.Run("gets default version", func(t *testing.T) {
		version, err := engine.GetDefaultVersion("aws-minimal-test")
		require.NoError(t, err)
		assert.Equal(t, "4.20.3", version)
	})

	t.Run("gets default region", func(t *testing.T) {
		region, err := engine.GetDefaultRegion("aws-minimal-test")
		require.NoError(t, err)
		assert.Equal(t, "us-east-1", region)
	})

	t.Run("gets default TTL", func(t *testing.T) {
		ttl, err := engine.GetDefaultTTL("aws-minimal-test")
		require.NoError(t, err)
		assert.Equal(t, 24, ttl)
	})
}
