package worker

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/baremetal/lifecycle"
	"github.com/tsanders-rh/ocpctl/internal/profile"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// rhwaAddonID is the ocpctl addon whose operator set (NHC/FAR/SNR/NMO/MDR) the
// native bare-metal create installs during provisioning.
const rhwaAddonID = "rhwa"

// handleBareMetalCreate provisions an agent-based bare-metal cluster natively:
// ocpctl launches the nested-virt EC2 host substrate, provisions libvirt +
// sushy-tools + haproxy, runs the Agent-Based Installer, and configures metal3
// BareMetalHosts + RHWA fence_redfish fencing — all in-process via the
// internal/baremetal packages (no rhwa-lab shell-out).
func (h *CreateHandler) handleBareMetalCreate(ctx context.Context, job *types.Job, cluster *types.Cluster) error {
	log.Printf("Starting bare-metal (agent-based) cluster creation for %s", cluster.Name)

	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusCreating); err != nil {
		return fmt.Errorf("update cluster status: %w", err)
	}

	prof, err := h.registry.Get(cluster.Profile)
	if err != nil {
		return fmt.Errorf("get profile: %w", err)
	}
	bm := prof.PlatformConfig.BareMetal
	if bm == nil {
		return fmt.Errorf("profile %s has no baremetal platform config", cluster.Profile)
	}

	// RHWA operator versions are declared in the `rhwa` addon catalog entry (the
	// single source of truth); they are installed natively during create so they
	// precede the substrate-specific fence_redfish fencing.
	rhwaAddon, err := h.store.PostConfigAddons.GetByAddonID(ctx, rhwaAddonID)
	if err != nil {
		return fmt.Errorf("load %s addon operators: %w", rhwaAddonID, err)
	}

	workDir, err := ensureSecureWorkDir(h.config.WorkDir, cluster.ID)
	if err != nil {
		return err
	}

	// Pull secret (with optional custom merge), same as the IPI path.
	pullSecret := os.Getenv("OPENSHIFT_PULL_SECRET")
	if pullSecret == "" {
		return fmt.Errorf("OPENSHIFT_PULL_SECRET environment variable not set")
	}
	if cluster.CustomPullSecret != nil && *cluster.CustomPullSecret != "" {
		merged, err := mergePullSecrets(pullSecret, *cluster.CustomPullSecret)
		if err != nil {
			return fmt.Errorf("merge pull secrets: %w", err)
		}
		pullSecret = merged
	}

	baseDomain := ""
	if cluster.BaseDomain != nil {
		baseDomain = *cluster.BaseDomain
	}

	// Stream the native orchestration's output to the database in real time: the
	// host client writes provisioning/install logs to logFile, which the streamer
	// tails into the deployment log.
	logPath := filepath.Join(workDir, "baremetal.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return fmt.Errorf("create deployment log: %w", err)
	}
	streamer := NewLogStreamer(h.store, cluster.ID, job.ID, logPath)
	streamCtx, streamCancel := context.WithCancel(ctx)
	defer streamCancel()
	if err := streamer.Start(streamCtx); err != nil {
		log.Printf("Warning: failed to start log streaming: %v", err)
	}

	renderer := profile.NewRenderer(h.registry)
	in := lifecycle.Input{
		ClusterID:   cluster.ID,
		ClusterName: cluster.Name,
		Region:      cluster.Region,
		BaseDomain:  baseDomain,
		Version:     cluster.Version,
		CreatedAt:   cluster.CreatedAt,
		WorkDir:     workDir,
		BareMetal:   bm,
		Compute:     &prof.Compute,
		Networking:  prof.Networking,
		Operators:   rhwaAddon.Config.Operators,
		// RenderInstallConfig runs after Launch so the cluster's sshKey is the
		// ephemeral substrate key (enables ocpctl's tunneled node access).
		RenderInstallConfig: func(sshPubKey string) ([]byte, error) {
			key := sshPubKey
			req := &types.CreateClusterRequest{
				Name:         cluster.Name,
				Platform:     string(cluster.Platform),
				Version:      cluster.Version,
				Profile:      cluster.Profile,
				Region:       cluster.Region,
				BaseDomain:   baseDomain,
				Owner:        cluster.Owner,
				Team:         cluster.Team,
				CostCenter:   cluster.CostCenter,
				TTLHours:     cluster.TTLHours,
				SSHPublicKey: &key,
				ExtraTags:    cluster.RequestTags,
			}
			return renderer.RenderInstallConfig(req, pullSecret, cluster.EffectiveTags)
		},
	}

	log.Printf("Running native bare-metal create for %s (version %s, host %s)", cluster.Name, cluster.Version, bm.HostInstanceType)
	res, runErr := lifecycle.Create(ctx, logFile, in)

	logFile.Close()
	streamCancel()
	time.Sleep(LogBatchFlushDelay)
	if stopErr := streamer.Stop(); stopErr != nil {
		log.Printf("Warning: error stopping log streamer: %v", stopErr)
	}

	if runErr != nil {
		if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusFailed); err != nil {
			log.Printf("Warning: failed to mark cluster failed: %v", err)
		}
		return fmt.Errorf("baremetal create: %w", runErr)
	}
	log.Printf("Bare-metal cluster %s created successfully", cluster.Name)

	// Write the returned auth bundle so the standard output-extraction and
	// artifact-storage paths pick it up.
	if err := writeAuthBundle(workDir, res.Kubeconfig, res.KubeadminPassword); err != nil {
		log.Printf("Warning: failed to write auth bundle: %v", err)
	}

	outputs, err := h.extractClusterOutputs(workDir, cluster)
	if err != nil {
		log.Printf("Warning: failed to extract cluster outputs: %v", err)
	} else if err := h.store.ClusterOutputs.Upsert(ctx, outputs); err != nil {
		log.Printf("Warning: failed to store cluster outputs: %v", err)
	}

	// Agent-based installs do not produce a local metadata.json (the substrate is
	// torn down by terminating the host, not by openshift-install destroy).
	if err := h.storeArtifacts(ctx, workDir, cluster.ID, false); err != nil {
		log.Printf("Warning: failed to store artifacts: %v", err)
	}

	if err := h.store.Clusters.UpdateStatus(ctx, nil, cluster.ID, types.ClusterStatusReady); err != nil {
		return fmt.Errorf("update cluster status to ready: %w", err)
	}

	gracePeriodExpiry := time.Now().Add(WorkHoursGracePeriod)
	if err := h.store.Clusters.SetLastWorkHoursCheck(ctx, cluster.ID, gracePeriodExpiry); err != nil {
		log.Printf("Warning: failed to set work hours grace period for cluster %s: %v", cluster.Name, err)
	}

	log.Printf("Bare-metal cluster %s is now READY", cluster.Name)

	h.handlePostDeployment(ctx, cluster)
	return nil
}

// writeAuthBundle writes the kubeconfig + kubeadmin password into workDir/auth so
// extractClusterOutputs and storeArtifacts pick them up like the IPI path.
func writeAuthBundle(workDir string, kubeconfig, kubeadmin []byte) error {
	authDir := filepath.Join(workDir, "auth")
	if err := os.MkdirAll(authDir, 0700); err != nil {
		return fmt.Errorf("create auth dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "kubeconfig"), kubeconfig, 0600); err != nil {
		return fmt.Errorf("write kubeconfig: %w", err)
	}
	if len(kubeadmin) > 0 {
		if err := os.WriteFile(filepath.Join(authDir, "kubeadmin-password"), kubeadmin, 0600); err != nil {
			return fmt.Errorf("write kubeadmin-password: %w", err)
		}
	}
	return nil
}
