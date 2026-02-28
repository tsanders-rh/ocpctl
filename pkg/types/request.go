package types

// CreateClusterRequest represents a cluster creation request to validate
type CreateClusterRequest struct {
	Name          string
	Platform      string
	Version       string
	Profile       string
	Region        string
	BaseDomain    string
	Owner         string
	Team          string
	CostCenter    string
	TTLHours      int
	SSHPublicKey  *string
	ExtraTags     map[string]string
	OffhoursOptIn bool
}
