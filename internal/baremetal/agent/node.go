package agent

import (
	"context"
	"strings"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

// nodeConnector runs a command on a cluster node, tunneled through the host
// connection. It is nil when node access is unavailable (no recovery path).
type nodeConnector interface {
	runOnNode(ctx context.Context, nodeIP, cmd string) (string, error)
}

// newNodeConnector returns a connector that tunnels through the host client's
// SSH connection, authenticating to nodes with the host signer (the ephemeral
// substrate key, which every node trusts via install-config sshKey).
func newNodeConnector(c *host.Client) nodeConnector {
	if c == nil {
		return nil
	}
	return &hostNodeConnector{c: c}
}

// hostNodeConnector runs commands on a node by tunneling through the host.
type hostNodeConnector struct{ c *host.Client }

func (n *hostNodeConnector) runOnNode(ctx context.Context, nodeIP, cmd string) (string, error) {
	// Reached by nested ssh from the host (core = RHCOS node login).
	return n.c.RunNodeCapture(ctx, nodeIP, "core", cmd)
}

// clusterAvailableViaRecovery reports whether ClusterVersion is Available=True,
// read from a master's always-trusted recovery kubeconfig. Any error (or a nil
// connector) yields false.
func clusterAvailableViaRecovery(ctx context.Context, nc nodeConnector, nodeIP string) bool {
	if nc == nil {
		return false
	}
	cmd := "sudo /usr/bin/oc --kubeconfig=" + recoveryKC +
		` get clusterversion version -o jsonpath='{.status.conditions[?(@.type=="Available")].status}'`
	out, err := nc.runOnNode(ctx, nodeIP, cmd)
	return err == nil && strings.TrimSpace(out) == "True"
}

// recoverKubeconfig fetches a master's recovery kubeconfig and repoints it at the
// public API with TLS verification disabled, yielding a working cluster-admin
// kubeconfig even when the installer's own kubeconfig was orphaned.
func recoverKubeconfig(ctx context.Context, nc nodeConnector, nodeIP, apiFQDN string) ([]byte, error) {
	raw, err := nc.runOnNode(ctx, nodeIP, "sudo cat "+recoveryKC)
	if err != nil {
		return nil, err
	}
	return []byte(rewriteRecoveryKubeconfig(raw, apiFQDN)), nil
}

// rewriteRecoveryKubeconfig ports os_fetch_creds's sed: point the server at the
// public API FQDN, replace the CA data with insecure-skip-tls-verify, and drop
// any tls-server-name line.
func rewriteRecoveryKubeconfig(raw, apiFQDN string) string {
	var b strings.Builder
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		indent := line[:len(line)-len(strings.TrimLeft(line, " "))]
		switch {
		case strings.Contains(line, "server: https://localhost:6443"):
			b.WriteString(indent + "server: https://" + apiFQDN + ":6443\n")
		case strings.HasPrefix(trimmed, "certificate-authority-data:"):
			b.WriteString(indent + "insecure-skip-tls-verify: true\n")
		case strings.HasPrefix(trimmed, "tls-server-name:"):
			// drop
		default:
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
