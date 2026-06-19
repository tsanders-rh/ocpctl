package worker

import (
	"strings"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// transientErrorPattern defines a pattern for detecting transient errors
type transientErrorPattern struct {
	Pattern     string // Error message substring to match
	Description string // Human-readable description
	Remediation string // User guidance
	BackoffMins int    // Suggested backoff in minutes
}

// knownTransientPatterns is a list of error patterns that indicate transient failures
var knownTransientPatterns = []transientErrorPattern{
	{
		Pattern:     "no nat gateways available",
		Description: "NAT Gateway timing issue (OpenShift 4.22 Cluster API race condition)",
		Remediation: `This is a transient error caused by a timing race in OpenShift 4.22's Cluster API integration.

The NAT gateway was created successfully, but the Cluster API controller checked for it
before AWS marked it as fully available.

What happened:
- NAT gateway was provisioned in AWS
- Cluster API controller tried to configure routes too quickly
- Controller didn't find the gateway (it was still in "pending" state)

This job will automatically retry in 5 minutes. The NAT gateway should be "available" by then
and the next attempt should succeed.

Alternative actions:
1. Wait for automatic retry (recommended)
2. Use OpenShift 4.21 (more stable Cluster API integration)
3. Pre-create VPC infrastructure to avoid the race condition

For details: https://github.com/kubernetes-sigs/cluster-api-provider-aws/issues/4234`,
		BackoffMins: 5,
	},
	{
		Pattern:     "cluster is not reachable",
		Description: "Cluster API server temporarily unreachable",
		Remediation: `The cluster API server is temporarily unreachable. This can happen during:
- Bootstrap process (API server not yet started)
- Network configuration (routes being updated)
- Load balancer provisioning

This job will automatically retry in 3 minutes.`,
		BackoffMins: 3,
	},
	{
		Pattern:     "connection to the workload cluster is down",
		Description: "Workload cluster connection lost",
		Remediation: `Connection to the workload cluster was lost during bootstrap. This can happen when:
- Network routes are being reconfigured
- API server is restarting
- Load balancer is being provisioned

This job will automatically retry in 3 minutes.`,
		BackoffMins: 3,
	},
	{
		Pattern:     "rate limit exceeded",
		Description: "AWS/Cloud provider rate limiting",
		Remediation: `The cloud provider (AWS/GCP/Azure) is rate limiting API calls.

This is usually caused by:
- Too many concurrent cluster deployments
- AWS service quota throttling
- Burst limit exceeded

This job will automatically retry in 10 minutes.`,
		BackoffMins: 10,
	},
	{
		Pattern:     "RequestLimitExceeded",
		Description: "AWS request limit exceeded",
		Remediation: `AWS is throttling API requests due to exceeding the request rate limit.

This job will automatically retry with exponential backoff.`,
		BackoffMins: 5,
	},
	{
		Pattern:     "Throttling",
		Description: "AWS API throttling",
		Remediation: `AWS API is being throttled. This job will automatically retry in 5 minutes.`,
		BackoffMins: 5,
	},
}

// DetectTransientError analyzes an error to determine if it's transient
// Returns a TransientError if the error matches known transient patterns, nil otherwise
func DetectTransientError(err error) *types.TransientError {
	if err == nil {
		return nil
	}

	// If it's already a TransientError, return it as-is
	if te, ok := err.(*types.TransientError); ok {
		return te
	}

	// Check error message against known patterns
	errMsg := strings.ToLower(err.Error())

	for _, pattern := range knownTransientPatterns {
		if strings.Contains(errMsg, strings.ToLower(pattern.Pattern)) {
			return &types.TransientError{
				Message:     pattern.Description,
				Cause:       err,
				Remediation: pattern.Remediation,
				BackoffMins: pattern.BackoffMins,
			}
		}
	}

	return nil
}
