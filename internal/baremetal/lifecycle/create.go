package lifecycle

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/agent"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/host"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/metal3"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/odf"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/rhwa"
	"github.com/tsanders-rh/ocpctl/internal/baremetal/substrate"
)

const hostReachableTimeout = 5 * time.Minute

// Create provisions a bare-metal / agent-based cluster end to end: launch the AWS
// substrate, provision the host (libvirt + sushy + domains), run the agent-based
// install, then configure metal3 BareMetalHosts, RHWA operators + fence_redfish
// fencing, and any post-install workers. All steps stream to out (the deployment
// log). Returns the verified kubeconfig and cluster URLs.
func Create(ctx context.Context, out io.Writer, in Input) (*Result, error) {
	user, pass, err := generateSushyCreds()
	if err != nil {
		return nil, err
	}

	sub, err := substrate.Launch(ctx, buildLaunchSpec(in))
	if err != nil {
		return nil, fmt.Errorf("baremetal create substrate: %w", err)
	}

	// The cluster trusts the ephemeral substrate key (piece-3 node access).
	sshKey := string(ssh.MarshalAuthorizedKey(sub.Signer.PublicKey()))
	installConfig, err := in.RenderInstallConfig(sshKey)
	if err != nil {
		return nil, fmt.Errorf("baremetal create render install-config: %w", err)
	}

	nodes := host.ComputeNodes(buildTopology(in))
	hs := buildHostSpec(in, nodes, user, pass)

	c := host.NewClient(sub.Addr, sub.User, sub.Signer, out)
	if err := c.WaitReachable(ctx, hostReachableTimeout); err != nil {
		return nil, fmt.Errorf("baremetal create host reachable: %w", err)
	}
	if err := c.Connect(ctx); err != nil {
		return nil, fmt.Errorf("baremetal create host connect: %w", err)
	}
	defer func() {
		if cerr := c.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "warning: close host client: %v\n", cerr)
		}
	}()

	// Place the ephemeral private key on the host so it can ssh into the cluster
	// nodes and the ceph VM (nested ssh). The target dir is created here since it
	// precedes host provisioning.
	if err := c.Run(ctx, "mkdir -p "+filepath.Dir(host.NodeKeyPath)); err != nil {
		return nil, fmt.Errorf("baremetal create node key dir: %w", err)
	}
	if err := c.Upload(ctx, bytes.NewReader(sub.PrivateKeyPEM), host.NodeKeyPath, 0o600); err != nil {
		return nil, fmt.Errorf("baremetal create stage node key: %w", err)
	}

	pr, err := host.Provision(ctx, c, &hs)
	if err != nil {
		return nil, fmt.Errorf("baremetal create provision host: %w", err)
	}

	is := buildInstallSpec(in, pr.Nodes, installConfig)
	creds, err := agent.Install(ctx, c, is)
	if err != nil {
		return nil, fmt.Errorf("baremetal create install: %w", err)
	}

	// Post-install: RHWA operators + fence_redfish fencing, then metal3 BMHs and
	// worker provisioning (rhwa-lab order: operators -> fencing -> bmh -> workers).
	rs := buildRHWASpec(in, pr.Nodes, pr.UUIDs, user, pass)
	if err := rhwa.InstallOperators(ctx, c, rs); err != nil {
		return nil, fmt.Errorf("baremetal create operators: %w", err)
	}
	if err := rhwa.ConfigureFencing(ctx, c, rs); err != nil {
		return nil, fmt.Errorf("baremetal create fencing: %w", err)
	}
	m3 := buildMetal3Spec(in, pr.Nodes, pr.UUIDs, user, pass)
	if err := metal3.ConfigureBMH(ctx, c, m3); err != nil {
		return nil, fmt.Errorf("baremetal create bmh: %w", err)
	}
	if err := metal3.ProvisionWorkers(ctx, c, m3); err != nil {
		return nil, fmt.Errorf("baremetal create workers: %w", err)
	}

	// ODF-external + single-VM Ceph, if the profile enables it. The ceph VM trusts
	// the ephemeral key (sshKey) so odf reaches it over the same host tunnel.
	if odfEnabled(in) {
		if err := odf.Setup(ctx, c, buildODFSpec(in, sshKey)); err != nil {
			return nil, fmt.Errorf("baremetal create odf: %w", err)
		}
	}

	return &Result{
		Kubeconfig:        creds.Kubeconfig,
		KubeadminPassword: creds.KubeadminPassword,
		APIURL:            creds.APIURL,
		ConsoleURL:        creds.ConsoleURL,
		Recovered:         creds.Recovered,
	}, nil
}
