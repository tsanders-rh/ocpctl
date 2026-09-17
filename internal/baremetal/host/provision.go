package host

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Provision brings a freshly-launched host up to "provisioned": libvirt +
// sushy-tools + haproxy installed and running, the libvirt NAT network defined
// with pinned reservations, and every domain defined (not started). The caller
// opens c (WaitReachable + Connect) beforehand. Steps run in rhwa-lab's order
// and preserve its idempotency guards, so re-running a failed create is safe.
func Provision(ctx context.Context, c *Client, spec *HostSpec) (*Result, error) {
	steps := []struct {
		name   string
		render func() (string, error)
	}{
		{"packages", renderPackages},
		{"storage pool", renderStoragePool},
	}
	for _, s := range steps {
		script, err := s.render()
		if err != nil {
			return nil, err
		}
		c.logf("=== %s ===", s.name)
		if err := c.Run(ctx, script); err != nil {
			return nil, fmt.Errorf("provision host %s: %w", s.name, err)
		}
	}

	// libvirt network: upload XML, then define.
	c.logf("=== libvirt network ===")
	netXML, err := renderLibvirtNetXML(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Upload(ctx, strings.NewReader(netXML), remoteNetXMLPath, 0o644); err != nil {
		return nil, fmt.Errorf("provision host libvirt net xml: %w", err)
	}
	netDefine, err := renderLibvirtNetDefine(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, netDefine); err != nil {
		return nil, fmt.Errorf("provision host libvirt net: %w", err)
	}

	// haproxy: upload cfg, then enable/restart.
	c.logf("=== haproxy ===")
	haCfg, err := renderHAProxyCfg(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Upload(ctx, strings.NewReader(haCfg), remoteHAProxyCfgPath, 0o644); err != nil {
		return nil, fmt.Errorf("provision host haproxy cfg: %w", err)
	}
	haReload, err := renderHAProxyReload()
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, haReload); err != nil {
		return nil, fmt.Errorf("provision host haproxy: %w", err)
	}

	// sushy-tools + health check.
	c.logf("=== sushy-tools ===")
	sushy, err := renderSushy(spec)
	if err != nil {
		return nil, err
	}
	if err := c.Run(ctx, sushy); err != nil {
		return nil, fmt.Errorf("provision host sushy: %w", err)
	}
	if err := c.waitSushy(ctx, spec); err != nil {
		return nil, err
	}

	// define domains (not started).
	c.logf("=== define domains ===")
	for _, vm := range spec.Nodes {
		script, err := renderDomain(spec, vm)
		if err != nil {
			return nil, err
		}
		if err := c.Run(ctx, script); err != nil {
			return nil, fmt.Errorf("provision host domain %s: %w", vm.Name, err)
		}
	}

	// record UUIDs (Redfish system ids).
	uuids := make(map[string]string, len(spec.Nodes))
	for _, vm := range spec.Nodes {
		uuid, err := c.RunCapture(ctx, fmt.Sprintf("sudo virsh domuuid %s", shQuote(vm.Name)))
		if err != nil {
			return nil, fmt.Errorf("provision host domuuid %s: %w", vm.Name, err)
		}
		uuids[vm.Name] = uuid
	}

	return &Result{UUIDs: uuids, Nodes: spec.Nodes}, nil
}

// waitSushy polls the emulator's Redfish endpoint; on final failure it pulls the
// container logs into the deployment log before failing, mirroring rhwa-lab.
func (c *Client) waitSushy(ctx context.Context, spec *HostSpec) error {
	return c.waitSushyN(ctx, spec, 12, 5*time.Second)
}

func (c *Client) waitSushyN(ctx context.Context, spec *HostSpec, n int, delay time.Duration) error {
	health := fmt.Sprintf("curl -sk -u %s https://%s:%d/redfish/v1/Systems >/dev/null",
		shQuote(spec.SushyUser+":"+spec.SushyPass), spec.NetGateway, spec.SushyPort)
	err := retry(n, delay, func() error {
		_, e := c.RunCapture(ctx, health)
		return e
	})
	if err != nil {
		logs, lerr := c.RunCapture(ctx, "sudo podman logs --tail 30 sushy 2>&1 || true")
		if lerr != nil {
			logs = "(failed to fetch sushy container logs: " + lerr.Error() + ")"
		}
		c.logf("sushy-tools did not answer; last container logs:\n%s", logs)
		return fmt.Errorf("provision host sushy: not serving Redfish on %s:%d: %w", spec.NetGateway, spec.SushyPort, err)
	}
	return nil
}

func (c *Client) logf(format string, args ...any) {
	if _, err := fmt.Fprintln(c.out, fmt.Sprintf(format, args...)); err != nil {
		fmt.Fprintf(os.Stderr, "warning: log write failed: %v\n", err)
	}
}

func retry(n int, delay time.Duration, fn func() error) error {
	var err error
	for i := 0; i < n; i++ {
		if err = fn(); err == nil {
			return nil
		}
		if i < n-1 {
			time.Sleep(delay)
		}
	}
	return err
}
