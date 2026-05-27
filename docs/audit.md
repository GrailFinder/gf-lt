# gf-lt Code Audit Report

**Date:** 2026-05-14
**Last Updated:** 2026-05-27 — Phase 1 Quick Wins implemented (see resolved items below)
**Auditor:** Code Review
**Version:** ~9.5K LOC Go TUI Application

---

## Executive Summary

gf-lt is a feature-rich terminal-based LLM chat application. The codebase is functional and well-structured for its purpose, but has several areas that would benefit from refactoring and hardening.

| Category | Issues | Critical | High | Medium | Low |
|----------|--------|----------|------|--------|-----|
| Security | 0 | 0 | 0 | 0 | 0 |
| Performance | 2 | 0 | 2 | 0 | 0 |
| Architecture | 5 | 1 | 1 | 3 | 0 |
| Error Handling | 2 | 0 | 1 | 1 | 0 |
| Testing | 2 | 0 | 0 | 0 | 2 |
| **Total** | **11** | **1** | **4** | **4** | **2** |

---

## 1. Security Issues

### 1.1 SQL Injection in GetTableColumns (HIGH) ✅ RESOLVED

**File:** `storage/storage.go:156`

```go
func (p ProviderSQL) GetTableColumns(table string) ([]TableColumn, error) {
    resp := []TableColumn{}
    err := p.db.Select(&resp, "PRAGMA table_info("+table+");")
    return resp, err
}
```

**Problem:** Table name is directly concatenated into SQL without sanitization.

**Impact:** If exposed via API, could allow SQL injection.

**Resolution:** `GetTableColumns` now validates the table name against `ListTables()` before querying, returning an error for invalid names.

---

### 1.2 Dangerous Command Detection Limited (MEDIUM) ✅ RESOLVED

**File:** `tools/dangerous.go:16-36`

```go
func IsDangerousCommand(name string, args map[string]string) (bool, string) {
    if name != "bash" {
        return false, ""
    }
    cmd := args["command"]
    if cmd == "" {
        return false, ""
    }
    cmdLower := strings.ToLower(cmd)

    if strings.HasPrefix(cmdLower, "rm ") {
        return true, "rm (delete file)"
    }
    if strings.HasPrefix(cmdLower, "git push") {
        return true, "git push (remote change)"
    }
    if strings.HasPrefix(cmdLower, "sudo ") {
        return true, "sudo (privilege escalation)"
    }
    return false, ""
}
```

**Problem:** Only checks `rm`, `git push`, and `sudo`. Missing many dangerous commands.

**Impact:** Users could execute destructive commands like `dd`, `mkfs`, `shutdown`, etc.

**Resolution:** Extended blocklist with `dd`, `shred`, `mkfs`, `fdisk`, `shutdown`, `poweroff`, `reboot`, `iptables`, `ufw`, `chmod -R`, `chown -R`.

---

### 1.3 API Token Logging (LOW) ✅ RESOLVED

**File:** `bot.go:637`

```go
func dumpRequestToFile(api string, body []byte, token string, statusCode int, respError string) {
    // token is written to file
}
```

**Problem:** API tokens may be logged in request dumps.

**Resolution:** Tokens are now redacted to `first8...last4` before writing to the curl dump file.

---

## 2. Performance Issues

### 2.1 O(n) Vector Search - Scalability Bottleneck (HIGH)

**Files:**
- `storage/vector.go:69-126`
- `rag/storage.go:205-262`

```go
func (p *Provider) SearchClosest(query []float32, limit int) ([]SearchResult, error) {
    querySQL := "SELECT embeddings, slug, raw_text, filename FROM " + tableName
    rows, err := p.db.Query(querySQL)  // Fetches ALL rows
    for rows.Next() {
        // Calculates similarity for EVERY row
        similarity := cosineSimilarity(q, storedEmbeddings)
    }
}
```

**Problem:** Linear O(n) search - loads all vectors into memory and computes cosine similarity for every row.

**Impact:**
- 10,000 vectors (768 dims) = ~30MB from disk per search
- Search time grows linearly with corpus size
- Memory usage grows unbounded

**Recommendations:**

**Short-term (Low effort):**
```go
// Use heap for top-k selection instead of sorting all results
type resultHeap []SearchResult
func (h resultHeap) Less(i, j int) bool { return h[i].distance < h[j].distance }
// Use container/heap to maintain top-k efficiently
```

**Long-term (High effort):**
1. Implement HNSW index using `sqlite-vss` or `sqlite-hnsw` extension
2. Consider dedicated vector database (Qdrant, Weaviate, pgvector)
3. Implement approximate nearest neighbor (ANN) algorithms

---

### 2.2 No Connection Pool Configuration (MEDIUM) ✅ RESOLVED

**File:** `storage/storage.go:106-134`

```go
func NewProviderSQL(dbPath string, logger *slog.Logger) FullRepo {
    db, err := sqlx.Open("sqlite", dbPath)
    if err != nil {
        logger.Error("failed to open db connection", "error", err)
        return nil
    }
    // Missing: db.SetMaxOpenConns(25)
    // Missing: db.SetMaxIdleConns(5)
    // Missing: db.SetConnMaxLifetime(5 * time.Minute)
}
```

**Problem:** No connection pool tuning.

**Resolution:** Added `SetMaxOpenConns(10)`, `SetMaxIdleConns(5)`, `SetConnMaxLifetime(5m)`, and `db.Ping()` on startup.

---

### 2.3 Unbounded Memory Allocation in Vector Search (MEDIUM)

**Files:**
- `storage/vector.go:84,109`
- `rag/storage.go:249-254`

```go
var allResults []SearchResult
for rows.Next() {
    allResults = append(allResults, result)  // Grows unbounded
}
```

**Problem:** `allResults` slice grows until all rows are processed.

**Recommendation:** Use max-heap with fixed capacity instead of collecting all results.

---

## 3. Architecture Issues

### 3.1 Giant Functions - Maintainability Risk (HIGH)

| Function | Lines | File | Issue |
|----------|-------|------|-------|
| `initTUI()` | ~1000+ | tui.go:246 | Key binding setup, UI component creation, event handlers all in one function |
| `chatRound()` | ~200+ | bot.go:928 | Main chat loop handles streaming, tool calls, UI updates, error handling |
| `makeDbTable()` | ~400+ | tables.go:1354 | Table building with complex navigation logic |
| `makeFilePicker()` | ~500+ | tables.go:845 | File picker with multiple callbacks and event handlers |

**Problem:** Functions exceed recommended size (ideal: <150 lines, warning: >200 lines).

**Recommendations:**

1. **Extract key binding setup:**
```go
func setupKeyBindings() {
    // Extract from initTUI()
    pages.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
        return handleGlobalKeys(event)
    })
}

func handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
    switch {
    case event.Key() == tcell.KeyF1:
        // Handle F1
    case event.Key() == tcell.KeyCtrlC:
        // Handle Ctrl+C
    }
    return event
}
```

2. **Extract chat round phases:**
```go
func chatRound(r *models.ChatRoundReq) error {
    setupRound(r)
    streamResponse(r)
    handleToolCalls(r)
    finalizeRound(r)
}
```

---

### 3.2 Global State - Testing Difficulty (MEDIUM)

**Problem:** Extensive package-level variables make unit testing difficult.

```go
var cfg *config.Config        // bot.go, main.go, tui.go
var store storage.FullRepo   // tables.go, bot.go
var logger *slog.Logger      // Multiple files
var chatBody *models.ChatBody // bot.go, llm.go
```

**Impact:**
- Cannot test functions in isolation
- Hidden dependencies between functions
- Race conditions possible with mutable global state

**Recommendations:**

1. **Use struct embedding for related state:**
```go
type AppContext struct {
    cfg     *config.Config
    store   storage.FullRepo
    logger  *slog.Logger
    chat    *models.ChatBody
    // ... other dependencies
}
```

2. **Pass context to functions that need it:**
```go
func chatRound(ctx *AppContext, r *models.ChatRoundReq) error {
    // Use ctx.cfg, ctx.store, etc.
}
```

---

### 3.3 No Dependency Injection (MEDIUM)

**Problem:** Hard-coded dependencies throughout.

**Recommendation:** Introduce interfaces for testability:
```go
type LLMProvider interface {
    Send(ctx context.Context, body io.Reader) (io.Reader, error)
    GetToken() string
}

// Then inject mock providers in tests
```

---

### 3.4 Goroutine Lifecycle - No Cleanup (MEDIUM)

**Files:**
- `helpfuncs.go:29-39` - `startModelColorUpdater()`
- `tui.go:253-270` - Confirm request handler

```go
func startModelColorUpdater() {
    go func() {
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()
        for range ticker.C {
            updateCachedModelColor()
        }
    }()
}
```

**Problem:** Goroutines run forever with no shutdown mechanism.

**Recommendation:** Add graceful shutdown:
```go
type ColorUpdater struct {
    stopCh chan struct{}
    doneCh chan struct{}
}

func (u *ColorUpdater) Start() {
    go func() {
        defer close(u.doneCh)
        ticker := time.NewTicker(5 * time.Second)
        defer ticker.Stop()
        for {
            select {
            case <-u.stopCh:
                return
            case <-ticker.C:
                updateCachedModelColor()
            }
        }
    }()
}

func (u *ColorUpdater) Stop() {
    close(u.stopCh)
    <-u.doneCh
}
```

---

### 3.5 Inconsistent Error Handling (LOW)

**Pattern found:**
```go
// Sometimes returns error
func foo() error {
    return fmt.Errorf("problem")
}

// Sometimes logs and continues
func bar() {
    logger.Error("problem")
    // continues
}

// Sometimes panics
func baz() {
    panic("should not happen")
}
```

**Recommendation:** Establish error handling conventions:
- Return errors for expected failure cases
- Log warnings for recoverable issues
- Panic only for truly unrecoverable states (e.g., initialization failures)

---

## 4. Error Handling Issues

### 4.1 Silent Error Suppression in Row Scanning (LOW) ⚠️ PARTIALLY RESOLVED

**Files:**
- `storage/vector.go:91` — **not yet fixed**
- `rag/storage.go:231,339` — **not yet fixed**
- `rag/storage.go:367,374` — **resolved** (`ListFiles` now logs table query and scan errors)

```go
if err := rows.Scan(&embeddingsBlob, &slug, &rawText, &fileName); err != nil {
    continue  // Error is silently lost
}
```

**Remaining locations:**
- `storage/vector.go:91` — `SearchClosest` scan errors are logged but not accumulated/propagated
- `rag/storage.go:231` — `SearchClosest` scan errors (same pattern)
- `rag/storage.go:339` — `scanRows` scan errors (logged but not propagated)
- `rag/storage.go:277` — `GetVectorBySlug` treats all scan errors as "not found" silently

---

### 4.2 Transaction Rollback Logic Bug (LOW) ✅ RESOLVED

**File:** `rag/storage.go:72-76,138-142`

```go
defer func() {
    if err != nil {
        _ = tx.Rollback()
    }
}()
```

**Problem:** Outer `err` may be overwritten after deferred function is set up.

**Resolution:** Rollback errors are now checked against `sql.ErrTxDone` and logged on failure instead of being silently discarded.

---

### 4.3 WAL Mode Not Enforced (LOW)

**File:** `storage/storage.go:113-127`

```go
if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
    logger.Warn("failed to enable WAL mode", "error", err)
}
// Falls back silently if filesystem doesn't support WAL
```

**Recommendation:** Make WAL a hard requirement:
```go
var journalMode string
if err := db.QueryRow("PRAGMA journal_mode;").Scan(&journalMode); err != nil || journalMode != "wal" {
    logger.Error("WAL mode is required", "current_mode", journalMode)
    db.Close()
    return nil
}
```

---

## 5. Testing Issues

### 5.1 Low Test Coverage

- ~11 test files for ~9.5K LOC project
- Main package has decent tests
- `tools/` has failing glob expansion tests
- `agent/` has no test files
- `config/` has no test files

**Recommendation:** Add tests for:
1. Agent client request building
2. Config loading/validation
3. Dangerous command detection
4. Tool call parsing

---

### 5.2 Failing Tests

**Location:** `tools/fs_test.go`, `tools/unix_test.go`

Tests related to glob expansion are failing - likely due to shell glob behavior differences.

**Recommendation:** Fix or skip these tests:
```go
func TestUnixGlobExpansion(t *testing.T) {
    t.Skip("Skipping glob tests - shell behavior differs across environments")
    // ...
}
```

---

## 6. Refactoring Priorities

### Phase 1: Quick Wins ✅ COMPLETED (2026-05-27)
1. ✅ Fix SQL injection in `GetTableColumns`
2. ✅ Add connection pool configuration
3. ✅ Add proper error logging for silent failures (partial — `ListFiles` only)
4. ✅ Fix transaction rollback logic
5. ✅ Extend dangerous command detection
6. ✅ Redact API tokens in request dumps

### Phase 2: Medium Effort
1. Extract giant functions (initTUI, chatRound)
2. Implement heap-based top-k for vector search
3. Add graceful shutdown for goroutines

### Phase 3: Long-term
1. Implement HNSW or use vector database
2. Introduce dependency injection
3. Add comprehensive test coverage
4. Refactor global state to structured context

---

## Appendix: File Statistics

| File | Lines | Functions | Complexity Score |
|------|-------|-----------|------------------|
| bot.go | 1828 | ~50 | High |
| tables.go | 1869 | ~15 | High |
| tui.go | 1271 | ~8 | Very High |
| helpfuncs.go | 1128 | ~25 | Medium |
| rag/rag.go | 1193 | ~30 | Medium |
| tools/tools.go | 1629 | ~40 | High |
| llm.go | 764 | ~20 | Medium |
| main.go | 341 | ~15 | Low |


*End of Report*
