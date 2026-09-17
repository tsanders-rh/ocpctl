package agent

import (
	"context"
	"fmt"
	"strings"
)

// recoveredKubeadminSentinel is stored as the kubeadmin password when the
// installer's own kubeconfig was orphaned and a recovery kubeconfig was used
// instead (there is no matching kubeadmin password in that case).
const recoveredKubeadminSentinel = "<unavailable — recovered kubeconfig; use it for oc access>"

// fetchCreds returns a kubeconfig that is verified to authenticate (os_fetch_creds).
// It prefers the installer-generated kubeconfig on the host, but verifies it
// actually works; if it does not (orphaned by a cert-generation mismatch) and a
// node connector is available, it recovers a working cluster-admin kubeconfig
// from a master's always-trusted recovery kubeconfig.
func fetchCreds(ctx context.Context, h hostRunner, nc nodeConnector, spec InstallSpec) (kubeconfig, kubeadmin []byte, recovered bool, err error) {
	verifyErr := h.Run(ctx, "export KUBECONFIG="+RemoteKubeconfig+"; "+RemoteOC+" get clusterversion >/dev/null 2>&1")
	if verifyErr == nil {
		kc, err := h.RunCapture(ctx, "sudo cat "+RemoteKubeconfig)
		if err != nil {
			return nil, nil, false, fmt.Errorf("agent fetch kubeconfig: %w", err)
		}
		pw, err := h.RunCapture(ctx, "sudo cat "+remoteAuthDir+"/kubeadmin-password")
		if err != nil {
			return nil, nil, false, fmt.Errorf("agent fetch kubeadmin: %w", err)
		}
		return []byte(strings.TrimRight(kc, "\n") + "\n"), []byte(pw), false, nil
	}

	if nc == nil {
		return nil, nil, false, fmt.Errorf("agent creds: installer kubeconfig failed to authenticate and no recovery path is available: %w", verifyErr)
	}
	kc, err := recoverKubeconfig(ctx, nc, spec.RendezvousIP, apiFQDN(spec))
	if err != nil {
		return nil, nil, false, fmt.Errorf("agent recover kubeconfig: %w", err)
	}
	return kc, []byte(recoveredKubeadminSentinel), true, nil
}

func apiFQDN(spec InstallSpec) string {
	return "api." + spec.ClusterName + "." + strings.TrimSuffix(spec.BaseDomain, ".")
}
