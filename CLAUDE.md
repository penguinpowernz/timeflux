# Claude Code Development Guide

This document provides guidance for Claude Code (or other AI assistants) when working on the Timeflux project.

## Project Overview

Timeflux is an InfluxDB v1 API facade that translates requests to TimescaleDB. It allows existing systems using InfluxDB clients to seamlessly switch to TimescaleDB without code changes.

**Key Performance Features:**
- Write-Ahead Log (WAL) for 10x faster writes
- Background index creation (non-blocking)
- Parallel measurement writes
- Streaming COPY operations
- CRC32 checksums for data integrity
- User authentication and authorization with bcrypt password hashing

## Architecture

### Core Components

1. **Write-Ahead Log** (`write/wal_buffer.go`, `write/wal_entry.go`)
   - Provides fast write path with crash recovery
   - CRC32 checksums for corruption detection
   - Snappy compression for reduced I/O
   - Worker pool (8 workers) for background processing
   - Graceful degradation on corruption (skip + alert)
   - Format: `[CRC32][length][snappy(JSON{db, lineprotocol})]`

2. **Schema Manager** (`schema/manager.go`)
   - Manages dynamic schema evolution
   - Handles concurrent writes with per-measurement locking (sync.Map)
   - Maintains in-memory cache of table schemas
   - Coordinates DDL operations (CREATE TABLE, ALTER TABLE)
   - **Background index creation** for tags (non-blocking)
   - Batches DDL in transactions

3. **Line Protocol Parser** (`write/lineprotocol.go`)
   - Parses InfluxDB line protocol format
   - Infers types from line protocol suffixes (i=integer, quoted=string, bare=float, t/f=boolean)
   - Handles escaping and quoted values

4. **Write Handler** (`write/handler.go`)
   - HTTP endpoint for `/write?db={database}`
   - **WAL-enabled**: Appends to WAL and returns immediately (~1-2ms)
   - **WAL-disabled**: Synchronous writes with parallel measurement batching
   - Uses streaming PostgreSQL COPY (no row materialization)
   - Ensures schema exists before writing

5. **Query Translator** (`query/translator.go`)
   - Uses official `influxdata/influxql` parser for AST-based translation
   - Translates InfluxQL to PostgreSQL SQL
   - Handles aggregations, GROUP BY time(), and schema introspection
   - Maps `GROUP BY time(5m)` to `time_bucket('5 minutes', time)`

6. **Query Handler** (`query/handler.go`)
   - HTTP endpoint for `/query?db={database}&q={query}`
   - Executes translated SQL queries
   - Returns results in InfluxDB JSON format

7. **Metrics System** (`metrics/metrics.go`)
   - Tracks writes, queries, schema evolutions, WAL operations
   - Duration statistics (avg/min/max)
   - Exposed via `/metrics` endpoint

8. **Authentication System** (`auth/`)
   - User management with bcrypt password hashing (cost 12)
   - Credential parsing from URL params, Basic Auth, and Token headers
   - Authorization middleware with granular permissions
   - Database and measurement-level access control
   - Wildcard permission support (`*` for all databases/measurements)
   - User and permission management tables (`_timeflux_users`, `_timeflux_user_permissions`)

9. **User Management CLI** (`usercli/user.go`)
   - Command-line interface for user operations
   - User creation with auto-generated passwords
   - Permission granting/revoking
   - User listing and inspection
   - Password reset functionality

## Key Design Patterns

### Write-Ahead Log Pattern

```go
// Fast write path (WAL enabled)
1. Parse line protocol
2. Create WAL entry with checksum: NewWALEntry(database, lineProtocol)
3. Append to WAL (sequential file write)
4. Return 204 No Content immediately

// Background processing (8 workers)
1. Read WAL entry
2. Validate CRC32 checksum
3. Decompress (snappy)
4. Parse points
5. Write to TimescaleDB using COPY
6. Mark as processed

// On corruption
- Log error with details
- Increment WAL corruption metric
- Skip corrupted entry (don't crash)
- Alert operator
- Continue processing next entry
```

### Concurrency Model

```go
// Fast path: Read lock for schema checks
sm.mu.RLock()
if hasAllColumns() {
    sm.mu.RUnlock()
    // proceed with write
}
sm.mu.RUnlock()

// Slow path: Measurement-specific lock for DDL (sync.Map)
lockIface, _ := sm.measurementLocks.LoadOrStore(lockKey, &sync.Mutex{})
lock := lockIface.(*sync.Mutex)
lock.Lock()
defer lock.Unlock()

// Double-check pattern
sm.mu.RLock()
if hasAllColumns() {
    sm.mu.RUnlock()
    return // another goroutine did the work
}
sm.mu.RUnlock()

// Perform DDL in transaction
tx.Begin()
tx.Exec("ALTER TABLE ... ADD COLUMN")
tx.Exec("INSERT INTO metadata ...")
tx.Commit()

// Queue index creation in background
indexQueue <- indexJob{database, measurement, tag, isTag}
```

### Streaming COPY Pattern

```go
// Bad: Materialize all rows in memory
rows := makeRows(points, columns)  // allocates [][]interface{}
CopyFrom(..., pgx.CopyFromRows(rows))  // wraps it again

// Good: Stream rows on-demand
type pointsCopySource struct {
    points  []*Point
    columns []string
    idx     int
    rowBuf  []interface{}  // reuse buffer
}

func (p *pointsCopySource) Next() bool {
    p.idx++
    return p.idx < len(p.points)
}

func (p *pointsCopySource) Values() ([]interface{}, error) {
    // Build row on-demand, reuse buffer
    for j, col := range p.columns {
        p.rowBuf[j] = getValueForColumn(p.points[p.idx], col)
    }
    return p.rowBuf, nil
}
```

### Type Inference

Line protocol → PostgreSQL type mapping:
- `field=42.5` → `DOUBLE PRECISION`
- `field=42i` → `BIGINT`
- `field="value"` → `TEXT`
- `field=true` → `BOOLEAN`

### Data Model

| InfluxDB | TimescaleDB |
|----------|-------------|
| Database | PostgreSQL schema |
| Measurement | Hypertable |
| Tag | TEXT column with index |
| Field | Typed column |
| Timestamp | TIMESTAMPTZ column |

## Development Guidelines

### Adding New InfluxQL Features

1. Check if the `influxdata/influxql` parser already supports it
2. Add translation logic in `query/translator.go`
3. Handle the new AST node type in `translateExpr()` or add a new handler
4. Test with real InfluxQL queries

Example:
```go
case *influxql.SomeNewExpr:
    return t.translateSomeNewExpr(e)
```

### Adding New Aggregate Functions

Add to `translateCall()` in `query/translator.go`:
```go
case "stddev":
    if len(call.Args) > 0 {
        return "STDDEV(" + t.translateExpr(call.Args[0]) + ")"
    }
    return "STDDEV(*)"
```

### Modifying Schema Evolution

Changes to `schema/manager.go` must maintain:
- Thread safety (use locks correctly)
- Idempotent DDL (`IF NOT EXISTS` clauses)
- Metadata table consistency
- In-memory cache updates

### Error Handling Principles

1. **Write errors**: Return HTTP 400 for client errors (bad line protocol), 500 for server errors
2. **Query errors**: Return HTTP 200 with error in JSON (InfluxDB convention)
3. **Log all errors** with context
4. **DDL errors**: Ignore "already exists" errors gracefully
5. **Auth errors**: Return HTTP 401 for authentication failures, 403 for authorization failures

### Modifying Authentication

Changes to authentication/authorization must maintain:
- Bcrypt password security (cost 12)
- Protection of auth tables from HTTP query access
- Proper credential parsing from all supported methods (Basic Auth, Token, query params)
- Permission precedence (specific > wildcard)
- Backward compatibility with InfluxDB clients
- Cascading permission deletion when users are deleted

When adding new auth methods:
1. Add credential parsing in `auth/auth.go::ParseCredentials()`
2. Update middleware in `auth/middleware.go`
3. Ensure auth tables remain inaccessible via query endpoint
4. Test with standard InfluxDB clients (curl, telegraf, etc.)

## Testing Approach

### Manual Testing

Write test:
```bash
curl -i -XPOST 'http://localhost:8086/write?db=testdb' \
  --data-binary 'cpu,host=server1 value=85.3 1620000000000000000'
```

Query test:
```bash
curl -G 'http://localhost:8086/query?db=testdb' \
  --data-urlencode 'q=SELECT mean(value) FROM cpu GROUP BY time(5m)'
```

Authentication test:
```bash
# Create user and grant permission
bin/timeflux user:add testuser testpass
bin/timeflux user:grant testuser testdb:rw

# Test write with auth
curl -i -XPOST -u testuser:testpass 'http://localhost:8086/write?db=testdb' \
  --data-binary 'cpu,host=server1 value=85.3 1620000000000000000'

# Test query with auth
curl -G -u testuser:testpass 'http://localhost:8086/query?db=testdb' \
  --data-urlencode 'q=SELECT mean(value) FROM cpu GROUP BY time(5m)'

# Test unauthorized access
curl -i -XPOST 'http://localhost:8086/write?db=testdb' \
  --data-binary 'cpu value=1'  # Should return 401
```

### Docker Testing

```bash
docker-compose up -d
docker-compose logs -f timeflux
```

## Common Issues and Solutions

### Issue: Compilation errors with influxql package

**Problem**: InfluxQL AST types may change between versions

**Solution**: Check the actual struct definitions in `vendor/github.com/influxdata/influxql/` and adapt the code accordingly. Use type assertions carefully.

### Issue: Deadlocks in schema manager

**Problem**: Incorrect lock ordering or holding locks too long

**Solution**:
- Always use RLock for reads, Lock for writes
- Use per-measurement locks to reduce contention
- Release locks as soon as possible
- Never acquire a second lock while holding one

### Issue: Query translation failures

**Problem**: InfluxQL feature not yet implemented

**Solution**:
1. Log the unsupported AST node type
2. Return clear error message to client
3. Add to documentation/roadmap
4. Implement incrementally

### Issue: Schema cache desync

**Problem**: In-memory cache doesn't match database

**Solution**:
- Always update cache after successful DDL
- Use transactions where appropriate
- Consider adding cache invalidation endpoint for debugging

### Issue: Authentication bypass attempts

**Problem**: Users trying to access auth tables or bypass permissions

**Solution**:
- Auth tables are protected via `IsAuthTableQuery()` check in query handler
- All queries containing `_timeflux_users` or `_timeflux_user_permissions` are rejected
- Permission middleware runs before handlers to prevent unauthorized access
- SQL injection prevented by `pgx.Identifier{}.Sanitize()` usage
- Passwords never logged or returned in API responses

## File Structure Reference

```
/
├── main.go                 # HTTP server, middleware, signal handling, user CLI
├── config/
│   └── config.go          # YAML config parsing, connection strings, auth config
├── schema/
│   └── manager.go         # Schema cache, DDL, metadata table
├── write/
│   ├── handler.go         # Write endpoint, COPY FROM bulk insert
│   ├── lineprotocol.go    # Parser, type inference
│   ├── wal_buffer.go      # WAL buffer, worker pool, recovery
│   └── wal_entry.go       # WAL entry format with CRC32 checksums
├── query/
│   ├── handler.go         # Query endpoint, result formatting
│   └── translator.go      # InfluxQL → SQL AST translation
├── auth/
│   ├── auth.go            # Credential parsing (Basic Auth, Token, query params)
│   ├── middleware.go      # Authentication and authorization middleware
│   └── user_manager.go    # User CRUD, permission management, bcrypt
├── usercli/
│   └── user.go            # User management CLI commands
├── metrics/
│   └── metrics.go         # Metrics collection
├── Dockerfile             # Multi-stage build
├── docker-compose.yml     # TimescaleDB + Timeflux
└── config.yaml            # Docker-ready config
```

## Future Enhancement Ideas

### High Priority
- JWT/Bearer token authentication (currently only username:password)
- Fine-grained measurement extraction from queries for permission checks
- Rate limiting per user/database
- Connection pool monitoring
- More InfluxQL functions (percentile, derivative, difference)
- Audit logging for authentication and permission events

### Medium Priority
- Continuous aggregates support
- Downsampling policies
- Query result caching
- Admin API for schema inspection

### Low Priority
- Multiple instance support with shared cache (Redis)
- InfluxDB v2 compatibility layer with Bearer token support
- Prometheus metrics endpoint (currently JSON format)
- Query optimizer hints
- User session management and token-based sessions
- Role-based access control (RBAC) with predefined roles

## Dependencies

- `github.com/jackc/pgx/v5` - PostgreSQL driver (fast, feature-rich)
- `github.com/influxdata/influxql` - Official InfluxQL parser
- `github.com/tidwall/wal` - Write-ahead log implementation
- `github.com/golang/snappy` - Snappy compression
- `golang.org/x/crypto/bcrypt` - Secure password hashing for authentication
- `gopkg.in/yaml.v3` - YAML config parsing
- `github.com/gin-gonic/gin` - HTTP router and middleware
- `github.com/google/uuid` - UUID generation for request IDs

## Code Style

- Use `gofmt` for formatting
- Prefer explicit error handling over panics
- Log important operations (DDL, errors, slow queries)
- Use `pgx.Identifier{}.Sanitize()` for SQL injection safety
- Comment complex logic, especially locking patterns

## Metadata Table Schema

```sql
CREATE TABLE {schema}._timeflux_metadata (
    measurement TEXT NOT NULL,
    column_name TEXT NOT NULL,
    column_type TEXT NOT NULL,
    is_tag BOOLEAN NOT NULL,
    PRIMARY KEY (measurement, column_name)
);
```

This tracks which columns are tags vs fields, necessary for correct query translation.

## Authentication Tables Schema

```sql
-- Users table
CREATE TABLE _timeflux_users (
    username TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Permissions table
CREATE TABLE _timeflux_user_permissions (
    username TEXT NOT NULL,
    database TEXT NOT NULL,           -- can be '*' for wildcard
    measurement TEXT NOT NULL DEFAULT '',  -- empty means all measurements
    can_read BOOLEAN NOT NULL DEFAULT false,
    can_write BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (username, database, measurement),
    FOREIGN KEY (username) REFERENCES _timeflux_users(username) ON DELETE CASCADE
);
```

Authentication tables are protected from query access via `IsAuthTableQuery()` check in handlers.

## Performance Considerations

### Write Performance

**WAL Enabled (Default):**
- Write latency: 1-2ms (append to file + return)
- Throughput: 500+ batches/second
- Background processing: 8 workers × COPY operations
- Query lag: 1-5 seconds (eventual consistency)
- Crash recovery: Replay WAL on startup
- Overhead: ~12µs for CRC32 + compression per batch

**WAL Disabled (Synchronous):**
- Write latency: 10-30ms (COPY + commit)
- Throughput: 50 batches/second
- Parallel writes across measurements
- No query lag (immediate consistency)
- Uses streaming COPY (no row materialization)

**Schema Evolution:**
- Tag indexes created in background (4 workers, non-blocking)
- DDL batched in transactions (reduces round-trips 3N → 1)
- Per-measurement locking (sync.Map) prevents contention
- Schema cache minimizes database queries

**Optimizations Applied:**
1. Streaming COPY (no double allocation)
2. Parallel measurement writes
3. Background index creation
4. Transaction batching for DDL
5. CRC32 + snappy compression in WAL

### Query Performance
- TimescaleDB time_bucket() is optimized for time-series aggregations
- Tag columns are indexed as (tag_name, time DESC)
- Background index creation completes asynchronously
- Use EXPLAIN ANALYZE for slow queries

### Memory
- Schema cache grows with number of unique measurements and columns
- Each measurement has its own mutex (sync.Map, minimal overhead)
- Connection pool size configurable (default: 32)
- WAL segments auto-rotate at 64MB
- Streaming COPY reduces memory by 30-50% vs materializing rows

## Debugging Tips

1. **Enable debug logging**: Set `logging.level: debug` in config.yaml
2. **Check translated SQL**: Logged before execution
3. **Inspect metadata table**: `SELECT * FROM {schema}._timeflux_metadata;`
4. **Monitor TimescaleDB**: `SELECT * FROM timescaledb_information.hypertables;`
5. **Check locks**: Use PostgreSQL's `pg_locks` view if queries hang
6. **Monitor WAL**:
   - Check metrics: `curl http://localhost:8086/metrics | jq '.wal'`
   - Check WAL directory: `ls -lh /tmp/timeflux/wal/`
   - Watch for corruptions: `grep "WAL corruption" logs`
7. **Monitor performance**:
   - Write latency: `.wal.duration_avg_us` (should be <500µs)
   - Throughput: `.writes.requests / time`
   - Lag: Check `.wal.writes - .wal.replay_success`

## Quick Reference Commands

```bash
# Build
make build        # Build to bin/timeflux
make clean        # Remove built binaries
make test         # Run tests
make run          # Build and run

# Run with custom config
bin/timeflux -config my-config.yaml

# User management
bin/timeflux user:add alice                    # Add user with auto-generated password
bin/timeflux user:add alice mypassword         # Add user with specific password
bin/timeflux user:grant alice mydb:rw          # Grant read/write to database
bin/timeflux user:grant alice mydb.cpu:r       # Grant read to specific measurement
bin/timeflux user:grant alice "*:rw"           # Grant read/write to all databases
bin/timeflux user:revoke alice mydb.cpu        # Revoke specific permission
bin/timeflux user:reset-password alice newpw   # Reset password
bin/timeflux user:list                         # List all users
bin/timeflux user:show alice                   # Show user details and permissions
bin/timeflux user:delete alice                 # Delete user

# Docker commands
make up           # Start Docker Compose services
make down         # Stop Docker Compose services
make logs         # Follow timeflux logs
make reup         # Rebuild and restart timeflux container
make dcl          # Stop and remove volumes

# Database inspection
docker exec -it timeflux-timescaledb psql -U postgres -d timeseries
\dn  # list schemas
\dt mydb.*  # list tables in schema
SELECT * FROM mydb._timeflux_metadata;
SELECT * FROM _timeflux_users;
SELECT * FROM _timeflux_user_permissions;
```

## When Modifying This Project

1. **Test write path**: Ensure new line protocol variations work
2. **Test query path**: Add new InfluxQL query examples
3. **Update README**: Document new features or limitations
4. **Check concurrency**: Verify schema evolution still works under concurrent load
5. **Update CLAUDE.md**: Document architectural changes here

## Resources

- InfluxDB Line Protocol: https://docs.influxdata.com/influxdb/v1.8/write_protocols/line_protocol_tutorial/
- InfluxQL Reference: https://docs.influxdata.com/influxdb/v1.8/query_language/
- TimescaleDB Docs: https://docs.timescale.com/
- pgx Documentation: https://pkg.go.dev/github.com/jackc/pgx/v5
