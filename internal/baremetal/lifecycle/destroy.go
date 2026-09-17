package lifecycle

import (
	"context"
	"fmt"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/substrate"
)

// Destroy tears down the cluster's AWS substrate by tag. Terminating the host
// takes the libvirt VMs, sushy-tools and haproxy with it; Teardown also removes
// the security group, Elastic IP, keypair, and Route53 records.
func Destroy(ctx context.Context, in DestroyInput) error {
	if err := substrate.Teardown(ctx, substrate.TeardownSpec{
		Region:      in.Region,
		ClusterName: in.ClusterName,
		BaseDomain:  in.BaseDomain,
		ZoneID:      in.ZoneID,
	}); err != nil {
		return fmt.Errorf("baremetal destroy: %w", err)
	}
	return nil
}
