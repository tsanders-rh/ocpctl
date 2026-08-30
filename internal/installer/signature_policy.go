package installer

import (
	"encoding/base64"
	"fmt"
)

// permissiveContainerPolicy is the /etc/containers/policy.json contents used for
// prerelease/nightly installs.
//
// Nightly OpenShift payload images (quay.io/openshift-release-dev/ocp-v4.0-art-dev,
// pulled by digest during node firstboot) are UNSIGNED. A default cluster's
// policy.json, however, enforces `sigstoreSigned` (Red Hat's production key) on the
// release repos, so the machine-config-daemon firstboot pull is rejected with
// "Source image rejected: A signature was required, but no signature exists". The
// node then never joins and bootstrapping times out at cb-bootstrap. GA payload
// images ARE signed, so GA installs are unaffected — this policy MUST only be used
// for prerelease/nightly installs.
//
// The permissive policy disables signature enforcement (equivalent to the
// pre-signature-verification default), matching what was proven to unblock the
// firstboot pull on master-0 during debugging.
const permissiveContainerPolicy = `{
  "default": [{"type": "insecureAcceptAnything"}],
  "transports": {
    "docker-daemon": {"": [{"type": "insecureAcceptAnything"}]}
  }
}
`

// permissiveImagePolicyMCTemplate renders a MachineConfig that overwrites
// /etc/containers/policy.json on nodes of a given role. openshift-install merges
// user manifests dropped into the asset dir's openshift/ subdir into the rendered
// Ignition/MachineConfig set, so the file is present via Ignition on firstboot —
// before machine-config-daemon-pull.service runs. Ignition file mode 420 == 0644.
const permissiveImagePolicyMCTemplate = `apiVersion: machineconfiguration.openshift.io/v1
kind: MachineConfig
metadata:
  labels:
    machineconfiguration.openshift.io/role: %s
  name: 99-%s-ocpctl-permissive-image-policy
spec:
  config:
    ignition:
      version: 3.2.0
    storage:
      files:
        - path: /etc/containers/policy.json
          mode: 420
          overwrite: true
          contents:
            source: data:text/plain;charset=utf-8;base64,%s
`

// PermissiveImagePolicyManifests returns MachineConfig manifests (filename ->
// content) that relax container image signature verification on all node roles.
// Intended ONLY for prerelease/nightly installs (see permissiveContainerPolicy).
func PermissiveImagePolicyManifests() map[string]string {
	b64 := base64.StdEncoding.EncodeToString([]byte(permissiveContainerPolicy))
	manifests := make(map[string]string, 2)
	for _, role := range []string{"master", "worker"} {
		name := fmt.Sprintf("99-%s-ocpctl-permissive-image-policy.yaml", role)
		manifests[name] = fmt.Sprintf(permissiveImagePolicyMCTemplate, role, role, b64)
	}
	return manifests
}
