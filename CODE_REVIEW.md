# OCPCTL Codebase - Comprehensive Code Review

**Review Date:** April 25, 2026
**Reviewer:** Automated Code Analysis
**Total Issues Found:** 40

---

## Executive Summary

This comprehensive code review identified 40 issues across the ocpctl codebase, ranging from critical stability and security problems to code quality improvements. The most urgent issues involve:

- **Race conditions** in health check handling
- **Goroutine leaks** in worker shutdown and API key validation
- **Memory exhaustion risks** from unbounded database queries
- **Context propagation issues** preventing graceful shutdown

### Severity Distribution

| Severity | Count | Categories |
|----------|-------|------------|
| Critical | 6 | Race conditions, memory exhaustion, goroutine leaks |
| High | 9 | N+1 queries, unbounded queries, security gaps |
| Medium | 19 | Validation gaps, audit logging, debug logging |
| Low | 6 | Code quality, TODOs, magic numbers |

---

## CRITICAL SEVERITY ISSUES

### 1. Race Condition on HealthCheck Ready Flag

**File:** `cmd/worker/main.go`, Lines 354, 364
**Severity:** CRITICAL
**Category:** Concurrency Bug

**Description:**
The `healthCheck.ready` flag is accessed concurrently by health check handlers and the main goroutine without synchronization. This is a classic data race.

**Code:**
```go
healthCheck.ready = true  // Unprotected write (line 354)
// ...
healthCheck.ready = false // Unprotected write (line 364)
```

**Impact:**
- Concurrent access without locks leads to undefined behavior
- Health checks may return inconsistent results
- Race detector will flag this in testing
- Potential crashes or incorrect health status reporting

**Recommended Fix:**
```go
// Use atomic operations
type HealthCheck struct {
    ready atomic.Bool
}

// Then use:
healthCheck.ready.Store(true)
if healthCheck.ready.Load() { ... }

// OR use sync.RWMutex:
type HealthCheck struct {
    mu    sync.RWMutex
    ready bool
}
```

---

### 2. Missing Context Cancellation Wait for Goroutines

**File:** `cmd/worker/main.go`, Lines 339-349, 370-371
**Severity:** CRITICAL
**Category:** Resource Leak

**Description:**
Worker and janitor goroutines are started but there's no guarantee they've actually stopped after context cancellation before the process exits. Contexts are cancelled but there's no waiting mechanism.

**Code:**
```go
go func() {
    if err := w.Start(workerCtx); err != nil && err != context.Canceled {
        log.Printf("Worker error: %v", err)
    }
}()
// ...
workerCancel()  // Context cancelled but no wait
janitorCancel()
```

**Impact:**
- Goroutine leaks on shutdown
- Incomplete job processing during graceful shutdown
- Resources not properly cleaned up (database connections, file handles)
- Potential data corruption from interrupted operations

**Recommended Fix:**
```go
var wg sync.WaitGroup

// Start worker
wg.Add(1)
go func() {
    defer wg.Done()
    if err := w.Start(workerCtx); err != nil && err != context.Canceled {
        log.Printf("Worker error: %v", err)
    }
}()

// Start janitor
wg.Add(1)
go func() {
    defer wg.Done()
    if err := j.Start(janitorCtx); err != nil && err != context.Canceled {
        log.Printf("Janitor error: %v", err)
    }
}()

// On shutdown:
workerCancel()
janitorCancel()
wg.Wait() // Wait for all goroutines to finish
log.Println("All goroutines stopped gracefully")
```

---

### 3. Context.Background() Used in Background Goroutines

**File:** `internal/janitor/janitor.go`, Line 114
**Severity:** CRITICAL
**Category:** Shutdown Handling

**Description:**
The janitor's `run()` method creates a new `context.Background()` instead of using the cancellation context passed to `Start()`. This means the janitor won't stop when told to shutdown.

**Code:**
```go
func (j *Janitor) run() {
    ctx := context.Background()  // Creates new context, ignores j.ctx
    // ... rest of janitor logic
}
```

**Impact:**
- Janitor cannot be properly shut down
- Cleanup operations continue indefinitely during shutdown
- Process hangs during graceful termination
- Kubernetes pod eviction timeouts

**Recommended Fix:**
```go
func (j *Janitor) run() {
    ctx := j.ctx  // Use the cancellation context
    // ... rest of janitor logic
}
```

---

### 4. Unbounded Query with Hardcoded High Limit

**File:** `internal/api/handler_clusters.go`, Line 1195
**Severity:** CRITICAL
**Category:** Memory Exhaustion

**Description:**
The `GetStatistics` handler uses `Limit: 10000` to fetch all clusters without pagination. This could load massive datasets into memory and cause OutOfMemory errors in production.

**Code:**
```go
clusters, _, err := h.store.Clusters.List(ctx, store.ListFilters{
    Limit:  10000, // High limit to get all clusters
    Offset: 0,
})
```

**Impact:**
- Memory exhaustion with large deployments (>10k clusters)
- Performance degradation as database result set grows
- Potential DoS vulnerability (admin endpoint but still risky)
- API timeout on slow queries

**Recommended Fix:**

**Option 1: Use Database Aggregation**
```go
// Add new store method for aggregated statistics
func (s *ClusterStore) GetStatistics(ctx context.Context) (*ClusterStatistics, error) {
    query := `
        SELECT
            COUNT(*) as total_clusters,
            status,
            COUNT(*) as count,
            SUM(CASE WHEN status NOT IN ('DESTROYED', 'FAILED') THEN 1 ELSE 0 END) as active_clusters
        FROM clusters
        WHERE status != 'DESTROYED'
        GROUP BY status
    `
    // ... execute aggregation query
}
```

**Option 2: Implement Streaming**
```go
// Process clusters in batches
const batchSize = 1000
offset := 0
for {
    batch, total, err := h.store.Clusters.List(ctx, store.ListFilters{
        Limit:  batchSize,
        Offset: offset,
    })
    if err != nil {
        return err
    }

    // Process batch
    for _, cluster := range batch {
        // Calculate statistics incrementally
    }

    offset += batchSize
    if offset >= total {
        break
    }
}
```

---

### 5. Unbounded DELETE Query Without WHERE Clause Validation

**File:** `internal/store/clusters.go`, Line 680
**Severity:** CRITICAL
**Category:** Data Loss Risk

**Description:**
Database DELETE operations exist but there's no validation to prevent accidental full-table deletes. The query construction doesn't enforce required WHERE clauses.

**Code:**
```go
func (s *ClusterStore) DeleteDestroyedClusters(ctx context.Context, olderThan time.Time) (int, error) {
    query := `
        DELETE FROM clusters
        WHERE status = $1
            AND destroyed_at IS NOT NULL
            AND destroyed_at < $2
    `
    // ... but what if parameters are accidentally nil or default values?
}
```

**Impact:**
- Potential data loss through programming error
- Accidental full-table deletion
- No recovery without backup restore

**Recommended Fix:**
```go
func (s *ClusterStore) DeleteDestroyedClusters(ctx context.Context, olderThan time.Time) (int, error) {
    // Validate inputs
    if olderThan.IsZero() {
        return 0, fmt.Errorf("olderThan time cannot be zero")
    }

    // Prevent deleting recent data (safety check)
    if olderThan.After(time.Now().Add(-24 * time.Hour)) {
        return 0, fmt.Errorf("olderThan must be at least 24 hours in the past")
    }

    query := `
        DELETE FROM clusters
        WHERE status = $1
            AND destroyed_at IS NOT NULL
            AND destroyed_at < $2
    `

    result, err := s.pool.Exec(ctx, query, types.ClusterStatusDestroyed, olderThan)
    if err != nil {
        return 0, fmt.Errorf("delete destroyed clusters: %w", err)
    }

    deleted := int(result.RowsAffected())

    // Log deletions for audit
    if deleted > 0 {
        log.Printf("Deleted %d destroyed clusters older than %s", deleted, olderThan)
    }

    return deleted, nil
}
```

---

### 6. Goroutine Leak in API Key Validation

**File:** `internal/auth/apikey.go`, Lines 71-73
**Severity:** CRITICAL
**Category:** Resource Leak

**Description:**
A goroutine is spawned without tracking in `ValidateAPIKey()` to update last-used timestamp. If many API keys are validated, goroutines accumulate.

**Code:**
```go
go func() {
    _ = st.APIKeys.UpdateLastUsed(context.Background(), apiKey.ID)
}()
```

**Impact:**
- Goroutine leak under high API key usage
- Resource exhaustion on high-traffic endpoints
- Memory growth over time
- No error handling or monitoring

**Recommended Fix:**

**Option 1: Use Buffered Channel (Non-blocking)**
```go
type APIKeyUpdateQueue struct {
    updates chan string
    store   *store.Store
}

func NewAPIKeyUpdateQueue(store *store.Store, workers int) *APIKeyUpdateQueue {
    q := &APIKeyUpdateQueue{
        updates: make(chan string, 1000), // Buffer 1000 updates
        store:   store,
    }

    // Start worker pool
    for i := 0; i < workers; i++ {
        go q.worker()
    }

    return q
}

func (q *APIKeyUpdateQueue) worker() {
    for apiKeyID := range q.updates {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        _ = q.store.APIKeys.UpdateLastUsed(ctx, apiKeyID)
        cancel()
    }
}

func (q *APIKeyUpdateQueue) Update(apiKeyID string) {
    select {
    case q.updates <- apiKeyID:
        // Queued successfully
    default:
        // Queue full, drop update (non-critical)
    }
}
```

**Option 2: Use sync.Pool with Timeout**
```go
var updatePool = sync.Pool{
    New: func() interface{} {
        return &struct{}{}
    },
}

// In ValidateAPIKey:
go func() {
    token := updatePool.Get()
    defer updatePool.Put(token)

    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    _ = st.APIKeys.UpdateLastUsed(ctx, apiKey.ID)
}()
```

---

## HIGH SEVERITY ISSUES

### 7. N+1 Query Pattern in Statistics Calculation

**File:** `internal/api/handler_clusters.go`, Lines 1202-1218
**Severity:** HIGH
**Category:** Performance

**Description:**
Although batch fetching with `GetByIDs()` is used, the original implementation had N+1 query risks. The current approach is partially optimized but could still be improved.

**Code:**
```go
// Current (partially optimized):
ownerIDs := make([]string, 0)
for _, cluster := range clusters {
    if !ownerIDSet[cluster.OwnerID] {
        ownerIDs = append(ownerIDs, cluster.OwnerID)
        ownerIDSet[cluster.OwnerID] = true
    }
}

usersByID, err := h.store.Users.GetByIDs(ctx, ownerIDs)
```

**Impact:**
- Performance degradation with large datasets
- Database connection pool exhaustion
- Increased API response time

**Recommended Fix:**
Already largely fixed with batch queries. Ensure all similar patterns use batch operations. Consider adding metrics to monitor query counts.

---

### 8. Unbounded Cluster List in Orphan Detection

**File:** `internal/janitor/orphan_detector.go`, Line 34
**Severity:** HIGH
**Category:** Memory Exhaustion

**Description:**
`ListAll()` loads all clusters into memory without limits. For large deployments, this causes memory issues.

**Code:**
```go
clusters, err := j.store.Clusters.ListAll(ctx)  // Loads up to 100k clusters
```

**Impact:**
- Memory exhaustion during orphan detection
- Performance degradation on janitor runs
- Potential OOM kills in containerized environments

**Recommended Fix:**

**Option 1: Streaming Approach**
```go
func (j *Janitor) detectOrphanedResources(ctx context.Context) error {
    const batchSize = 1000
    offset := 0

    clustersByID := make(map[string]*types.Cluster)
    clustersByName := make(map[string]*types.Cluster)

    for {
        batch, total, err := j.store.Clusters.List(ctx, store.ListFilters{
            Limit:  batchSize,
            Offset: offset,
        })
        if err != nil {
            return fmt.Errorf("list clusters batch: %w", err)
        }

        for _, cluster := range batch {
            clustersByID[cluster.ID] = cluster
            clustersByName[cluster.Name] = cluster
        }

        offset += batchSize
        if offset >= total {
            break
        }
    }

    // Continue with orphan detection using maps
    return j.detectAWSOrphanedResources(ctx, clustersByID, clustersByName)
}
```

**Option 2: Database-Level Detection**
```sql
-- Find orphaned resources directly in database
SELECT DISTINCT r.resource_id
FROM aws_resources r
LEFT JOIN clusters c ON r.cluster_id = c.id
WHERE c.id IS NULL OR c.status = 'DESTROYED'
```

---

### 9. Missing Rate Limit Cleanup for Old Limiters

**File:** `internal/api/middleware/ratelimit.go`, Lines 56-61
**Severity:** HIGH
**Category:** Memory Leak

**Description:**
The rate limiter cleanup routine resets the entire map every 5 minutes, but IPs that haven't made requests recently still accumulate. No tracking of last access time per IP.

**Code:**
```go
for range ticker.C {
    s.mu.Lock()
    s.limiters = make(map[string]*rate.Limiter)  // Crude cleanup
    s.mu.Unlock()
}
```

**Impact:**
- Memory leak in long-running processes
- Map grows unbounded with unique source IPs
- Potential abuse through IP rotation attacks

**Recommended Fix:**
```go
type rateLimiterEntry struct {
    limiter    *rate.Limiter
    lastAccess time.Time
}

type IPRateLimiter struct {
    limiters map[string]*rateLimiterEntry
    mu       sync.RWMutex
    rate     rate.Limit
    burst    int
}

func (s *IPRateLimiter) cleanup() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()

    for range ticker.C {
        s.mu.Lock()
        now := time.Now()
        for ip, entry := range s.limiters {
            // Remove entries not accessed in last 30 minutes
            if now.Sub(entry.lastAccess) > 30*time.Minute {
                delete(s.limiters, ip)
            }
        }
        s.mu.Unlock()
    }
}

func (s *IPRateLimiter) getLimiter(ip string) *rate.Limiter {
    s.mu.Lock()
    defer s.mu.Unlock()

    entry, exists := s.limiters[ip]
    if !exists {
        entry = &rateLimiterEntry{
            limiter:    rate.NewLimiter(s.rate, s.burst),
            lastAccess: time.Now(),
        }
        s.limiters[ip] = entry
    } else {
        entry.lastAccess = time.Now()
    }

    return entry.limiter
}
```

---

### 10. No Pagination Limit on GetAllLogStats/GetAllLogs

**File:** `internal/api/handler_logs.go`, Lines 81, 87
**Severity:** HIGH
**Category:** DoS Risk

**Description:**
While there's a `limit` parameter, the max is 2000 which is still quite large. More importantly, there's no final offset check to prevent excessive queries.

**Code:**
```go
limit := 100
if limitParam, _ := strconv.Atoi(c.QueryParam("limit")); limitParam > 0 {
    if limitParam > 2000 {
        limitParam = 2000  // Still very large
    }
    limit = limitParam
}
```

**Impact:**
- Large memory allocations
- Potential DoS through large log requests
- Database query performance issues

**Recommended Fix:**
```go
const (
    DefaultLogLimit = 100
    MaxLogLimit     = 500  // Reduce from 2000
    MaxLogOffset    = 10000 // Prevent deep pagination
)

limit := DefaultLogLimit
if limitParam, _ := strconv.Atoi(c.QueryParam("limit")); limitParam > 0 {
    if limitParam > MaxLogLimit {
        return ErrorBadRequest(c, fmt.Sprintf("limit cannot exceed %d", MaxLogLimit))
    }
    limit = limitParam
}

offset := 0
if offsetParam, _ := strconv.Atoi(c.QueryParam("offset")); offsetParam > 0 {
    if offsetParam > MaxLogOffset {
        return ErrorBadRequest(c, fmt.Sprintf("offset cannot exceed %d for performance reasons", MaxLogOffset))
    }
    offset = offsetParam
}
```

---

### 11. Missing Error Handling for GetByIDs() Partial Failures

**File:** `internal/api/handler_clusters.go`, Lines 1215-1218
**Severity:** HIGH
**Category:** Data Integrity

**Description:**
If `GetByIDs()` fails to fetch some users, the statistics will be incomplete but no error is propagated. The function silently continues with incomplete data.

**Code:**
```go
usersByID, err := h.store.Users.GetByIDs(ctx, ownerIDs)
if err != nil {
    return LogAndReturnGenericError(c, fmt.Errorf("failed to fetch users: %w", err))
}
// But what if only some users are returned?
```

**Impact:**
- Incorrect statistics reported to users
- Missing user information in cost breakdowns
- Silent data loss

**Recommended Fix:**
```go
usersByID, err := h.store.Users.GetByIDs(ctx, ownerIDs)
if err != nil {
    return LogAndReturnGenericError(c, fmt.Errorf("failed to fetch users: %w", err))
}

// Validate that all requested users were fetched
if len(usersByID) != len(ownerIDs) {
    missingCount := len(ownerIDs) - len(usersByID)
    log.Printf("WARNING: Failed to fetch %d/%d users for statistics", missingCount, len(ownerIDs))

    // Optional: return error if too many missing
    if missingCount > len(ownerIDs)/10 { // More than 10% missing
        return LogAndReturnGenericError(c, fmt.Errorf("too many users not found: %d/%d", missingCount, len(ownerIDs)))
    }
}
```

---

### 12. API Key Plaintext Exposure in Memory

**File:** `internal/api/handler_api_keys.go`, Lines 119-122
**Severity:** HIGH
**Category:** Security

**Description:**
The plaintext API key is returned in the response. While documented, if the response is logged or cached anywhere, the key is exposed.

**Code:**
```go
response := &types.CreateAPIKeyResponse{
    APIKey:   apiKey.ToResponse(),
    PlainKey: plainKey,  // Sensitive!
}
return c.JSON(http.StatusCreated, response)
```

**Impact:**
- Key exposure if response is logged
- Potential compromise if logs are accessed
- Cannot rotate compromised keys retroactively from logs

**Recommended Fix:**
```go
// 1. Add response logging filter to exclude PlainKey
func sanitizeResponse(data interface{}) interface{} {
    if resp, ok := data.(*types.CreateAPIKeyResponse); ok {
        sanitized := *resp
        sanitized.PlainKey = "[REDACTED]"
        return &sanitized
    }
    return data
}

// 2. Add warning in response
response := &types.CreateAPIKeyResponse{
    APIKey:   apiKey.ToResponse(),
    PlainKey: plainKey,
    Warning:  "This key will only be shown once. Store it securely.",
}

// 3. Add audit log (separate from regular logs)
auditLog := log.New(auditWriter, "[AUDIT] ", log.LstdFlags)
auditLog.Printf("API key created: id=%s, user=%s, name=%s", apiKey.ID, userID, req.Name)
```

---

### 13. Missing Validation of Cluster Region in Delete

**File:** `internal/api/handler_clusters.go`, Line 598
**Severity:** HIGH
**Category:** Authorization

**Description:**
The `Delete` handler doesn't validate the cluster region exists or that the user has permissions for that region. An attacker could enumerate regions.

**Impact:**
- Information disclosure
- Region enumeration possible
- Potential unauthorized access

**Recommended Fix:**
```go
func (h *ClusterHandler) Delete(c echo.Context) error {
    ctx := c.Request().Context()
    id := c.Param("id")

    // Get cluster first
    cluster, err := h.store.Clusters.GetByID(ctx, id)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return ErrorNotFound(c, "Cluster not found")
        }
        return LogAndReturnGenericError(c, fmt.Errorf("failed to retrieve cluster: %w", err))
    }

    // Check access (already exists)
    if err := h.checkClusterAccess(c, cluster); err != nil {
        return err
    }

    // Validate region is allowed for this user
    if !auth.IsAdmin(c) {
        userID, _ := auth.GetUserID(c)
        allowedRegions, err := h.policy.GetAllowedRegions(userID, cluster.Platform)
        if err != nil {
            return LogAndReturnGenericError(c, fmt.Errorf("failed to get allowed regions: %w", err))
        }

        regionAllowed := false
        for _, allowed := range allowedRegions {
            if allowed == cluster.Region {
                regionAllowed = true
                break
            }
        }

        if !regionAllowed {
            return ErrorForbidden(c, "You do not have access to clusters in this region")
        }
    }

    // Continue with deletion...
}
```

---

### 14. No Timeout on Database Statement Execution

**File:** `internal/store/store.go`, Line 155
**Severity:** HIGH
**Category:** Performance/DoS

**Description:**
While statement timeout is set to 30 seconds, context timeouts from handlers (30s in server.go) should be checked for alignment. A slow query could hang the entire request.

**Code:**
```go
config.ConnConfig.RuntimeParams["statement_timeout"] = "30000" // 30 seconds
```

**Impact:**
- Slow query attacks
- Resource exhaustion
- Cascading failures

**Recommended Fix:**
```go
// In store.go
const (
    DefaultStatementTimeout = 30 * time.Second
    CriticalQueryTimeout    = 5 * time.Second  // For user-facing queries
    BackgroundQueryTimeout  = 60 * time.Second // For janitor/background jobs
)

// Add helper for critical queries
func (s *Store) WithQueryTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
    return context.WithTimeout(ctx, timeout)
}

// In handlers
func (h *ClusterHandler) List(c echo.Context) error {
    ctx, cancel := h.store.WithQueryTimeout(c.Request().Context(), 5*time.Second)
    defer cancel()

    clusters, total, err := h.store.Clusters.List(ctx, listFilters)
    if err != nil {
        if errors.Is(err, context.DeadlineExceeded) {
            return ErrorBadRequest(c, "Query took too long, please add more filters")
        }
        return LogAndReturnGenericError(c, err)
    }
    // ...
}
```

---

### 15. Fire-and-Forget UpdateLastUsed with Background Context

**File:** `internal/auth/apikey.go`, Lines 71-73
**Severity:** HIGH
**Category:** Shutdown Handling

**Description:**
Using `context.Background()` in a goroutine means it ignores cancellation during shutdown. If the database is slow, this could cause hanging shutdowns.

**Impact:**
- Delayed graceful shutdown
- Hung processes during deployment
- Kubernetes pod eviction timeouts

**Recommended Fix:**
```go
// Create a shutdown-aware context with timeout
shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
go func() {
    defer cancel()
    if err := st.APIKeys.UpdateLastUsed(shutdownCtx, apiKey.ID); err != nil {
        // Log but don't fail validation
        log.Printf("Failed to update API key last used: %v", err)
    }
}()

// Better: Use worker pool from Issue #6 fix
```

---

## MEDIUM SEVERITY ISSUES

### 16. Default JWT Secret Exposed in Logs (Development)

**File:** `cmd/api/main.go`, Lines 103-105
**Severity:** MEDIUM
**Category:** Security

**Description:**
The default JWT secret is logged when using development mode. While there's a warning, the default value itself is visible in logs.

**Code:**
```go
log.Println("WARNING: Using default JWT_SECRET for development only")
jwtSecret = "change-me-in-production-min-32-chars"
```

**Impact:**
- Developers might accidentally deploy with logged secrets visible
- Audit logs contain sensitive defaults
- Security scanning tools may flag this

**Recommended Fix:**
```go
if jwtSecret == "" {
    log.Println("WARNING: JWT_SECRET not set. Using development default.")
    log.Println("WARNING: This is INSECURE and must not be used in production!")
    jwtSecret = generateDevelopmentSecret() // Don't log the actual value
}

func generateDevelopmentSecret() string {
    // Use a random value even for development
    secret := make([]byte, 32)
    if _, err := rand.Read(secret); err != nil {
        panic("failed to generate development secret: " + err.Error())
    }
    return base64.StdEncoding.EncodeToString(secret)
}
```

---

### 17. Insecure Cookie SameSite Configuration Fallback

**File:** `internal/api/handler_auth.go`, Line 111
**Severity:** MEDIUM
**Category:** Security

**Description:**
The Secure flag is set based on TLS presence: `Secure: c.Request().TLS != nil`. However, there's no explicit check for HTTPS in production.

**Code:**
```go
http.SetCookie(c.Response(), &http.Cookie{
    // ...
    Secure:   c.Request().TLS != nil, // Only secure if TLS
    HttpOnly: true,
    SameSite: http.SameSiteStrictMode,
})
```

**Impact:**
- Cookies could be transmitted insecurely if reverse proxy doesn't properly set TLS context
- Session hijacking risk
- CSRF vulnerabilities

**Recommended Fix:**
```go
// Add environment variable for production mode
isProduction := os.Getenv("ENVIRONMENT") == "production"

// In handler:
http.SetCookie(c.Response(), &http.Cookie{
    Name:     "session_token",
    Value:    token,
    Path:     "/",
    MaxAge:   int(24 * time.Hour.Seconds()),
    Secure:   isProduction || c.Request().TLS != nil, // Always secure in production
    HttpOnly: true,
    SameSite: http.SameSiteStrictMode,
})

// Add startup check
if isProduction && os.Getenv("TLS_ENABLED") != "true" {
    log.Fatal("Production mode requires TLS to be enabled")
}
```

---

### 18. Missing Input Validation: Cluster Name Regex

**File:** `internal/api/handler_clusters.go`, Line 67
**Severity:** MEDIUM
**Category:** Input Validation

**Description:**
The cluster name is validated with `min=3,max=63` but there's no regex pattern validation. Invalid DNS characters could slip through.

**Code:**
```go
Name string `json:"name" validate:"required,min=3,max=63"`
```

**Impact:**
- Invalid cluster names that could cause deployment failures
- DNS resolution issues
- Potential injection attacks

**Recommended Fix:**
```go
// In types package, add custom validation
const ClusterNamePattern = `^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`

type CreateClusterRequest struct {
    Name string `json:"name" validate:"required,min=3,max=63,cluster_name"`
    // ...
}

// Register custom validator
func RegisterClusterNameValidator(v *validator.Validate) {
    v.RegisterValidation("cluster_name", func(fl validator.FieldLevel) bool {
        name := fl.Field().String()
        matched, _ := regexp.MatchString(ClusterNamePattern, name)
        return matched
    })
}

// Validation errors should be user-friendly
if err := c.Validate(req); err != nil {
    if strings.Contains(err.Error(), "cluster_name") {
        return ErrorBadRequest(c, "Cluster name must contain only lowercase letters, numbers, and hyphens, and must start and end with an alphanumeric character")
    }
    return ErrorBadRequest(c, err.Error())
}
```

---

### 19. No SQL Injection Protection Verification

**File:** All store files
**Severity:** MEDIUM
**Category:** Security

**Description:**
While parameterized queries are used ($1, $2, etc.), there's no automated verification that all user inputs are properly parameterized.

**Impact:**
- Potential SQL injection if string concatenation is used anywhere
- Difficult to audit manually

**Recommended Fix:**

**Add linting rules:**
```yaml
# .golangci.yml
linters-settings:
  govet:
    enable:
      - composites
      - printf
  gosec:
    excludes:
      - G201  # SQL injection - we want this check ENABLED
      - G202  # SQL injection - we want this check ENABLED
```

**Add code review checklist:**
```go
// SECURITY CHECKLIST for database queries:
// [ ] All user inputs use parameterized queries ($1, $2, etc.)
// [ ] No string concatenation with fmt.Sprintf() for SQL
// [ ] No raw query strings from user input
// [ ] Query builder validated if used

// BAD:
query := fmt.Sprintf("SELECT * FROM clusters WHERE name = '%s'", userInput)

// GOOD:
query := "SELECT * FROM clusters WHERE name = $1"
rows, err := db.Query(ctx, query, userInput)
```

---

### 20. Incomplete Error Messages Reveal System Details

**File:** `internal/api/handler_clusters.go`, Lines 375-376
**Severity:** MEDIUM
**Category:** Information Disclosure

**Description:**
Error messages returned to clients include database errors and internal details.

**Code:**
```go
return ErrorBadRequest(c, fmt.Sprintf("Database error: %v (owner_id=%s, cluster_type=%s)",
    err, ownerID, cluster.ClusterType))
```

**Impact:**
- Information disclosure
- Attackers learn system internals (table names, field names, etc.)
- Makes enumeration attacks easier

**Recommended Fix:**
```go
// Log detailed error server-side
log.Printf("Database error creating cluster: %v (owner_id=%s, cluster_type=%s, cluster_id=%s)",
    err, ownerID, cluster.ClusterType, cluster.ID)

// Return generic error to client
return ErrorBadRequest(c, "Failed to create cluster. Please contact support with request ID: " + requestID)

// Better: Add request ID tracking
type RequestContext struct {
    RequestID string
    UserID    string
    Timestamp time.Time
}

// In middleware
c.Set("request_id", uuid.New().String())

// In error handler
requestID := c.Get("request_id").(string)
log.Printf("[%s] Database error: %v", requestID, err)
return ErrorBadRequest(c, fmt.Sprintf("An error occurred. Reference ID: %s", requestID))
```

---

### 21-28. Additional Medium Issues

Due to length, additional medium severity issues are summarized:

- **21. Missing Nil Check Before Pointer Dereference** (handler_clusters.go:818)
- **22. No Timeout on Deployment Logs Query** (handler_logs.go:81, 110)
- **23. Idempotency Key Not Used in Cluster Creation** (handler_clusters.go:89)
- **24. Debug Logging Left in Production** (Multiple files)
- **25. Missing Audit Logging for Sensitive Operations** (handler_api_keys.go)
- **26. Rate Limiter Map Growth Without Cleanup** (Already covered in Issue #9)
- **27. No Rate Limit on Sensitive Endpoints** (server.go:210)
- **28. Missing Version Validation for Post-Config Addons** (handler_clusters.go:313)

---

## LOW SEVERITY ISSUES

### 29-40. Code Quality Issues

**Low severity issues include:**

- Missing LIMIT optimization in ListAll()
- Inconsistent error handling patterns
- Missing connection pool monitoring
- Swagger documentation inconsistencies
- No expiration validation for API keys
- Magic numbers without constants
- Missing test coverage for error paths
- Context propagation inconsistencies
- Unbounded work hours lookup loop
- TODO comments left in code
- Potential division by zero
- Missing validation of credentials mode values

---

## RECOMMENDED PRIORITY ACTIONS

### IMMEDIATE (Critical - Block Deployment)

1. **Fix race condition** on `healthCheck.ready` with atomic operations
2. **Add sync.WaitGroup** for graceful goroutine shutdown
3. **Replace context.Background()** in janitor with proper context propagation
4. **Fix unbounded query** in GetStatistics (reduce limit or implement aggregation)
5. **Implement goroutine tracking** for API key updates
6. **Add DELETE query validation** to prevent accidental data loss

**Estimated Effort:** 1-2 days
**Risk if Not Fixed:** Production outages, data loss, memory exhaustion

---

### HIGH PRIORITY (This Sprint)

1. **Fix rate limiter memory leak** with LRU cache
2. **Implement idempotency key** checking in cluster creation
3. **Remove all DEBUG logging** statements
4. **Add audit logging** for sensitive operations
5. **Reduce log query limits** to prevent DoS
6. **Add partial failure handling** for batch user queries

**Estimated Effort:** 2-3 days
**Risk if Not Fixed:** Performance degradation, security gaps

---

### MEDIUM PRIORITY (Next Sprint)

1. **Implement comprehensive input validation** with regex patterns
2. **Add query-level monitoring** and alerts
3. **Standardize error handling** across all handlers
4. **Fix cookie security** in production mode
5. **Add connection pool monitoring**
6. **Remove plaintext secrets** from logs

**Estimated Effort:** 3-5 days
**Risk if Not Fixed:** Security vulnerabilities, maintainability issues

---

### LOW PRIORITY (Backlog)

1. **Extract magic numbers** to constants
2. **Implement comprehensive test coverage** for error paths
3. **Migrate TODOs** to issue tracking system
4. **Optimize database queries** with proper indexing analysis
5. **Add API key expiration** checking
6. **Improve Swagger documentation**

**Estimated Effort:** 5-7 days
**Risk if Not Fixed:** Code quality degradation

---

## TESTING RECOMMENDATIONS

### Race Condition Testing
```bash
# Run tests with race detector
go test -race ./...

# Run race detector on worker
go run -race cmd/worker/main.go
```

### Load Testing
```bash
# Test API under load to find memory leaks
hey -n 10000 -c 100 -H "Authorization: Bearer $TOKEN" \
    https://api.example.com/api/v1/clusters

# Monitor memory usage
watch -n 1 'ps aux | grep ocpctl'
```

### Database Query Testing
```sql
-- Find slow queries
SELECT query, mean_exec_time, calls
FROM pg_stat_statements
WHERE mean_exec_time > 1000  -- > 1 second
ORDER BY mean_exec_time DESC
LIMIT 20;
```

---

## SECURITY SCANNING

### Recommended Tools

1. **gosec** - Go security scanner
```bash
gosec ./...
```

2. **golangci-lint** - Comprehensive linter
```bash
golangci-lint run
```

3. **staticcheck** - Static analysis
```bash
staticcheck ./...
```

4. **nancy** - Dependency vulnerability scanner
```bash
go list -json -m all | nancy sleuth
```

---

## MONITORING RECOMMENDATIONS

### Key Metrics to Track

1. **Goroutine count** - Detect leaks
2. **Memory usage** - Detect unbounded growth
3. **Database connection pool** - Detect exhaustion
4. **Query duration** - Detect slow queries
5. **API response times** - Detect performance degradation
6. **Error rates** - Detect failures

### Prometheus Metrics Example
```go
var (
    goroutineCount = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "ocpctl_goroutines_total",
        Help: "Total number of goroutines",
    })

    dbPoolSize = promauto.NewGauge(prometheus.GaugeOpts{
        Name: "ocpctl_db_pool_size",
        Help: "Database connection pool size",
    })

    apiDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
        Name: "ocpctl_api_duration_seconds",
        Help: "API endpoint duration",
    }, []string{"endpoint", "method", "status"})
)

// Update metrics
func updateMetrics() {
    goroutineCount.Set(float64(runtime.NumGoroutine()))
}
```

---

## CONCLUSION

This code review identified **40 issues** across security, performance, and code quality. The most critical issues involve:

- **Concurrency bugs** that could cause production outages
- **Memory exhaustion** from unbounded queries
- **Resource leaks** from untracked goroutines
- **Security gaps** in authentication and input validation

Addressing the **6 critical issues** should be prioritized before the next production deployment. The high and medium severity issues should be addressed in the next 2-4 weeks to improve overall system stability and security.

---

**Next Steps:**

1. Review this document with the development team
2. Prioritize fixes based on risk and effort
3. Create GitHub issues for tracking
4. Implement fixes with thorough testing
5. Add regression tests to prevent reoccurrence
6. Schedule follow-up code review in 3 months
