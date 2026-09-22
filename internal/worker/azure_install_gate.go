package worker

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// capzMetricsPort is the TCP port cluster-api-provider-azure (CAPZ) binds its
// metrics listener to while openshift-install runs its local Cluster API system
// for an Azure IPI install.
//
// The installer randomizes every other port it hands the local CAPI providers
// (--health-addr, --webhook-port) and explicitly disables metrics for the AWS
// provider (--metrics-bind-addr=0) and for CAPI core (--diagnostics-address=0),
// but it passes no metrics flag at all to the Azure provider:
//
//	Running process: azure infrastructure provider with args
//	  [-v=2 --health-addr=127.0.0.1:37841 --webhook-port=40641
//	   --webhook-cert-dir=... --feature-gates=MachinePool=false --kubeconfig=...]
//
// So CAPZ falls back to controller-runtime's default of :8443. A second Azure
// install starting on the same host while the first still holds that port loses
// the bind, and because controller-runtime treats a failed metrics listener as
// fatal, the whole CAPZ manager exits 1:
//
//	"problem running manager" err="failed to start metrics server: failed to
//	 create listener: listen tcp :8443: bind: address already in use"
//
// The installer then fails a few seconds later trying to reach CAPZ's validating
// webhook, which reads as an Azure credential problem but is not one:
//
//	failed to create infrastructure manifest: ... failed calling webhook
//	"validation.azureclusteridentity.infrastructure.cluster.x-k8s.io": ...
//	dial tcp 127.0.0.1:34511: connect: connection refused
const capzMetricsPort = 8443

// capiShutdownMarker is the installer log line emitted once the local Cluster
// API system — and with it the CAPZ process holding capzMetricsPort — is gone.
// It appears whether the CAPI phase succeeded or failed, so it is a safe signal
// to hand the gate to the next Azure install.
const capiShutdownMarker = "Local Cluster API system has completed operations"

// capiShutdownPollInterval is how often releaseAzureGateOnCAPIShutdown rechecks
// the installer log. Overridden in tests. Cheap to poll (a 64 KB tail read) and
// only affects how promptly the next Azure install may start, so it is tuned for
// negligible cost rather than precision.
var capiShutdownPollInterval = 15 * time.Second

// azureInstallGate serializes Azure OpenShift IPI installs within a single
// worker host, because they cannot share capzMetricsPort.
//
// The gate is intentionally host-scoped rather than fleet-scoped: the conflict
// is over a local port, so two Azure installs on two different workers are fine
// and are how Azure creates run in parallel. A queued Azure job is left in
// PENDING for another worker (or a later poll) to claim, which also keeps it
// visible to the PendingJobs metric that drives ASG scale-out.
type azureInstallGate struct {
	mu     sync.Mutex
	holder string    // job ID currently holding the gate; "" when free
	since  time.Time // when the current holder acquired it
}

// azureGate is the host's gate. A package-level value is appropriate here
// because the unit of exclusion is the host and exactly one worker process runs
// per host; both the dispatcher (poll) and the create handler need to reach it
// without threading it through every handler constructor.
var azureGate = &azureInstallGate{}

// tryAcquire claims the gate for jobID without blocking. It reports whether the
// gate was claimed.
func (g *azureInstallGate) tryAcquire(jobID string) bool {
	if jobID == "" {
		return false
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.holder != "" {
		return false
	}

	g.holder = jobID
	g.since = time.Now()
	return true
}

// release frees the gate if jobID is holding it, and does nothing otherwise.
//
// Being a no-op for non-holders is what makes the two release paths safe to
// combine: the create handler releases early once the installer reports the
// local CAPI system is down (~22 min into a ~42 min install), and processJob
// releases unconditionally when the job ends as a backstop for installs that
// died before reaching that point, or if the installer ever changes the wording
// of capiShutdownMarker.
func (g *azureInstallGate) release(jobID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.holder == "" || g.holder != jobID {
		return
	}

	g.holder = ""
	g.since = time.Time{}
}

// heldBy returns the job ID holding the gate and how long it has held it, or
// ("", 0) when the gate is free.
func (g *azureInstallGate) heldBy() (string, time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.holder == "" {
		return "", 0
	}
	return g.holder, time.Since(g.since)
}

// needsAzureInstallGate reports whether a job would run an Azure IPI install and
// therefore needs exclusive use of capzMetricsPort on this host.
//
// Scoped deliberately narrowly: only openshift-install drives CAPZ, so ARO/AKS
// jobs (az CLI) and Azure destroy/hibernate/resume jobs (Azure SDK, no local
// CAPI system) are unaffected and keep running concurrently.
func needsAzureInstallGate(job *types.Job, cluster *types.Cluster) bool {
	if job == nil || cluster == nil {
		return false
	}
	if job.JobType != types.JobTypeCreate {
		return false
	}
	return cluster.Platform == types.PlatformAzure && cluster.ClusterType == types.ClusterTypeOpenShift
}

// capzMetricsPortFree reports whether capzMetricsPort can currently be bound.
//
// This covers holders the gate cannot see — chiefly an openshift-install run
// started by hand on the worker. Go sets SO_REUSEADDR but not SO_REUSEPORT on
// listeners, so a successful probe means the port really was free; binding
// 0.0.0.0 also fails when something holds 127.0.0.1 only, which is the
// conservative answer we want.
func capzMetricsPortFree() bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", capzMetricsPort))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// errCAPZMetricsPortBusy returns the transient error to fail an Azure install
// with when capzMetricsPort is held by something outside this worker's control.
//
// Transient rather than permanent: the port frees itself when the other install
// finishes, and failing here costs seconds, whereas letting the install start
// costs a CAPZ crash plus the partial-infrastructure cleanup that follows it.
func errCAPZMetricsPortBusy(clusterName string) error {
	return &types.TransientError{
		Message: fmt.Sprintf("port %d is already in use on this worker, which an Azure IPI install requires", capzMetricsPort),
		Remediation: fmt.Sprintf(`The OpenShift installer runs cluster-api-provider-azure (CAPZ) during an Azure
install, and CAPZ binds port %d. The installer provides no way to change that
port, so only one Azure install can run per worker at a time.

Something already holds port %d on this host that this worker did not start —
most likely an openshift-install run started manually. Had the install been
allowed to start, CAPZ would have exited immediately and the install would have
failed with a misleading "connection refused" webhook error.

Cluster %s will retry automatically. If this persists, check for stray
openshift-install processes on the worker.`, capzMetricsPort, capzMetricsPort, clusterName),
		BackoffMins: 25,
	}
}

// installLogSize returns the current size of the installer log, or 0 if it does
// not exist yet.
//
// Callers pass this to releaseAzureGateOnCAPIShutdown as the floor for marker
// matching. openshift-install *appends* to .openshift_install.log and every
// attempt reuses the same work directory, so a retry starts with a log that
// already contains the previous attempt's capiShutdownMarker. Matching without
// a floor would free the port seconds into the retry while this attempt's CAPZ
// still held it — recreating the exact collision the gate exists to prevent.
func installLogSize(logPath string) int64 {
	info, err := os.Stat(logPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

// releaseAzureGateOnCAPIShutdown releases the Azure install gate as soon as the
// installer log shows the local Cluster API system has shut down, so the next
// Azure install can start without waiting for the remaining ~20 minutes of
// bootstrap and operator rollout.
//
// Only content written after floor is considered (see installLogSize). It
// returns when the gate is released or ctx is done; the caller's deferred
// release is the backstop, so failing to spot the marker only costs throughput.
func releaseAzureGateOnCAPIShutdown(ctx context.Context, logPath, jobID string, floor int64) {
	ticker := time.NewTicker(capiShutdownPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tail, err := readLogTailAfter(logPath, floor)
			if err != nil {
				// The log may not exist yet; keep watching.
				continue
			}
			if strings.Contains(tail, capiShutdownMarker) {
				azureGate.release(jobID)
				log.Printf("Local Cluster API system has shut down; released Azure install gate for job %s (port %d is free for the next Azure install)",
					jobID, capzMetricsPort)
				return
			}
		}
	}
}

// capiShutdownTailBytes caps how much of the installer log is read per check.
// Only the tail matters and a debug-level install log grows into the tens of MB.
const capiShutdownTailBytes = 64 * 1024

// readLogTailAfter returns the tail of the file at path, reading no further back
// than floor and no more than capiShutdownTailBytes.
func readLogTailAfter(path string, floor int64) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	size := info.Size()
	if size < floor {
		// The log was truncated or replaced, so the floor no longer refers to
		// anything: everything present was written by this attempt.
		floor = 0
	}

	offset := size - capiShutdownTailBytes
	if offset < floor {
		offset = floor
	}
	if offset < 0 {
		offset = 0
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return "", err
	}

	buf, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}
