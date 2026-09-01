package types

// CreateClusterRequest represents a cluster creation request to validate
type CreateClusterRequest struct {
	Name              string
	Platform          string
	ClusterType       string
	Version           string
	Profile           string
	Region            string
	BaseDomain        string
	Owner             string
	Team              string
	CostCenter        string
	TTLHours          int
	SSHPublicKey      *string
	ExtraTags         map[string]string
	OffhoursOptIn     bool
	WorkHoursEnabled  *bool
	WorkHours         *WorkHoursSchedule
	PreserveOnFailure bool
	CredentialsMode   *string

	// Azure capacity pre-flight overrides. These are NOT set from API requests;
	// the worker populates them after probing AzureConfig.CapacityFallback so the
	// rendered install-config pins the create to a region/zone/SKU combination
	// with real allocation capacity. Region above is also overwritten to match.
	AzureZones            []string
	AzureControlPlaneType string
	AzureWorkerType       string
}
