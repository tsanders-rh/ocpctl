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

// permanentErrorPattern describes a failure that a retry cannot fix.
type permanentErrorPattern struct {
	Pattern     string // lowercased error substring to match
	Description string // short, human-readable root cause for the DB/UI
}

// knownPermanentErrorPatterns lists error substrings that indicate a permanent
// failure. These are checked BEFORE the transient patterns because a permanent
// error can surface a transient-looking symptom downstream (e.g. an Azure
// SkuNotAvailable on one master stalls provisioning, which then bubbles up as
// "cluster is not reachable" / "failed to provision control-plane machines", or a
// misconfigured DNS zone yields "ResourceGroupNotFound" that co-occurs with
// "cluster is not reachable"). Retrying such an error just burns time and leaks
// partial infrastructure, so any match here means "fail fast, do not retry".
var knownPermanentErrorPatterns = []permanentErrorPattern{
	// Azure capacity / SKU availability (also short-circuits transient matching).
	{"skunotavailable", "Azure VM size unavailable in the selected region/zone"},
	{"not available in location", "Azure VM size not available in this location"},
	{"notavailableforsubscription", "Azure VM size/zone not enabled for this subscription"},
	{"capacity restrictions", "Azure capacity restrictions for the requested VM size"},
	{"overconstrainedallocationrequest", "Azure could not satisfy the requested zone/SKU allocation constraints"},
	// Azure configuration / DNS zone (the originally-reported #148 case).
	{"resourcegroupnotfound", "Azure resource group not found (check the profile's base-domain / DNS zone resource group)"},
	{"parentresourcenotfound", "Azure parent resource not found (check base-domain / DNS zone configuration)"},
	// Azure credentials / authorization.
	{"authorizationfailed", "Azure authorization failed — the service principal lacks a required permission"},
	{"invalidclientsecret", "Azure service-principal credential is invalid or expired"},
	{"invalidauthenticationtokentenant", "Azure credentials are scoped to the wrong tenant"},
	{"subscriptionnotfound", "Azure subscription not found or inaccessible"},
	// Azure quota.
	{"quotaexceeded", "Azure quota exceeded for the requested resources"},
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

	// Permanent errors take precedence: never retry a known-permanent failure
	// even if it also contains a transient-looking substring.
	for _, p := range knownPermanentErrorPatterns {
		if strings.Contains(errMsg, p.Pattern) {
			return nil
		}
	}

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

// DetectPermanentError reports whether err represents a permanent failure that a
// retry cannot fix (bad credentials, missing resource group / DNS zone, quota
// exhaustion, Azure capacity restrictions). It returns a short human-readable
// cause and the most relevant line from the error — preferring the installer's
// final `level=error` fatal message over any incidental controller log line that
// merely mentions the marker. When err is not recognized as permanent it returns
// ("", "").
//
// This matters because a permanent failure often co-occurs with a
// transient-looking symptom (e.g. "ResourceGroupNotFound" alongside "cluster is
// not reachable"). The caller must classify such an error as permanent and fail
// fast rather than retrying it up to MaxAttempts.
func DetectPermanentError(err error) (cause string, detail string) {
	if err == nil {
		return "", ""
	}
	raw := err.Error()
	lower := strings.ToLower(raw)
	for _, p := range knownPermanentErrorPatterns {
		if strings.Contains(lower, p.Pattern) {
			return p.Description, extractRelevantLine(raw, p.Pattern)
		}
	}
	return "", ""
}

// extractRelevantLine returns the line of raw most useful for diagnosing a
// permanent failure: among the lines that contain pattern, it prefers the last
// one tagged `level=error` (the installer's final fatal message), falling back to
// the last line that merely mentions the pattern. Returns "" if no line matches.
func extractRelevantLine(raw, pattern string) string {
	var match, errLevelMatch string
	for _, ln := range strings.Split(raw, "\n") {
		l := strings.ToLower(ln)
		if !strings.Contains(l, pattern) {
			continue
		}
		match = strings.TrimSpace(ln)
		if strings.Contains(l, "level=error") {
			errLevelMatch = strings.TrimSpace(ln)
		}
	}
	if errLevelMatch != "" {
		return errLevelMatch
	}
	return match
}
