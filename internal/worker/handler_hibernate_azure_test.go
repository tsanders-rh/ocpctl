package worker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAzureResourceGroupFromMetadata(t *testing.T) {
	write := func(t *testing.T, contents string) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "metadata.json"), []byte(contents), 0o600); err != nil {
			t.Fatalf("write metadata.json: %v", err)
		}
		return dir
	}

	t.Run("falls back to <infraID>-rg when resourceGroupName is empty", func(t *testing.T) {
		// Reproduces the tsanders-azure-e failure: the installer creates its own
		// resource group, so metadata records an empty resourceGroupName and the
		// group is named "<infraID>-rg".
		dir := write(t, `{
			"infraID": "tsanders-azure-e-l7dx8",
			"azure": {"resourceGroupName": "", "baseDomainResourceGroupName": "azure-mg-dog8code-com-dns"}
		}`)
		rg, err := azureResourceGroupFromMetadata(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rg != "tsanders-azure-e-l7dx8-rg" {
			t.Fatalf("expected tsanders-azure-e-l7dx8-rg, got %q", rg)
		}
	})

	t.Run("uses explicit resourceGroupName when set", func(t *testing.T) {
		dir := write(t, `{
			"infraID": "abc-123",
			"azure": {"resourceGroupName": "my-preexisting-rg"}
		}`)
		rg, err := azureResourceGroupFromMetadata(dir)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rg != "my-preexisting-rg" {
			t.Fatalf("expected my-preexisting-rg, got %q", rg)
		}
	})

	t.Run("errors when neither resourceGroupName nor infraID is present", func(t *testing.T) {
		dir := write(t, `{"azure": {"resourceGroupName": ""}}`)
		if _, err := azureResourceGroupFromMetadata(dir); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	t.Run("errors when metadata.json is missing", func(t *testing.T) {
		if _, err := azureResourceGroupFromMetadata(t.TempDir()); err == nil {
			t.Fatal("expected error, got nil")
		}
	})
}
