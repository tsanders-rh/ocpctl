package odf

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A representative rook exporter array: a StorageClass with bare *-secret-name
// refs, a Secret, and a ConfigMap (with a numeric-ish value).
const exporterJSON = `[
  {"name":"rook-ceph-mon-endpoints","kind":"ConfigMap","data":{"data":"a=1.2.3.4:6789","maxMonId":"0","mapping":"{}"}},
  {"name":"rook-csi-rbd-provisioner","kind":"Secret","data":{"userID":"csi-rbd-provisioner","userKey":"AQAA=="}},
  {"name":"ceph-rbd","kind":"StorageClass","data":{"pool":"ocs-storagepool","csi.storage.k8s.io/provisioner-secret-name":"rook-csi-rbd-provisioner","csi.storage.k8s.io/node-publish-secret-name":"rook-csi-rbd-node"}}
]`

func TestBuildImportManifests_NormalizesAndSplits(t *testing.T) {
	listJSON, blobJSON, err := buildImportManifests([]byte(exporterJSON), "openshift-storage")
	require.NoError(t, err)

	// --- List: only Secret + ConfigMap objects, namespaced, string values ---
	var list struct {
		Kind  string `json:"kind"`
		Items []struct {
			Kind       string            `json:"kind"`
			Metadata   map[string]string `json:"metadata"`
			StringData map[string]string `json:"stringData"`
			Data       map[string]string `json:"data"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(listJSON, &list))
	assert.Equal(t, "List", list.Kind)
	require.Len(t, list.Items, 2) // ConfigMap + Secret; the StorageClass is not applied as an object

	byKind := map[string]int{}
	for i, it := range list.Items {
		byKind[it.Kind] = i
		assert.Equal(t, "openshift-storage", it.Metadata["namespace"])
	}
	require.Contains(t, byKind, "Secret")
	require.Contains(t, byKind, "ConfigMap")
	assert.Equal(t, "AQAA==", list.Items[byKind["Secret"]].StringData["userKey"])
	assert.Equal(t, "a=1.2.3.4:6789", list.Items[byKind["ConfigMap"]].Data["data"])

	// --- Blob: base64 of the normalized array; StorageClass gained namespaces ---
	var blob struct {
		Metadata map[string]string `json:"metadata"`
		Type     string            `json:"type"`
		Data     map[string]string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(blobJSON, &blob))
	assert.Equal(t, "rook-ceph-external-cluster-details", blob.Metadata["name"])
	assert.Equal(t, "openshift-storage", blob.Metadata["namespace"])

	decoded, err := base64.StdEncoding.DecodeString(blob.Data["external_cluster_details"])
	require.NoError(t, err)
	var arr []map[string]interface{}
	require.NoError(t, json.Unmarshal(decoded, &arr))
	var sc map[string]interface{}
	for _, it := range arr {
		if it["kind"] == "StorageClass" {
			sc = it["data"].(map[string]interface{})
		}
	}
	require.NotNil(t, sc)
	// every *-secret-name gained a matching *-secret-namespace = the ODF namespace
	assert.Equal(t, "openshift-storage", sc["csi.storage.k8s.io/provisioner-secret-namespace"])
	assert.Equal(t, "openshift-storage", sc["csi.storage.k8s.io/node-publish-secret-namespace"])
	// original refs preserved
	assert.Equal(t, "rook-csi-rbd-provisioner", sc["csi.storage.k8s.io/provisioner-secret-name"])
}

func TestBuildImportManifests_RejectsEmpty(t *testing.T) {
	_, _, err := buildImportManifests([]byte(`[]`), "openshift-storage")
	require.Error(t, err)
	_, _, err = buildImportManifests([]byte(`not json`), "openshift-storage")
	require.Error(t, err)
}
