package odf

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// buildImportManifests turns the rook external-cluster exporter's JSON array into
// the two things the OCP console importer applies for external mode:
//
//  1. a v1/List of the individual rook-ceph-* Secret/ConfigMap objects (the
//     rook-ceph-mon-endpoints ConfigMap in particular is not recreated by
//     ocs-operator), and
//  2. the rook-ceph-external-cluster-details blob Secret (base64 of the whole
//     array) the StorageCluster's external controller ingests.
//
// Before emitting either, it NORMALIZES the array: the exporter emits each
// StorageClass secret ref as a bare "*-secret-name" with no "*-secret-namespace",
// which makes ocs-operator generate an invalid StorageClass. For every
// "*-secret-name" in a StorageClass's data it adds the matching
// "*-secret-namespace"=namespace. Ports odf.sh's odf_import_external jq.
func buildImportManifests(raw []byte, namespace string) (listJSON, blobSecretJSON []byte, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // keep numbers exact when stringified into Secret/ConfigMap data
	var items []map[string]interface{}
	if err := dec.Decode(&items); err != nil {
		return nil, nil, fmt.Errorf("parse external ceph details: %w", err)
	}
	if len(items) == 0 {
		return nil, nil, fmt.Errorf("external ceph details are empty")
	}

	normalizeSecretNamespaces(items, namespace)

	normalized, err := json.Marshal(items)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal normalized details: %w", err)
	}

	listItems := []interface{}{}
	for _, it := range items {
		kind, kok := it["kind"].(string)
		name, nok := it["name"].(string)
		data, dok := it["data"].(map[string]interface{})
		if !kok || !nok || !dok {
			continue
		}
		switch kind {
		case "Secret":
			listItems = append(listItems, map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "Secret",
				"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
				"stringData": stringifyValues(data),
			})
		case "ConfigMap":
			listItems = append(listItems, map[string]interface{}{
				"apiVersion": "v1",
				"kind":       "ConfigMap",
				"metadata":   map[string]interface{}{"name": name, "namespace": namespace},
				"data":       stringifyValues(data),
			})
		}
	}
	list := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "List",
		"items":      listItems,
	}
	listJSON, err = json.Marshal(list)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal import list: %w", err)
	}

	blob := map[string]interface{}{
		"apiVersion": "v1",
		"kind":       "Secret",
		"metadata":   map[string]interface{}{"name": "rook-ceph-external-cluster-details", "namespace": namespace},
		"type":       "Opaque",
		"data":       map[string]interface{}{"external_cluster_details": base64.StdEncoding.EncodeToString(normalized)},
	}
	blobSecretJSON, err = json.Marshal(blob)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal blob secret: %w", err)
	}
	return listJSON, blobSecretJSON, nil
}

// normalizeSecretNamespaces adds a "*-secret-namespace"=namespace for every
// "*-secret-name" key in each StorageClass item's data.
func normalizeSecretNamespaces(items []map[string]interface{}, namespace string) {
	for _, it := range items {
		if kind, ok := it["kind"].(string); !ok || kind != "StorageClass" {
			continue
		}
		data, ok := it["data"].(map[string]interface{})
		if !ok {
			continue
		}
		for k := range data {
			if strings.HasSuffix(k, "-secret-name") {
				nsKey := strings.TrimSuffix(k, "-secret-name") + "-secret-namespace"
				if _, exists := data[nsKey]; !exists {
					data[nsKey] = namespace
				}
			}
		}
	}
}

// stringifyValues renders every value as a string (Secret stringData / ConfigMap
// data require string values), matching odf.sh's `.data|map_values(tostring)`.
func stringifyValues(data map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(data))
	for k, v := range data {
		out[k] = toString(v)
	}
	return out
}

func toString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}
