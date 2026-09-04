// Package orphan holds the shared safety policy for deleting orphaned cloud
// resources. It is deliberately independent of the API and janitor packages so
// both the manual admin-delete path and (later) automated janitor remediation
// enforce the exact same gate before anything is torn down.
package orphan

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// Config controls the orphaned-resource deletion safety gate.
type Config struct {
	// MinAge is how long a resource must have been continuously detected as
	// orphaned before it may be deleted. Guards against create/destroy races
	// where a mid-flight cluster's resources briefly look orphaned.
	MinAge time.Duration
	// MinDetections is the minimum number of detection cycles a resource must
	// have appeared in before it may be deleted.
	MinDetections int
	// CheckVPCEmpty enables the (VPC-only) live-infrastructure probe.
	CheckVPCEmpty bool
}

// DefaultConfig returns conservative defaults.
func DefaultConfig() Config {
	return Config{MinAge: 24 * time.Hour, MinDetections: 2, CheckVPCEmpty: true}
}

// ConfigFromEnv builds a Config from environment variables, falling back to
// DefaultConfig for anything unset or unparseable:
//
//	ORPHAN_DELETE_MIN_AGE_HOURS   (int hours, default 24)
//	ORPHAN_DELETE_MIN_DETECTIONS  (int, default 2)
//	ORPHAN_DELETE_CHECK_VPC_EMPTY (bool, default true)
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if v := os.Getenv("ORPHAN_DELETE_MIN_AGE_HOURS"); v != "" {
		if h, err := strconv.Atoi(v); err == nil && h >= 0 {
			cfg.MinAge = time.Duration(h) * time.Hour
		}
	}
	if v := os.Getenv("ORPHAN_DELETE_MIN_DETECTIONS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			cfg.MinDetections = n
		}
	}
	if v := os.Getenv("ORPHAN_DELETE_CHECK_VPC_EMPTY"); v != "" {
		cfg.CheckVPCEmpty = strings.EqualFold(v, "true") || v == "1"
	}
	return cfg
}

// VPCLiveCounts summarizes live infrastructure found inside a VPC.
type VPCLiveCounts struct {
	RunningInstances int `json:"running_instances"`
	NATGateways      int `json:"nat_gateways"`
	LoadBalancers    int `json:"load_balancers"`
}

// HasLive reports whether the VPC still hosts any live infrastructure.
func (c VPCLiveCounts) HasLive() bool {
	return c.RunningInstances > 0 || c.NATGateways > 0 || c.LoadBalancers > 0
}

// Verdict is the result of evaluating the safety gate for one resource.
type Verdict struct {
	// Safe is true only when there are no block reasons.
	Safe bool `json:"safe"`
	// BlockReasons lists every reason the resource must not be deleted. All
	// checks run (no short-circuit) so operators see the full picture at once.
	BlockReasons []string `json:"block_reasons,omitempty"`
	// Warnings are non-blocking notes (e.g. a lookup that could not complete).
	Warnings []string `json:"warnings,omitempty"`
	// SourceClusterStatus, when set, is the status of the resolved source cluster.
	SourceClusterStatus *string `json:"source_cluster_status,omitempty"`
	// VPCLive, when set, is the live-infrastructure probe result for a VPC.
	VPCLive *VPCLiveCounts `json:"vpc_live,omitempty"`
}

// ClusterLookup resolves cluster status for the live-cluster guard.
type ClusterLookup interface {
	// ClusterStatusByID returns the status of the cluster with the given id;
	// found is false when no such cluster exists.
	ClusterStatusByID(ctx context.Context, id string) (status types.ClusterStatus, found bool, err error)
	// MostRecentClusterStatusByName returns the status of the most recently
	// created cluster with the given name (any status); found is false when none
	// exists.
	MostRecentClusterStatusByName(ctx context.Context, name string) (status types.ClusterStatus, found bool, err error)
}

// VPCInspector probes a VPC for live infrastructure.
type VPCInspector interface {
	InspectVPC(ctx context.Context, vpcID, region string) (VPCLiveCounts, error)
}

// IsLiveStatus reports whether a cluster in the given status could still own
// cloud resources. Only DESTROYED, DESTROY_FAILED, and FAILED clusters are
// considered gone (their leftover resources are genuinely leaked); every other
// status -- including unknown/future ones -- is treated as live, so the gate
// fails safe by refusing to delete resources that might still be in use.
func IsLiveStatus(s types.ClusterStatus) bool {
	switch s {
	case types.ClusterStatusDestroyed, types.ClusterStatusDestroyFailed, types.ClusterStatusFailed:
		return false
	default:
		return true
	}
}

// Evaluate runs the full safety gate for a single orphaned resource and returns
// a Verdict. It never deletes anything. now is passed in for deterministic
// testing. cl and vpc may be nil (their checks are skipped, with a warning).
//
// The four checks, in order (all run; reasons accumulate):
//  1. Live-cluster guard  -- refuse if the source cluster (by cluster_id, else
//     by name) is still live. This is the primary guard against deleting a
//     running cluster's infrastructure.
//  2. Grace period        -- refuse if detected too recently or too few times.
//  3. Status guard        -- refuse unless the resource is ACTIVE.
//  4. VPC-alive probe      -- (VPC only) refuse if the VPC still hosts live
//     instances / NAT gateways / load balancers.
func Evaluate(ctx context.Context, res *types.OrphanedResource, cfg Config, now time.Time, cl ClusterLookup, vpc VPCInspector) Verdict {
	var v Verdict

	// 1. Live-cluster guard.
	var srcStatus types.ClusterStatus
	var srcFound bool
	if cl != nil {
		if res.ClusterID != nil && *res.ClusterID != "" {
			st, found, err := cl.ClusterStatusByID(ctx, *res.ClusterID)
			switch {
			case err != nil:
				v.Warnings = append(v.Warnings, fmt.Sprintf("could not verify source cluster %s: %v", *res.ClusterID, err))
			case found:
				srcStatus, srcFound = st, true
			}
		}
		if !srcFound && res.ClusterName != "" {
			st, found, err := cl.MostRecentClusterStatusByName(ctx, res.ClusterName)
			switch {
			case err != nil:
				v.Warnings = append(v.Warnings, fmt.Sprintf("could not verify source cluster by name %q: %v", res.ClusterName, err))
			case found:
				srcStatus, srcFound = st, true
			}
		}
	} else {
		v.Warnings = append(v.Warnings, "cluster lookup unavailable; skipped live-cluster guard")
	}
	if srcFound {
		s := string(srcStatus)
		v.SourceClusterStatus = &s
		if IsLiveStatus(srcStatus) {
			v.BlockReasons = append(v.BlockReasons,
				fmt.Sprintf("source cluster is live (status %s); resource may still be in use", srcStatus))
		}
	}

	// 2. Grace period.
	age := now.Sub(res.FirstDetectedAt)
	if age < cfg.MinAge {
		v.BlockReasons = append(v.BlockReasons,
			fmt.Sprintf("within grace period: first detected %s ago, minimum age is %s",
				age.Round(time.Minute), cfg.MinAge))
	}
	if res.DetectionCount < cfg.MinDetections {
		v.BlockReasons = append(v.BlockReasons,
			fmt.Sprintf("detected only %d time(s), minimum is %d", res.DetectionCount, cfg.MinDetections))
	}

	// 3. Status guard.
	if res.Status != types.OrphanedResourceStatusActive {
		v.BlockReasons = append(v.BlockReasons,
			fmt.Sprintf("resource is not ACTIVE (status %s)", res.Status))
	}

	// 4. VPC-alive probe.
	if cfg.CheckVPCEmpty && res.ResourceType == types.OrphanedResourceTypeVPC {
		if vpc == nil {
			v.Warnings = append(v.Warnings, "VPC inspector unavailable; skipped live-infrastructure probe")
		} else {
			counts, err := vpc.InspectVPC(ctx, res.ResourceID, res.Region)
			if err != nil {
				v.Warnings = append(v.Warnings, fmt.Sprintf("could not inspect VPC %s: %v", res.ResourceID, err))
			} else {
				v.VPCLive = &counts
				if counts.HasLive() {
					v.BlockReasons = append(v.BlockReasons,
						fmt.Sprintf("VPC still hosts live resources (%d running instances, %d NAT gateways, %d load balancers)",
							counts.RunningInstances, counts.NATGateways, counts.LoadBalancers))
				}
			}
		}
	}

	v.Safe = len(v.BlockReasons) == 0
	return v
}
