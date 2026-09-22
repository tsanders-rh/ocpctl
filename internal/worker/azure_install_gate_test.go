package worker

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tsanders-rh/ocpctl/pkg/types"
)

func TestAzureInstallGateExcludesSecondHolder(t *testing.T) {
	g := &azureInstallGate{}

	if !g.tryAcquire("job-1") {
		t.Fatal("first acquire failed on a free gate")
	}
	if g.tryAcquire("job-2") {
		t.Fatal("second job acquired a gate already held by job-1")
	}

	holder, held := g.heldBy()
	if holder != "job-1" {
		t.Fatalf("heldBy() = %q, want job-1", holder)
	}
	if held < 0 {
		t.Fatalf("heldBy() duration = %v, want >= 0", held)
	}

	g.release("job-1")

	if holder, _ := g.heldBy(); holder != "" {
		t.Fatalf("gate still held by %q after release", holder)
	}
	if !g.tryAcquire("job-2") {
		t.Fatal("gate not acquirable after release")
	}
}

// A non-holder must not be able to free the gate, or the early-release path
// (which fires per job ID from a goroutine watching the installer log) could
// hand the port to a second install while the first still holds it.
func TestAzureInstallGateReleaseIgnoresNonHolder(t *testing.T) {
	g := &azureInstallGate{}

	if !g.tryAcquire("job-1") {
		t.Fatal("acquire failed on a free gate")
	}

	g.release("job-2")

	if holder, _ := g.heldBy(); holder != "job-1" {
		t.Fatalf("gate holder = %q after a non-holder released it, want job-1", holder)
	}
}

// Both the log-marker watcher and processJob's defer release the same job, so a
// repeated release must not free a gate a later job has since acquired.
func TestAzureInstallGateReleaseIsIdempotent(t *testing.T) {
	g := &azureInstallGate{}

	if !g.tryAcquire("job-1") {
		t.Fatal("acquire failed on a free gate")
	}
	g.release("job-1")
	g.release("job-1")

	if !g.tryAcquire("job-2") {
		t.Fatal("job-2 could not acquire the released gate")
	}

	// The stale second release from job-1 must not evict job-2.
	g.release("job-1")

	if holder, _ := g.heldBy(); holder != "job-2" {
		t.Fatalf("gate holder = %q after stale release, want job-2", holder)
	}
}

func TestAzureInstallGateEmptyJobIDCannotAcquire(t *testing.T) {
	g := &azureInstallGate{}

	if g.tryAcquire("") {
		t.Fatal("empty job ID acquired the gate; release(\"\") would then free it for anyone")
	}
}

func TestAzureInstallGateIsExclusiveUnderConcurrency(t *testing.T) {
	g := &azureInstallGate{}

	const goroutines = 50
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		acquired int
	)

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			if g.tryAcquire(fmt.Sprintf("job-%d", i)) {
				mu.Lock()
				acquired++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if acquired != 1 {
		t.Fatalf("%d goroutines acquired the gate concurrently, want exactly 1", acquired)
	}
}

func TestNeedsAzureInstallGate(t *testing.T) {
	tests := []struct {
		name    string
		job     *types.Job
		cluster *types.Cluster
		want    bool
	}{
		{
			name:    "azure openshift create needs the gate",
			job:     &types.Job{JobType: types.JobTypeCreate},
			cluster: &types.Cluster{Platform: types.PlatformAzure, ClusterType: types.ClusterTypeOpenShift},
			want:    true,
		},
		{
			// Destroy uses the Azure SDK and starts no local CAPI system.
			name:    "azure openshift destroy does not",
			job:     &types.Job{JobType: types.JobTypeDestroy},
			cluster: &types.Cluster{Platform: types.PlatformAzure, ClusterType: types.ClusterTypeOpenShift},
			want:    false,
		},
		{
			name:    "azure hibernate does not",
			job:     &types.Job{JobType: types.JobTypeHibernate},
			cluster: &types.Cluster{Platform: types.PlatformAzure, ClusterType: types.ClusterTypeOpenShift},
			want:    false,
		},
		{
			// The installer disables the AWS provider's metrics listener, so AWS
			// installs never contend for the port and must not be serialized.
			name:    "aws openshift create does not",
			job:     &types.Job{JobType: types.JobTypeCreate},
			cluster: &types.Cluster{Platform: types.PlatformAWS, ClusterType: types.ClusterTypeOpenShift},
			want:    false,
		},
		{
			name:    "gcp openshift create does not",
			job:     &types.Job{JobType: types.JobTypeCreate},
			cluster: &types.Cluster{Platform: types.PlatformGCP, ClusterType: types.ClusterTypeOpenShift},
			want:    false,
		},
		{
			name:    "nil cluster (pool-level job) does not",
			job:     &types.Job{JobType: types.JobTypeCreate},
			cluster: nil,
			want:    false,
		},
		{
			name:    "nil job does not",
			job:     nil,
			cluster: &types.Cluster{Platform: types.PlatformAzure, ClusterType: types.ClusterTypeOpenShift},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsAzureInstallGate(tt.job, tt.cluster); got != tt.want {
				t.Errorf("needsAzureInstallGate() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCAPZMetricsPortFreeDetectsHolder(t *testing.T) {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", capzMetricsPort))
	if err != nil {
		t.Skipf("cannot bind port %d in this environment: %v", capzMetricsPort, err)
	}

	if capzMetricsPortFree() {
		_ = ln.Close()
		t.Fatalf("capzMetricsPortFree() = true while port %d is held", capzMetricsPort)
	}

	if err := ln.Close(); err != nil {
		t.Fatalf("closing listener: %v", err)
	}
	if !capzMetricsPortFree() {
		t.Fatalf("capzMetricsPortFree() = false after the holder released port %d", capzMetricsPort)
	}
}

// An Azure install blocked by a foreign port holder must fail transiently, or
// the worker burns one of its three attempts on a condition that clears itself.
func TestErrCAPZMetricsPortBusyIsTransient(t *testing.T) {
	err := errCAPZMetricsPortBusy("octl-test-1")

	transient := DetectTransientError(err)
	if transient == nil {
		t.Fatal("DetectTransientError() = nil, want a transient error")
	}
	if transient.BackoffMins <= 0 {
		t.Errorf("BackoffMins = %d, want > 0", transient.BackoffMins)
	}
	if cause, _ := DetectPermanentError(err); cause != "" {
		t.Errorf("DetectPermanentError() = %q, want no permanent classification", cause)
	}
	if !strings.Contains(err.Error(), "octl-test-1") {
		t.Errorf("error does not name the cluster: %q", err.Error())
	}
}

// The raw CAPZ failure, as it reaches the worker inside the installer's stderr,
// must also classify as transient so a collision the gate did not prevent is
// retried rather than recorded as a hard failure.
func TestCAPZBindFailureClassifiesTransient(t *testing.T) {
	installerErr := fmt.Errorf(`openshift-install create cluster failed: exit status 4
Stderr: level=debug msg=E0922 08:14:56.034434  227208 main.go:353] "problem running manager" err="failed to start metrics server: failed to create listener: listen tcp :8443: bind: address already in use" logger="setup"
level=error msg=failed to create infrastructure manifest: Internal error occurred: failed calling webhook "validation.azureclusteridentity.infrastructure.cluster.x-k8s.io": dial tcp 127.0.0.1:34511: connect: connection refused`)

	transient := DetectTransientError(installerErr)
	if transient == nil {
		t.Fatal("DetectTransientError() = nil for a CAPZ port collision, want transient")
	}
	if transient.BackoffMins <= 0 {
		t.Errorf("BackoffMins = %d, want > 0", transient.BackoffMins)
	}
}

// withFastCAPIShutdownPolling shortens the watcher's poll interval so the test
// does not wait out the production interval.
func withFastCAPIShutdownPolling(t *testing.T) {
	t.Helper()
	original := capiShutdownPollInterval
	capiShutdownPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { capiShutdownPollInterval = original })
}

func TestReleaseAzureGateOnCAPIShutdown(t *testing.T) {
	withFastCAPIShutdownPolling(t)

	logPath := filepath.Join(t.TempDir(), ".openshift_install.log")
	if err := os.WriteFile(logPath, []byte("level=info msg=Creating infrastructure resources...\n"), 0600); err != nil {
		t.Fatalf("writing log: %v", err)
	}

	if !azureGate.tryAcquire("job-shutdown") {
		t.Fatal("could not acquire the package gate")
	}
	t.Cleanup(func() { azureGate.release("job-shutdown") })

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		releaseAzureGateOnCAPIShutdown(ctx, logPath, "job-shutdown", installLogSize(logPath))
		close(done)
	}()

	// Still mid-install: the gate must stay held.
	time.Sleep(100 * time.Millisecond)
	if holder, _ := azureGate.heldBy(); holder != "job-shutdown" {
		t.Fatalf("gate released before the CAPI shutdown marker appeared (holder %q)", holder)
	}

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("reopening log: %v", err)
	}
	if _, err := f.WriteString("level=info msg=" + capiShutdownMarker + "\n"); err != nil {
		t.Fatalf("appending marker: %v", err)
	}
	f.Close()

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("watcher did not return after the CAPI shutdown marker was written")
	}

	if holder, _ := azureGate.heldBy(); holder != "" {
		t.Fatalf("gate still held by %q after the CAPI shutdown marker", holder)
	}
}

// A retry reuses the work directory, and openshift-install appends to the same
// .openshift_install.log — so the log already holds the previous attempt's
// shutdown marker. Releasing on that stale marker would free the port while this
// attempt's CAPZ still held it, recreating the collision the gate prevents.
func TestReleaseAzureGateIgnoresPreviousAttemptMarker(t *testing.T) {
	withFastCAPIShutdownPolling(t)

	logPath := filepath.Join(t.TempDir(), ".openshift_install.log")
	previousAttempt := "level=info msg=Creating infrastructure resources...\n" +
		"level=warning msg=process cluster-api-provider-azure exited with error: exit status 1\n" +
		"level=info msg=" + capiShutdownMarker + "\n"
	if err := os.WriteFile(logPath, []byte(previousAttempt), 0600); err != nil {
		t.Fatalf("writing previous attempt log: %v", err)
	}

	floor := installLogSize(logPath)
	if floor == 0 {
		t.Fatal("installLogSize() = 0 for a non-empty log")
	}

	if !azureGate.tryAcquire("job-retry") {
		t.Fatal("could not acquire the package gate")
	}
	t.Cleanup(func() { azureGate.release("job-retry") })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		releaseAzureGateOnCAPIShutdown(ctx, logPath, "job-retry", floor)
		close(done)
	}()

	// The retry is now mid-install and has written output of its own, but no new
	// shutdown marker. The gate must stay held.
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("reopening log: %v", err)
	}
	if _, err := f.WriteString("level=info msg=Started local control plane with envtest\n"); err != nil {
		t.Fatalf("appending retry output: %v", err)
	}
	f.Close()

	time.Sleep(200 * time.Millisecond)

	if holder, _ := azureGate.heldBy(); holder != "job-retry" {
		t.Fatalf("gate released on the previous attempt's marker (holder %q, want job-retry)", holder)
	}

	select {
	case <-done:
		t.Fatal("watcher returned before this attempt logged its own shutdown marker")
	default:
	}

	// Now this attempt's CAPI phase really ends.
	f, err = os.OpenFile(logPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("reopening log: %v", err)
	}
	if _, err := f.WriteString("level=info msg=" + capiShutdownMarker + "\n"); err != nil {
		t.Fatalf("appending marker: %v", err)
	}
	f.Close()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("watcher did not release after this attempt's shutdown marker")
	}

	if holder, _ := azureGate.heldBy(); holder != "" {
		t.Fatalf("gate still held by %q after this attempt's marker", holder)
	}
}

func TestReadLogTailAfterHonoursFloorAndTruncation(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "install.log")

	if err := os.WriteFile(logPath, []byte("OLD-"+capiShutdownMarker+"\nNEW-content"), 0600); err != nil {
		t.Fatalf("writing log: %v", err)
	}

	floor := int64(len("OLD-" + capiShutdownMarker + "\n"))
	tail, err := readLogTailAfter(logPath, floor)
	if err != nil {
		t.Fatalf("readLogTailAfter() error: %v", err)
	}
	if strings.Contains(tail, capiShutdownMarker) {
		t.Errorf("tail contains pre-floor content: %q", tail)
	}
	if tail != "NEW-content" {
		t.Errorf("tail = %q, want %q", tail, "NEW-content")
	}

	// A floor past EOF means the log was truncated or replaced, so everything
	// present belongs to this attempt and must be readable.
	if err := os.WriteFile(logPath, []byte("fresh-"+capiShutdownMarker), 0600); err != nil {
		t.Fatalf("truncating log: %v", err)
	}
	tail, err = readLogTailAfter(logPath, 1<<20)
	if err != nil {
		t.Fatalf("readLogTailAfter() after truncation: %v", err)
	}
	if !strings.Contains(tail, capiShutdownMarker) {
		t.Errorf("tail after truncation = %q, want it to include the marker", tail)
	}

	if _, err := readLogTailAfter(filepath.Join(dir, "missing.log"), 0); err == nil {
		t.Error("readLogTailAfter() on a missing file returned no error")
	}
}

func TestReleaseAzureGateOnCAPIShutdownStopsOnContextCancel(t *testing.T) {
	withFastCAPIShutdownPolling(t)

	logPath := filepath.Join(t.TempDir(), ".openshift_install.log")

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		releaseAzureGateOnCAPIShutdown(ctx, logPath, "job-cancel", 0)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not return after context cancellation")
	}
}

func TestReadLogTailAfterCapsBytesRead(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "big.log")
	body := strings.Repeat("a", capiShutdownTailBytes*2) + "TAIL"
	if err := os.WriteFile(logPath, []byte(body), 0600); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	tail, err := readLogTailAfter(logPath, 0)
	if err != nil {
		t.Fatalf("readLogTailAfter() error: %v", err)
	}
	if len(tail) != capiShutdownTailBytes {
		t.Fatalf("readLogTailAfter() returned %d bytes, want %d", len(tail), capiShutdownTailBytes)
	}
	if !strings.HasSuffix(tail, "TAIL") {
		t.Errorf("readLogTailAfter() did not return the end of the file")
	}
}
