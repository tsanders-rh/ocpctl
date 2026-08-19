package installer

import (
	"encoding/json"
	"testing"
)

// TestAROCredentialsUnmarshal guards the JSON tags against the exact field names
// emitted by `az aro list-credentials -o json`. A typo here would silently yield
// empty credentials, which is the failure mode #97 fixes.
func TestAROCredentialsUnmarshal(t *testing.T) {
	// Representative output of `az aro list-credentials --output json`.
	raw := `{"kubeadminUsername": "kubeadmin", "kubeadminPassword": "Abcd1-2Efgh-3Ijkl-4Mnop"}`

	var creds AROCredentials
	if err := json.Unmarshal([]byte(raw), &creds); err != nil {
		t.Fatalf("unmarshal aro credentials: %v", err)
	}

	if creds.KubeadminUsername != "kubeadmin" {
		t.Errorf("KubeadminUsername = %q, want %q", creds.KubeadminUsername, "kubeadmin")
	}
	if creds.KubeadminPassword != "Abcd1-2Efgh-3Ijkl-4Mnop" {
		t.Errorf("KubeadminPassword = %q, want non-empty password", creds.KubeadminPassword)
	}
}
