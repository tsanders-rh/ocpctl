package gcp

import (
	"context"
	"os/exec"
	"sync"
	"time"
)

// LabelCache provides thread-safe caching for sanitized GCP labels
// This reduces redundant string processing for frequently used labels
type LabelCache struct {
	mu    sync.RWMutex
	cache map[string]string
}

// NewLabelCache creates a new label cache
func NewLabelCache() *LabelCache {
	return &LabelCache{
		cache: make(map[string]string),
	}
}

// Get retrieves a cached label or computes and caches it
func (c *LabelCache) Get(input string, sanitizer func(string) string) string {
	// Fast path: read lock for cache hit
	c.mu.RLock()
	if cached, ok := c.cache[input]; ok {
		c.mu.RUnlock()
		return cached
	}
	c.mu.RUnlock()

	// Slow path: write lock for cache miss
	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check in case another goroutine cached it
	if cached, ok := c.cache[input]; ok {
		return cached
	}

	// Compute and cache
	sanitized := sanitizer(input)
	c.cache[input] = sanitized
	return sanitized
}

// Clear removes all cached entries
func (c *LabelCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]string)
}

// Size returns the number of cached entries
func (c *LabelCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.cache)
}

// CommandPool manages concurrent gcloud command execution
// with rate limiting to avoid overwhelming the GCP API
type CommandPool struct {
	semaphore chan struct{}
	timeout   time.Duration
}

// NewCommandPool creates a pool that allows maxConcurrent commands
func NewCommandPool(maxConcurrent int, timeout time.Duration) *CommandPool {
	return &CommandPool{
		semaphore: make(chan struct{}, maxConcurrent),
		timeout:   timeout,
	}
}

// Execute runs a command with concurrency limiting
func (p *CommandPool) Execute(ctx context.Context, name string, args ...string) ([]byte, error) {
	// Acquire semaphore
	select {
	case p.semaphore <- struct{}{}:
		defer func() { <-p.semaphore }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Create command with timeout
	cmdCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, name, args...)
	return cmd.CombinedOutput()
}

// ParallelExecutor runs multiple commands concurrently and collects results
type ParallelExecutor struct {
	pool *CommandPool
}

// NewParallelExecutor creates a new parallel command executor
func NewParallelExecutor(maxConcurrent int, timeout time.Duration) *ParallelExecutor {
	return &ParallelExecutor{
		pool: NewCommandPool(maxConcurrent, timeout),
	}
}

// CommandSpec defines a command to execute
type CommandSpec struct {
	Name string
	Args []string
	Key  string // Identifier for the result
}

// CommandResult contains the output and any error from a command
type CommandResult struct {
	Key    string
	Output []byte
	Err    error
}

// ExecuteAll runs multiple commands in parallel and returns results
func (e *ParallelExecutor) ExecuteAll(ctx context.Context, commands []CommandSpec) map[string]CommandResult {
	results := make(map[string]CommandResult)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, cmd := range commands {
		wg.Add(1)
		go func(spec CommandSpec) {
			defer wg.Done()

			output, err := e.pool.Execute(ctx, spec.Name, spec.Args...)

			mu.Lock()
			results[spec.Key] = CommandResult{
				Key:    spec.Key,
				Output: output,
				Err:    err,
			}
			mu.Unlock()
		}(cmd)
	}

	wg.Wait()
	return results
}

// StringBuilderPool provides pooled string builders to reduce allocations
var stringBuilderPool = sync.Pool{
	New: func() interface{} {
		return new(stringBuilder)
	},
}

type stringBuilder struct {
	buf []byte
}

// GetStringBuilder acquires a string builder from the pool
func GetStringBuilder() *stringBuilder {
	sb := stringBuilderPool.Get().(*stringBuilder)
	sb.buf = sb.buf[:0] // Reset
	return sb
}

// PutStringBuilder returns a string builder to the pool
func PutStringBuilder(sb *stringBuilder) {
	// Only pool builders that aren't too large
	if cap(sb.buf) <= 4096 {
		stringBuilderPool.Put(sb)
	}
}

// WriteString appends a string
func (sb *stringBuilder) WriteString(s string) {
	sb.buf = append(sb.buf, s...)
}

// WriteByte appends a byte
func (sb *stringBuilder) WriteByte(b byte) {
	sb.buf = append(sb.buf, b)
}

// String returns the built string
func (sb *stringBuilder) String() string {
	return string(sb.buf)
}

// Len returns the current length
func (sb *stringBuilder) Len() int {
	return len(sb.buf)
}

// ArgsBuilder efficiently builds command argument slices
type ArgsBuilder struct {
	args []string
}

// NewArgsBuilder creates a builder with estimated capacity
func NewArgsBuilder(estimatedSize int) *ArgsBuilder {
	return &ArgsBuilder{
		args: make([]string, 0, estimatedSize),
	}
}

// Add appends arguments
func (ab *ArgsBuilder) Add(args ...string) *ArgsBuilder {
	ab.args = append(ab.args, args...)
	return ab
}

// AddIf conditionally adds arguments
func (ab *ArgsBuilder) AddIf(condition bool, args ...string) *ArgsBuilder {
	if condition {
		ab.args = append(ab.args, args...)
	}
	return ab
}

// AddKeyValue adds a key-value pair if value is not empty
func (ab *ArgsBuilder) AddKeyValue(key, value string) *ArgsBuilder {
	if value != "" {
		ab.args = append(ab.args, key, value)
	}
	return ab
}

// Build returns the argument slice
func (ab *ArgsBuilder) Build() []string {
	return ab.args
}
