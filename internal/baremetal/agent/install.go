package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
)

// Install renders the agent-config, builds the agent ISO once, boots the master
// VMs, drives openshift-install agent wait-for to completion, and returns a
// verified admin kubeconfig plus cluster URLs. openshift-install and oc for the
// cluster run on the host (only there are the node IPs routable); output streams
// to the host client's log writer.
func Install(ctx context.Context, c *host.Client, spec InstallSpec) (*Result, error) {
	return install(ctx, c, newNodeConnector(c), time.Sleep, spec)
}

func install(ctx context.Context, h hostRunner, nc nodeConnector, sleep func(time.Duration), spec InstallSpec) (*Result, error) {
	if spec.InstallTimeout == 0 {
		spec.InstallTimeout = defaultInstallTimeout
	}
	if err := hostTools(ctx, h, spec); err != nil {
		return nil, err
	}
	if err := uploadConfigs(ctx, h, spec); err != nil {
		return nil, err
	}
	if err := buildImage(ctx, h, spec); err != nil {
		return nil, err
	}
	if err := bootMasters(ctx, h, spec); err != nil {
		return nil, err
	}
	if err := waitInstall(ctx, h, nc, sleep, spec); err != nil {
		return nil, err
	}
	kubeconfig, kubeadmin, recovered, err := fetchCreds(ctx, h, nc, spec)
	if err != nil {
		return nil, err
	}
	waitOperators(ctx, h, sleep)

	base := strings.TrimSuffix(spec.BaseDomain, ".")
	return &Result{
		Kubeconfig:        kubeconfig,
		KubeadminPassword: kubeadmin,
		APIURL:            "https://api." + spec.ClusterName + "." + base + ":6443",
		ConsoleURL:        "https://console-openshift-console.apps." + spec.ClusterName + "." + base,
		Recovered:         recovered,
	}, nil
}

// hostTools downloads openshift-install + oc for the requested version onto the
// host (os_host_tools).
func hostTools(ctx context.Context, h hostRunner, spec InstallSpec) error {
	base := mirrorBase + "/" + spec.OCPVersion
	script := strings.Join([]string{
		"set -euo pipefail",
		"mkdir -p " + remoteBinDir + " && cd " + remoteBinDir,
		"curl -fsSL " + shQuote(base+"/openshift-install-linux.tar.gz") + " | tar xz openshift-install",
		"curl -fsSL " + shQuote(base+"/openshift-client-linux.tar.gz") + " | tar xz oc kubectl",
		// openshift-install (agent create image) shells out to `oc`, so it must be
		// on PATH — symlink the tools into /usr/local/bin.
		"ln -sf " + remoteBinDir + "/oc /usr/local/bin/oc",
		"ln -sf " + remoteBinDir + "/kubectl /usr/local/bin/kubectl",
		"ln -sf " + remoteBinDir + "/openshift-install /usr/local/bin/openshift-install",
		"./openshift-install version",
	}, "\n")
	if err := h.Run(ctx, script); err != nil {
		return fmt.Errorf("agent host tools: %w", err)
	}
	return nil
}

// uploadConfigs stages install-config.yaml (from the caller) and the rendered
// agent-config.yaml on the host (os_render_configs).
func uploadConfigs(ctx context.Context, h hostRunner, spec InstallSpec) error {
	if err := h.Run(ctx, "mkdir -p "+RemoteWorkDir+"/orig"); err != nil {
		return fmt.Errorf("agent stage dir: %w", err)
	}
	if err := h.Upload(ctx, bytes.NewReader(spec.InstallConfig), RemoteWorkDir+"/orig/install-config.yaml", 0o600); err != nil {
		return fmt.Errorf("agent upload install-config: %w", err)
	}
	agentCfg, err := renderAgentConfig(spec)
	if err != nil {
		return fmt.Errorf("agent render config: %w", err)
	}
	if err := h.Upload(ctx, strings.NewReader(agentCfg), RemoteWorkDir+"/orig/agent-config.yaml", 0o600); err != nil {
		return fmt.Errorf("agent upload agent-config: %w", err)
	}
	return nil
}

// buildImage builds the agent ISO exactly once (os_build_image). Rebuilding after
// nodes have booted would orphan the cluster's certs, so a present marker (ISO +
// work/auth/kubeconfig) is reused.
func buildImage(ctx context.Context, h hostRunner, spec InstallSpec) error {
	iso := "/var/lib/libvirt/images/" + spec.ClusterName + "-agent.iso"
	check := "sudo bash -c " + shQuote("test -f "+RemoteKubeconfig+" && test -f "+iso) + " && echo REUSE || echo BUILD"
	out, err := h.RunCapture(ctx, check)
	if err != nil {
		return fmt.Errorf("agent build-image check: %w", err)
	}
	if strings.TrimSpace(out) == "REUSE" {
		return nil
	}
	build := strings.Join([]string{
		"set -euo pipefail",
		// agent create image invokes `oc` for release extraction.
		"export PATH=" + remoteBinDir + ":/usr/local/bin:$PATH",
		"cd " + RemoteWorkDir,
		"rm -rf work && mkdir -p work",
		"cp -f orig/install-config.yaml orig/agent-config.yaml work/",
		remoteBinDir + "/openshift-install --dir work agent create image --log-level=info",
		"cp work/agent.x86_64.iso " + shQuote(iso),
		"chmod 644 " + shQuote(iso),
	}, "\n")
	if err := h.Run(ctx, build); err != nil {
		return fmt.Errorf("agent build image: %w", err)
	}
	return nil
}

// bootMasters powers on the master domains from the agent ISO (vms_boot).
func bootMasters(ctx context.Context, h hostRunner, spec InstallSpec) error {
	for _, vm := range masters(spec) {
		st, err := h.RunCapture(ctx, "sudo virsh domstate "+shQuote(vm.Name))
		if err != nil {
			return fmt.Errorf("agent domstate %s: %w", vm.Name, err)
		}
		if strings.TrimSpace(st) == "running" {
			continue
		}
		if err := h.Run(ctx, "sudo virsh start "+shQuote(vm.Name)); err != nil {
			return fmt.Errorf("agent boot %s: %w", vm.Name, err)
		}
	}
	return nil
}

// waitInstall runs openshift-install agent wait-for through to completion
// (os_wait_install), re-attaching across wait-for's own internal timeout until
// the budget is spent. When a node connector is available it also cross-checks
// the cluster's recovery kubeconfig so a wedged wait-for on an already-up cluster
// breaks out.
func waitInstall(ctx context.Context, h hostRunner, nc nodeConnector, sleep func(time.Duration), spec InstallSpec) error {
	deadline := time.Now().Add(spec.InstallTimeout)

	if clusterAvailableViaRecovery(ctx, nc, spec.RendezvousIP) {
		return nil
	}

	for {
		if err := h.Run(ctx, waitForCmd("bootstrap-complete")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent wait bootstrap-complete: budget exhausted")
		}
		sleep(5 * time.Second)
	}

	for {
		if err := h.Run(ctx, waitForCmd("install-complete")); err == nil {
			break
		}
		if clusterAvailableViaRecovery(ctx, nc, spec.RendezvousIP) {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent wait install-complete: budget exhausted")
		}
		sleep(5 * time.Second)
	}
	return nil
}

func waitForCmd(phase string) string {
	return "cd " + RemoteWorkDir + " && " + remoteBinDir + "/openshift-install --dir work agent wait-for " + phase + " --log-level=info"
}

// waitOperators waits (best-effort) for cluster operators to stabilize
// (os_wait_cluster_ready). A timeout here is not fatal — operators may still be
// settling — matching rhwa-lab.
func waitOperators(ctx context.Context, h hostRunner, sleep func(time.Duration)) {
	const cmd = "export KUBECONFIG=" + RemoteKubeconfig + "; " +
		RemoteOC + " wait --for=condition=Available=True clusterversion/version --timeout=30s >/dev/null 2>&1"
	for i := 0; i < 60; i++ {
		if err := h.Run(ctx, cmd); err == nil {
			return
		}
		sleep(20 * time.Second)
	}
}

// shQuote single-quotes a value for safe interpolation into a remote shell script.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
