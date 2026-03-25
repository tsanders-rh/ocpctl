package worker

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/tsanders-rh/ocpctl/internal/store"
	"github.com/tsanders-rh/ocpctl/pkg/types"
)

// LogStreamer tails a log file and streams entries to the database
type LogStreamer struct {
	store      *store.Store
	clusterID  string
	jobID      string
	logPath    string
	sequence   int64
	stopCh     chan struct{}
	wg         sync.WaitGroup
	batchSize  int
	flushInterval time.Duration

	// Regular expression for parsing openshift-install log format
	// Example: time="2024-03-01T10:30:00Z" level=info msg="Creating cluster..."
	logRegex *regexp.Regexp
}

// NewLogStreamer creates a new log streamer that tails a log file and streams entries to the database.
// The streamer batches log entries for efficient database writes and parses log levels from openshift-install format.
func NewLogStreamer(store *store.Store, clusterID, jobID, logPath string) *LogStreamer {
	return &LogStreamer{
		store:         store,
		clusterID:     clusterID,
		jobID:         jobID,
		logPath:       logPath,
		sequence:      0,
		stopCh:        make(chan struct{}),
		batchSize:     100,
		flushInterval: 2 * time.Second,
		// Regex to extract level from openshift-install logs
		// Matches: level=info, level=error, level=warn, level=debug
		logRegex:      regexp.MustCompile(`level=(\w+)`),
	}
}

// Start begins tailing the log file and streaming entries to the database.
// This is non-blocking and runs in a goroutine. The streamer will wait up to 30 seconds for the log file to be created.
// Call Stop() to gracefully shutdown the streamer and flush remaining log entries.
func (ls *LogStreamer) Start(ctx context.Context) error {
	ls.wg.Add(1)
	go ls.tailLogFile(ctx)
	return nil
}

// Stop gracefully stops the log streamer and flushes any pending log entries to the database.
// Blocks until the streamer goroutine has completed and all logs are flushed.
func (ls *LogStreamer) Stop() error {
	close(ls.stopCh)
	ls.wg.Wait()
	return nil
}

// tailLogFile continuously reads new lines from the log file
func (ls *LogStreamer) tailLogFile(ctx context.Context) {
	defer ls.wg.Done()

	var batch []*types.DeploymentLog
	flushTimer := time.NewTicker(ls.flushInterval)
	defer flushTimer.Stop()

	// Wait for log file to exist (it's created by openshift-install)
	if err := ls.waitForLogFile(ctx, 30*time.Second); err != nil {
		log.Printf("Warning: log file %s not found: %v", ls.logPath, err)
		return
	}

	// Open log file for reading
	file, err := os.Open(ls.logPath)
	if err != nil {
		log.Printf("Warning: failed to open log file %s: %v", ls.logPath, err)
		return
	}
	defer file.Close()

	reader := bufio.NewReader(file)

	for {
		select {
		case <-ctx.Done():
			// Context cancelled, flush remaining logs and exit
			if len(batch) > 0 {
				ls.flushBatch(context.Background(), batch)
			}
			return

		case <-ls.stopCh:
			// Stop signal received, flush remaining logs and exit
			if len(batch) > 0 {
				ls.flushBatch(context.Background(), batch)
			}
			return

		case <-flushTimer.C:
			// Flush timer triggered, flush any pending logs
			if len(batch) > 0 {
				batch = ls.flushBatch(ctx, batch)
			}

		default:
			// Try to read a line from the file
			line, err := reader.ReadString('\n')
			if err != nil {
				// No more data available, sleep briefly and retry
				time.Sleep(LogStreamPollInterval)
				continue
			}

			// Trim whitespace
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Parse log level from the line
			level := ls.extractLogLevel(line)

			// Create log entry
			logEntry := &types.DeploymentLog{
				ClusterID: ls.clusterID,
				JobID:     ls.jobID,
				Sequence:  ls.sequence,
				Timestamp: time.Now(),
				LogLevel:  level,
				Message:   line,
				Source:    types.DeploymentLogSourceInstaller,
			}

			ls.sequence++
			batch = append(batch, logEntry)

			// Flush if batch size reached
			if len(batch) >= ls.batchSize {
				batch = ls.flushBatch(ctx, batch)
			}
		}
	}
}

// waitForLogFile waits for the log file to be created (with timeout)
func (ls *LogStreamer) waitForLogFile(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if _, err := os.Stat(ls.logPath); err == nil {
			return nil // File exists
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(LogStreamFlushInterval):
			// Continue waiting
		}
	}

	return fmt.Errorf("timeout waiting for log file to be created")
}

// extractLogLevel parses the log level from an openshift-install log line
// Returns nil if level cannot be determined
func (ls *LogStreamer) extractLogLevel(line string) *string {
	matches := ls.logRegex.FindStringSubmatch(line)
	if len(matches) < 2 {
		return nil
	}

	level := strings.ToLower(matches[1])

	// Normalize to our standard levels
	switch level {
	case "debug":
		return stringPtr("debug")
	case "info":
		return stringPtr("info")
	case "warning", "warn":
		return stringPtr("warn")
	case "error", "fatal":
		return stringPtr("error")
	default:
		return nil
	}
}

// flushBatch writes the current batch of logs to the database
// Returns an empty batch (or the same batch if flush failed)
func (ls *LogStreamer) flushBatch(ctx context.Context, batch []*types.DeploymentLog) []*types.DeploymentLog {
	if len(batch) == 0 {
		return batch
	}

	// Use a timeout context for the database operation
	flushCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	err := ls.store.DeploymentLogs.AppendLogs(flushCtx, batch)
	if err != nil {
		log.Printf("Warning: failed to flush deployment logs batch (size=%d): %v", len(batch), err)
		// Return the batch to retry later
		return batch
	}

	// Successfully flushed, return empty batch
	return []*types.DeploymentLog{}
}

// stringPtr is a helper to create a pointer to a string
func stringPtr(s string) *string {
	return &s
}
