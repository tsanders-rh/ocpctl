package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// argValue returns the value following flag in argv, or "" if the flag is absent.
func argValue(argv []string, flag string) string {
	for i, a := range argv {
		if a == flag && i+1 < len(argv) {
			return argv[i+1]
		}
	}
	return ""
}

// Golden snapshots must be created encrypted with the *default* EBS KMS key —
// the same key the `encrypted: "true"` StorageClasses give the restored VM
// disk. Any mismatch (including unencrypted) makes EBS re-encrypt on restore,
// which breaks incremental-snapshot lineage and turns the first CSI backup into
// a full 70 GiB snapshot. See issue #198.
func TestGoldenSnapshotsAreCreatedEncrypted(t *testing.T) {
	t.Run("import-snapshot requests encryption", func(t *testing.T) {
		argv := importSnapshotArgs("us-east-1", "Windows 10 OADP v1.0",
			"ocpctl-binaries", "windows-images/windows-10-oadp.raw")

		assert.Contains(t, argv, "--encrypted",
			"golden snapshot import must be encrypted or CSI backups lose incrementality")
	})

	t.Run("same-region persistent copy requests encryption", func(t *testing.T) {
		argv := copySnapshotArgs("snap-source", "us-east-1", "us-east-1", "persistent copy")

		assert.Contains(t, argv, "--encrypted")
	})

	t.Run("cross-region copy requests encryption", func(t *testing.T) {
		argv := copySnapshotArgs("snap-source", "us-east-1", "us-west-2", "regional copy")

		assert.Contains(t, argv, "--encrypted")
	})
}

// --kms-key-id must stay absent so AWS falls back to the destination region's
// default EBS key. Pinning a key here would both re-break the lineage (the CSI
// driver would still use the default key) and require granting the vmimport
// service role access to it.
func TestGoldenSnapshotsUseDefaultEBSKey(t *testing.T) {
	t.Run("import-snapshot does not pin a KMS key", func(t *testing.T) {
		argv := importSnapshotArgs("us-east-1", "desc", "bucket", "key")

		assert.NotContains(t, argv, "--kms-key-id",
			"pinning a key diverges from the key the CSI StorageClass uses")
	})

	t.Run("copy-snapshot does not pin a KMS key", func(t *testing.T) {
		argv := copySnapshotArgs("snap-source", "us-east-1", "eu-west-1", "desc")

		assert.NotContains(t, argv, "--kms-key-id")
	})
}

func TestCopySnapshotArgsTargetCorrectRegions(t *testing.T) {
	argv := copySnapshotArgs("snap-0abc", "us-east-1", "us-west-2", "regional copy")

	assert.Equal(t, "snap-0abc", argValue(argv, "--source-snapshot-id"))
	assert.Equal(t, "us-east-1", argValue(argv, "--source-region"))
	assert.Equal(t, "us-west-2", argValue(argv, "--destination-region"))

	// The API call itself must be made against the destination region, otherwise
	// --encrypted resolves the default EBS key in the wrong region.
	assert.Equal(t, "us-west-2", argValue(argv, "--region"))

	require.GreaterOrEqual(t, len(argv), 2)
	assert.Equal(t, []string{"ec2", "copy-snapshot"}, argv[:2])
}

func TestImportSnapshotArgsBuildDiskContainer(t *testing.T) {
	argv := importSnapshotArgs("us-east-1", "Windows 10 OADP v1.0",
		"ocpctl-binaries", "windows-images/windows-10-oadp.raw")

	container := argValue(argv, "--disk-container")
	assert.Equal(t,
		"Format=RAW,UserBucket={S3Bucket=ocpctl-binaries,S3Key=windows-images/windows-10-oadp.raw}",
		container)

	assert.Equal(t, "us-east-1", argValue(argv, "--region"))
	require.GreaterOrEqual(t, len(argv), 2)
	assert.Equal(t, []string{"ec2", "import-snapshot"}, argv[:2])
}

// An unencrypted golden snapshot boots VMs perfectly well, so nothing else
// downstream rejects it — this gate is the only thing standing between a bad
// snapshot and every cluster that restores from it.
func TestCheckSnapshotEncrypted(t *testing.T) {
	t.Run("rejects an unencrypted snapshot", func(t *testing.T) {
		err := checkSnapshotEncrypted("snap-0059372671a43a646", false)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "snap-0059372671a43a646",
			"operator needs to know which snapshot was rejected")
		assert.Contains(t, err.Error(), "not encrypted")
	})

	t.Run("accepts an encrypted snapshot", func(t *testing.T) {
		assert.NoError(t, checkSnapshotEncrypted("snap-0abc", true))
	})
}
