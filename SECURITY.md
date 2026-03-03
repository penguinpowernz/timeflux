# Timeflux Security Documentation

This document details the security measures, mitigations, and potential security considerations for Timeflux. It is intended to help potential users assess security posture and calculate threat models for their deployments.

**Last Updated:** 2026-03-03
**Version:** 1.0

---

## Table of Contents

1. [Security Overview](#security-overview)
2. [Authentication & Authorization](#authentication--authorization)
3. [Data Integrity](#data-integrity)
4. [SQL Injection Prevention](#sql-injection-prevention)
5. [Network Security](#network-security)
6. [Data at Rest](#data-at-rest)
7. [Data in Transit](#data-in-transit)
8. [Logging & Monitoring](#logging--monitoring)
9. [Known Limitations & Blind Spots](#known-limitations--blind-spots)
10. [Threat Model Considerations](#threat-model-considerations)
11. [Security Best Practices](#security-best-practices)
12. [Incident Response](#incident-response)

---

## Security Overview

Timeflux is an InfluxDB v1 API facade that translates requests to TimescaleDB/PostgreSQL. Security is implemented at multiple layers:

- **Authentication**: Username/password with bcrypt hashing (cost 12)
- **Authorization**: Granular database and measurement-level permissions
- **Data Integrity**: CRC32 checksums for WAL entries, PostgreSQL ACID guarantees
- **SQL Injection Prevention**: Parameterized queries and identifier sanitization
- **Access Control**: Protected system tables, credential validation

**Security Model**: Defense in depth with authentication → authorization → input validation → query execution layers.

---

## Authentication & Authorization

### Authentication Mechanisms

#### Supported Methods

Timeflux supports multiple authentication methods for InfluxDB client compatibility:

1. **HTTP Basic Authentication**
   ```bash
   curl -u username:password http://localhost:8086/query?db=mydb&q=SELECT+*+FROM+cpu
   ```
   - Standard `Authorization: Basic <base64(username:password)>` header
   - Parsed in `auth/auth.go::ParseCredentials()`

2. **Query Parameter Authentication**
   ```bash
   curl http://localhost:8086/query?u=username&p=password&db=mydb&q=SELECT+*+FROM+cpu
   ```
   - Legacy InfluxDB v1 compatibility
   - Parameters: `u` (username), `p` (password)

3. **Token Authentication (Influx-Compatible)**
   ```bash
   curl -H "Authorization: Token username:password" http://localhost:8086/query?db=mydb&q=SELECT+*
   ```
   - Format: `Authorization: Token <username:password>`
   - InfluxDB v1 token format (not JWT)

**Credential Precedence**: Basic Auth > Token Header > Query Parameters

#### Password Storage

- **Hashing Algorithm**: bcrypt with cost factor 12
- **Implementation**: `golang.org/x/crypto/bcrypt`
- **Cost Factor Rationale**: Cost 12 provides ~250ms verification time, balancing security and performance
- **Salt**: Bcrypt automatically generates cryptographically random salts per password
- **Storage Location**: `_timeflux_users.password_hash` column in TimescaleDB

**Security Properties**:
- Passwords never stored in plaintext
- Passwords never logged or returned in API responses
- Each password has unique salt (bcrypt default)
- Rainbow table attacks infeasible
- Cost factor can be increased in future (backward compatible)

#### Authentication Implementation

**Code Location**: `auth/auth.go`

```go
// Credential parsing order
func ParseCredentials(r *http.Request) (username, password string, found bool) {
    // 1. HTTP Basic Auth (highest priority)
    if u, p, ok := r.BasicAuth(); ok {
        return u, p, true
    }

    // 2. Authorization Token header
    if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Token ") {
        // Parse username:password from token
    }

    // 3. Query parameters (lowest priority)
    username = r.URL.Query().Get("u")
    password = r.URL.Query().Get("p")
}
```

**Verification**:
```go
storedHash := getUserPasswordHash(username)  // From database
err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(password))
if err != nil {
    return false  // Authentication failed
}
```

**Protection Mechanisms**:
- No timing-based username enumeration (bcrypt comparison runs even for non-existent users in some paths)
- Failed authentication returns generic 401 error (no username validation hints)
- Passwords truncated at 72 bytes (bcrypt limitation) - consider pre-hashing for longer passwords if needed

### Authorization System

#### Permission Model

**Granularity Levels**:
1. **Database-level**: `database:permissions`
2. **Measurement-level**: `database.measurement:permissions`
3. **Wildcard**: `*:permissions` (all databases)

**Permission Types**:
- `r` (read): SELECT, SHOW queries
- `w` (write): INSERT/write operations
- `rw` (read-write): Both read and write

#### Permission Storage

**Table Schema**:
```sql
CREATE TABLE _timeflux_user_permissions (
    username TEXT NOT NULL,
    database TEXT NOT NULL,           -- can be '*' for wildcard
    measurement TEXT NOT NULL DEFAULT '',  -- empty '' means all measurements
    can_read BOOLEAN NOT NULL DEFAULT false,
    can_write BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (username, database, measurement),
    FOREIGN KEY (username) REFERENCES _timeflux_users(username) ON DELETE CASCADE
);
```

**Cascading Deletion**: When a user is deleted, all permissions are automatically removed via `ON DELETE CASCADE`.

#### Permission Evaluation

**Code Location**: `auth/middleware.go::CheckPermission()`

**Evaluation Order** (first match wins):
1. Check `database.measurement` specific permission
2. Check `database` wildcard permission (empty measurement)
3. Check `*` global wildcard permission
4. Deny access (default deny)

**Example**:
```
User has permissions:
- mydb.cpu:r (read CPU measurement)
- mydb:w (write any measurement in mydb)
- *:r (read any database)

Query: SELECT * FROM mydb.cpu → Allowed (rule 1: specific read)
Query: INSERT INTO mydb.cpu → Denied (rule 1: no write on specific)
Query: INSERT INTO mydb.memory → Allowed (rule 2: database write)
Query: SELECT * FROM otherdb.disk → Allowed (rule 3: global read)
Query: INSERT INTO otherdb.disk → Denied (default deny)
```

#### Protected Tables

**System Tables** (inaccessible via query endpoint):
- `_timeflux_users`
- `_timeflux_user_permissions`
- `_timeflux_metadata` (accessible, contains schema info only)

**Protection Mechanism** (`query/handler.go`):
```go
func IsAuthTableQuery(query string) bool {
    lowerQuery := strings.ToLower(query)
    return strings.Contains(lowerQuery, "_timeflux_users") ||
           strings.Contains(lowerQuery, "_timeflux_user_permissions")
}

// In query handler
if IsAuthTableQuery(queryStr) {
    return http.StatusForbidden, "Access to authentication tables is forbidden"
}
```

**Blind Spot**: This is a simple string-based check. Sophisticated obfuscation might bypass it (see [Known Limitations](#known-limitations--blind-spots)).

#### Authorization Middleware

**Code Location**: `auth/middleware.go`

**Flow**:
1. Parse credentials from request
2. Authenticate user (bcrypt verification)
3. Determine operation type (read/write) from HTTP method and query
4. Extract database from query parameters
5. Check permissions for user + database + operation
6. Allow or deny request

**HTTP Status Codes**:
- `401 Unauthorized`: Authentication failed (invalid credentials)
- `403 Forbidden`: Authorization failed (valid credentials, insufficient permissions)
- `200 OK`: Query succeeded but may contain error in JSON (InfluxDB convention)

**Measurement-Level Extraction**:
Currently, measurement-level permissions are checked only when explicitly provided. For complex queries, measurement extraction from InfluxQL AST is limited.

**Current Limitation**: If a user has `mydb:r` but query accesses `mydb.sensitive`, permission check may not catch measurement-level restrictions in all query types. See [Known Limitations](#known-limitations--blind-spots).

---

## Data Integrity

### Write-Ahead Log (WAL) Integrity

Timeflux uses a Write-Ahead Log for fast writes with crash recovery. Data integrity is ensured through checksums and validation.

#### WAL Entry Format

**Binary Structure** (`write/wal_entry.go`):
```
[4 bytes: CRC32 checksum]
[8 bytes: length of compressed data]
[N bytes: snappy-compressed JSON payload]
```

**JSON Payload**:
```json
{
  "database": "mydb",
  "lineProtocol": "cpu,host=server1 value=85.3 1620000000000000000"
}
```

#### CRC32 Checksum

**Algorithm**: IEEE CRC32 polynomial (0xEDB88320)
**Library**: Go standard library `hash/crc32`
**Scope**: Checksum covers compressed payload only (not length field)

**Creation** (`write/wal_entry.go::NewWALEntry()`):
```go
compressed := snappy.Encode(nil, jsonData)
checksum := crc32.ChecksumIEEE(compressed)
```

**Validation** (`write/wal_buffer.go::processWALEntry()`):
```go
decompressed, err := snappy.Decode(nil, compressedData)
if err != nil {
    log.Error("WAL decompression failed", "error", err)
    metrics.IncrementWALCorruption()
    return // Skip corrupted entry
}

calculatedChecksum := crc32.ChecksumIEEE(compressedData)
if calculatedChecksum != storedChecksum {
    log.Error("WAL corruption detected", "expected", storedChecksum, "actual", calculatedChecksum)
    metrics.IncrementWALCorruption()
    return // Skip corrupted entry
}
```

#### Corruption Handling

**Graceful Degradation**:
1. Detect corrupted entry (CRC32 mismatch or decompression failure)
2. Log error with details (checksum values, entry position)
3. Increment WAL corruption metric (`.wal.corruption_count`)
4. **Skip corrupted entry** (do not crash)
5. Continue processing next entry

**Data Loss Risk**: Corrupted WAL entries are discarded. This prevents cascading failures but results in data loss for the affected batch.

**Mitigation Strategies**:
- Monitor `.wal.corruption_count` metric (should be zero)
- Use reliable storage for WAL directory (ECC RAM, enterprise SSDs)
- Regular WAL segment backups (not implemented, manual process)

**Alert Recommended**: Set up alerts for any non-zero corruption count.

### PostgreSQL ACID Guarantees

Once data is written to TimescaleDB, PostgreSQL's ACID properties ensure:
- **Atomicity**: Transactions commit fully or not at all
- **Consistency**: Foreign key constraints (user deletion cascades)
- **Isolation**: Concurrent writes don't interfere (serializable isolation available)
- **Durability**: WAL-backed persistence (PostgreSQL's WAL, not Timeflux's)

**DDL Operations**: Schema changes (ALTER TABLE) are transactional in PostgreSQL.

### Snappy Compression

**Purpose**: Reduce WAL I/O overhead (~3-5x compression ratio for time-series data)
**Library**: `github.com/golang/snappy`
**Security Consideration**: Snappy is not cryptographically secure compression (no encryption). WAL files are readable if storage is compromised.

---

## SQL Injection Prevention

### Primary Defense: Parameterized Queries

**pgx Library**: `github.com/jackc/pgx/v5` automatically parameterizes query values.

**Example** (`write/handler.go`):
```go
// Safe: values are parameterized
_, err := tx.CopyFrom(
    ctx,
    pgx.Identifier{database, measurement},
    columns,
    pgx.CopyFromSource(pointsCopySource),
)
```

**Example** (`query/translator.go`):
```go
// Safe: column names sanitized with Identifier
columns := pgx.Identifier{columnName}.Sanitize()
sql := "SELECT " + columns + " FROM " + pgx.Identifier{schema, table}.Sanitize()
```

### Identifier Sanitization

**Method**: `pgx.Identifier{}.Sanitize()`
**Purpose**: Safely quote SQL identifiers (database names, table names, column names)

**Implementation**:
- Encloses identifiers in double quotes
- Escapes internal double quotes (e.g., `my"table` → `"my""table"`)
- Prevents SQL injection via identifier manipulation

**Example**:
```go
// User input: database = "mydb; DROP TABLE users; --"
safeDB := pgx.Identifier{database}.Sanitize()
// Result: "mydb; DROP TABLE users; --" (quoted, safe)

// SQL: SELECT * FROM "mydb; DROP TABLE users; --"."cpu"
// PostgreSQL interprets this as a literal database name (will fail to find it)
```

### InfluxQL Parser as Defense Layer

**Parser**: `github.com/influxdata/influxql` (official InfluxDB parser)
**AST-Based Translation**: Queries are parsed into Abstract Syntax Tree before translation

**Security Benefit**:
- Only valid InfluxQL queries are accepted (malformed queries rejected)
- AST nodes are type-safe (no string concatenation of user input)
- SQL is generated from AST structure, not raw input

**Example**:
```go
// User query: "SELECT * FROM cpu; DROP TABLE users; --"
q, err := influxql.ParseQuery(userQuery)
if err != nil {
    return err // Rejected: invalid InfluxQL syntax
}

// Only valid AST nodes are translated
for _, stmt := range q.Statements {
    sql := translateStatement(stmt) // Type-safe translation
}
```

### Input Validation

**Database Names**: Validated in `schema/manager.go::EnsureDatabaseExists()`
```go
// Database names must match PostgreSQL identifier rules
// Sanitized via pgx.Identifier before any SQL execution
```

**Measurement Names**: Sanitized before schema operations
```go
tableName := pgx.Identifier{database, measurement}.Sanitize()
```

**Line Protocol Parsing**: Custom parser in `write/lineprotocol.go`
- Handles escaping (`\,`, `\ `, `\=`, `\"`)
- Validates field value types (integer, float, boolean, string)
- Rejects malformed input (returns 400 Bad Request)

### Known SQL Injection Risks

**1. Protected Table Query Check** (`query/handler.go::IsAuthTableQuery()`):
- Simple string-based detection
- May be bypassed with creative formatting (e.g., Unicode characters, comments)
- **Mitigation**: PostgreSQL permissions should be configured to prevent query user from accessing auth tables directly

**2. Schema Introspection Queries**:
- `SHOW DATABASES`, `SHOW MEASUREMENTS`, `SHOW TAG KEYS` are translated to PostgreSQL system queries
- These queries access `information_schema` and `pg_catalog`
- **Risk**: Information disclosure about database structure
- **Mitigation**: Authorization checks still apply (user must have read permission on database)

**3. Dynamic SQL in Translator**:
- Some SQL is constructed via string concatenation (after sanitization)
- Complex InfluxQL queries may have edge cases
- **Mitigation**: All identifiers sanitized, values parameterized

**Recommendation**: Run Timeflux with a PostgreSQL user that has minimal privileges (CONNECT, USAGE on schemas, SELECT/INSERT on specific tables only).

---

## Network Security

### Transport Layer

**Current State**: HTTP only (no TLS)

**Exposure**:
- Credentials transmitted in plaintext (Basic Auth base64 is not encryption)
- Query data and results visible to network observers
- Vulnerable to man-in-the-middle attacks

**Mitigation Options**:

1. **Reverse Proxy with TLS**:
   ```
   [Client] --HTTPS--> [Nginx/Caddy] --HTTP--> [Timeflux]
   ```
   - Recommended for production deployments
   - Proxy handles TLS termination, certificate management
   - Timeflux runs on localhost or private network

2. **TLS Support in Timeflux** (not implemented):
   - Would require adding `http.ListenAndServeTLS()`
   - Certificate management (Let's Encrypt, custom CA)
   - Configuration for cert/key paths

**Recommendation**: Always deploy behind TLS-terminating reverse proxy in production.

### Network Exposure

**Default Configuration**:
- Listens on `0.0.0.0:8086` (all interfaces)
- No built-in IP allowlisting
- No rate limiting

**Mitigation Strategies**:

1. **Firewall Rules**:
   ```bash
   # iptables example: allow only specific IPs
   iptables -A INPUT -p tcp --dport 8086 -s 192.168.1.0/24 -j ACCEPT
   iptables -A INPUT -p tcp --dport 8086 -j DROP
   ```

2. **Bind to Localhost** (if proxy used):
   ```yaml
   # config.yaml
   server:
     host: 127.0.0.1
     port: 8086
   ```

3. **Network Segmentation**:
   - Deploy Timeflux in private network/VPC
   - Expose only via VPN or bastion host

4. **Container Networking**:
   ```yaml
   # docker-compose.yml
   services:
     timeflux:
       ports:
         - "127.0.0.1:8086:8086"  # Bind to localhost only
   ```

### Denial of Service (DoS)

**Current Protections**: None built-in

**Vulnerabilities**:
1. **Large Query Results**: No result size limits
2. **Expensive Queries**: `SELECT * FROM measurement` on large tables
3. **Connection Exhaustion**: No connection rate limiting
4. **Large Write Batches**: No batch size limits on write endpoint

**Mitigation Strategies**:

1. **Reverse Proxy Rate Limiting**:
   ```nginx
   # Nginx example
   limit_req_zone $binary_remote_addr zone=timeflux:10m rate=10r/s;
   location / {
       limit_req zone=timeflux burst=20;
       proxy_pass http://localhost:8086;
   }
   ```

2. **PostgreSQL Resource Limits**:
   ```sql
   ALTER ROLE timeflux_user SET statement_timeout = '30s';
   ALTER ROLE timeflux_user SET work_mem = '128MB';
   ```

3. **Connection Pool Configuration** (`config.yaml`):
   ```yaml
   database:
     max_connections: 32  # Limit concurrent connections
   ```

4. **WAL Segment Size Limits** (prevents disk exhaustion):
   - WAL auto-rotates at 64MB per segment
   - Old segments retained (no auto-cleanup currently)
   - **Recommendation**: Monitor WAL directory size, implement retention policy

**Future Enhancement**: Rate limiting per user/IP address.

---

## Data at Rest

### TimescaleDB/PostgreSQL Storage

**Encryption**: Not enabled by default

**Options**:

1. **Filesystem-Level Encryption**:
   - LUKS (Linux)
   - dm-crypt
   - BitLocker (Windows)
   - FileVault (macOS)
   - **Pros**: Transparent, protects against physical theft
   - **Cons**: No protection if OS compromised

2. **PostgreSQL Transparent Data Encryption (TDE)**:
   - Available in some PostgreSQL distributions (not standard)
   - Encrypts data files, WAL logs
   - Requires key management

3. **Application-Level Encryption**:
   - Encrypt field values before writing to Timeflux
   - **Cons**: Breaks aggregation queries (can't `SUM()` encrypted values)

**Recommendation**: Use filesystem-level encryption for production deployments.

### WAL Files

**Location**: Configurable in `config.yaml` (default: `/tmp/timeflux/wal/`)

**Exposure**:
- WAL files contain unencrypted data (snappy compression only)
- Readable by anyone with filesystem access
- May persist after crash (recovery on restart)

**Mitigation**:
1. **Secure Directory Permissions**:
   ```bash
   mkdir -p /var/lib/timeflux/wal
   chown timeflux:timeflux /var/lib/timeflux/wal
   chmod 700 /var/lib/timeflux/wal
   ```

2. **Encrypted Filesystem**: Place WAL on encrypted volume

3. **Secure Deletion**: Overwrite WAL segments on rotation (not implemented)

**Recommendation**: Store WAL on encrypted filesystem with restrictive permissions.

### Password Hash Storage

**Storage**: `_timeflux_users.password_hash` column in TimescaleDB

**Protection**:
- Bcrypt hashes (cost 12) are computationally expensive to crack
- Unique salts prevent rainbow tables
- Protected by PostgreSQL access controls

**Risk**: If database is compromised, hashes are exposed. Bcrypt slows brute-force but doesn't prevent it entirely.

**Mitigation**:
- Strong passwords required (enforce at user creation)
- Monitor for unauthorized database access
- Regular password rotation policy (manual, not enforced)

---

## Data in Transit

### Between Client and Timeflux

**Current State**: HTTP only (plaintext)

**Risks**:
- Credentials intercepted (Basic Auth is base64, not encrypted)
- Data payloads visible (line protocol, query results)
- Session hijacking (no session tokens, but credentials reused)

**Mitigation**: Deploy behind HTTPS reverse proxy (see [Network Security](#network-security))

### Between Timeflux and TimescaleDB

**Connection String** (from `config.yaml`):
```yaml
database:
  host: localhost
  port: 5432
  user: postgres
  password: secretpassword
  database: timeseries
  sslmode: disable  # ⚠️ WARNING: No encryption
```

**SSL/TLS Options**:
- `disable`: No encryption (default in example configs)
- `require`: TLS required, no certificate verification
- `verify-ca`: TLS required, verify server certificate against CA
- `verify-full`: TLS required, verify server certificate and hostname

**Recommendation for Production**:
```yaml
database:
  sslmode: verify-full
  sslrootcert: /path/to/ca-cert.pem
  sslcert: /path/to/client-cert.pem  # Optional: mutual TLS
  sslkey: /path/to/client-key.pem
```

**Local Deployments**: If Timeflux and PostgreSQL are on the same host (localhost), `sslmode: disable` is acceptable as traffic doesn't leave the machine.

**Network Deployments**: Always use `sslmode: verify-full` if TimescaleDB is on a different host.

---

## Logging & Monitoring

### Logging

**Implementation**: Go standard library `log` package

**Log Levels**:
- `INFO`: Normal operations (startup, config loading, user operations)
- `WARN`: Recoverable errors (WAL corruption, missing columns)
- `ERROR`: Request failures (auth failures, query errors)
- `DEBUG`: Verbose debugging (translated SQL, detailed metrics)

**Configuration** (`config.yaml`):
```yaml
logging:
  level: info  # Options: debug, info, warn, error
  format: json # Options: text, json
```

**Logged Information**:
- ✅ Authentication failures (username, source IP)
- ✅ Authorization failures (username, database, operation)
- ✅ Query execution (translated SQL, duration)
- ✅ Write operations (database, measurement, point count)
- ✅ Schema changes (DDL operations)
- ✅ WAL corruption events (checksum mismatches)
- ✅ User management operations (create, delete, grant, revoke)
- ❌ **Never logged**: Passwords (plaintext or hashed)

**Security Considerations**:

1. **Log Rotation**: Not implemented in Timeflux
   - **Recommendation**: Use external log management (systemd journal, syslog, logrotate)

2. **Log Retention**: No automatic cleanup
   - **Recommendation**: Configure retention policy (e.g., 90 days)

3. **Log Access**: Logs may contain sensitive information (query data, database names)
   - **Recommendation**: Restrict log file permissions (`chmod 600`)

4. **Log Injection**: User-controlled input in logs (database names, measurement names)
   - **Risk**: Log parsing tools may be vulnerable to crafted identifiers
   - **Mitigation**: Logs use structured format (JSON); identifiers are quoted

### Metrics

**Endpoint**: `GET /metrics`
**Format**: JSON (not Prometheus format currently)

**Exposed Metrics**:
```json
{
  "writes": {
    "requests": 12345,
    "points": 67890,
    "duration_avg_us": 1234,
    "duration_min_us": 500,
    "duration_max_us": 5000
  },
  "queries": {
    "requests": 5432,
    "duration_avg_us": 12000,
    "duration_min_us": 1000,
    "duration_max_us": 100000
  },
  "wal": {
    "writes": 12345,
    "replay_success": 12340,
    "replay_errors": 3,
    "corruption_count": 2,
    "duration_avg_us": 450
  },
  "schema": {
    "evolutions": 42,
    "tables_created": 10,
    "columns_added": 32
  }
}
```

**Security Monitoring**:
- Monitor `wal.corruption_count` (should be zero)
- Monitor `wal.replay_errors` (transient errors acceptable, persistent errors indicate issues)
- Monitor write latency `writes.duration_avg_us` (spikes may indicate DoS or resource exhaustion)
- Monitor query latency `queries.duration_avg_us` (expensive queries or DoS)

**Information Disclosure**: Metrics endpoint is unauthenticated
- **Risk**: Exposes database activity levels (write/query rates)
- **Mitigation**: Bind metrics endpoint to localhost or restrict via firewall

**Recommendation**: Add authentication to `/metrics` endpoint or expose only on internal network.

### Audit Logging

**Current State**: Basic authentication/authorization logging only

**Recommended Enhancements**:
1. **Structured Audit Log**: Separate audit events from operational logs
2. **Audit Events**:
   - User login attempts (success/failure, source IP, timestamp)
   - Permission changes (who granted/revoked what, when)
   - User creation/deletion (admin user, target user, timestamp)
   - Database access (user, database, measurement, operation type)
   - Schema modifications (DDL operations, initiating user)
3. **Audit Log Protection**: Write-once, immutable storage (append-only)
4. **Compliance**: GDPR, SOC2, HIPAA may require detailed audit trails

**Not Currently Implemented**: Would require significant logging system enhancements.

---

## Known Limitations & Blind Spots

### Authentication & Authorization

#### 1. No Password Complexity Requirements

**Issue**: Users can set weak passwords (e.g., `password`, `123456`)

**Risk**: Brute-force attacks succeed quickly

**Mitigation**:
- Enforce password policy at user creation (not implemented)
- Use external authentication (LDAP, OAuth) - not supported
- Rate limit authentication attempts (not implemented)

**Workaround**: Manual password policy enforcement by administrators

#### 2. No Account Lockout

**Issue**: Unlimited authentication attempts allowed

**Risk**: Brute-force attacks can run indefinitely

**Mitigation**:
- Implement rate limiting per username/IP (not implemented)
- Use reverse proxy rate limiting (external solution)
- Monitor authentication failure logs

**Detection**: Monitor logs for repeated `401 Unauthorized` from same IP/username

#### 3. No Session Management

**Issue**: Credentials sent with every request (stateless authentication)

**Risk**: Increased credential exposure window

**Benefits**: Simplicity, scalability (no session storage)

**Mitigation**: Use short-lived credentials or implement token-based auth (not implemented)

#### 4. Limited Measurement-Level Extraction

**Issue**: Measurement names not fully extracted from complex InfluxQL queries

**Example**:
```sql
-- Permission check may only see database "mydb", not measurement "sensitive"
SELECT * FROM mydb.sensitive WHERE time > now() - 1h
```

**Risk**: User with `mydb:r` can access `mydb.sensitive:w` (if write somehow triggered)

**Mitigation**:
- Use database-level permissions for sensitive data
- Separate sensitive measurements into dedicated databases
- Implement full AST-based measurement extraction (complex)

**Impact**: Medium (authorization bypass for measurement-specific permissions)

#### 5. Protected Table Query Bypass

**Issue**: `IsAuthTableQuery()` uses simple string matching

**Code**:
```go
func IsAuthTableQuery(query string) bool {
    lowerQuery := strings.ToLower(query)
    return strings.Contains(lowerQuery, "_timeflux_users") ||
           strings.Contains(lowerQuery, "_timeflux_user_permissions")
}
```

**Bypass Attempts**:
- Unicode variations: `_timeflux\u005fusers` (unlikely to work in SQL)
- SQL comments: `/**/` (would break InfluxQL parser first)
- Case obfuscation: (handled by `strings.ToLower()`)

**Additional Mitigation**: Configure PostgreSQL user permissions to deny access to auth tables at database level

**Recommendation**:
```sql
-- Grant minimal permissions to Timeflux database user
REVOKE ALL ON TABLE _timeflux_users FROM timeflux_user;
REVOKE ALL ON TABLE _timeflux_user_permissions FROM timeflux_user;
-- Only allow Timeflux to query these tables (internal use)
GRANT SELECT ON TABLE _timeflux_users TO timeflux_user;
GRANT SELECT ON TABLE _timeflux_user_permissions TO timeflux_user;
```

### SQL Injection

#### 1. Complex InfluxQL Translation Edge Cases

**Issue**: InfluxQL-to-SQL translation may have untested code paths

**Risk**: Crafted InfluxQL queries might produce unsafe SQL

**Mitigation**:
- InfluxQL parser rejects malformed queries (first line of defense)
- All identifiers sanitized via `pgx.Identifier{}.Sanitize()`
- All values parameterized
- PostgreSQL query user should have minimal privileges

**Testing Recommendation**: Conduct fuzzing tests on query translator

#### 2. Schema Introspection Queries

**Issue**: `SHOW DATABASES`, `SHOW MEASUREMENTS` query PostgreSQL system catalogs

**Queries Generated**:
```sql
-- SHOW DATABASES
SELECT schema_name FROM information_schema.schemata
WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'public', 'timescaledb_information');

-- SHOW MEASUREMENTS
SELECT tablename FROM pg_tables WHERE schemaname = 'mydb';
```

**Risk**: Information disclosure about database structure

**Mitigation**: Authorization checks still apply (user must have read permission)

**Blind Spot**: System schema exclusion list may be incomplete (custom PostgreSQL extensions)

### Data Integrity

#### 1. WAL Corruption Data Loss

**Issue**: Corrupted WAL entries are skipped (not recovered)

**Risk**: Silent data loss if corruption occurs

**Detection**: Monitor `wal.corruption_count` metric

**Mitigation**:
- Use high-reliability storage (ECC RAM, enterprise SSDs)
- Monitor metrics and alert on corruption
- Consider WAL segment backups (manual process)

**Recommendation**: Implement WAL segment backup and recovery mechanism

#### 2. No Write Acknowledgment Levels

**Issue**: WAL-enabled writes return immediately (eventual consistency)

**Risk**: Client believes write succeeded, but background processing fails

**Scenario**:
1. Client writes to `/write?db=mydb`
2. Timeflux returns `204 No Content` (success)
3. Background worker encounters error (e.g., schema issue, disk full)
4. Data is lost, client is unaware

**Detection**: Monitor `wal.replay_errors` metric

**Mitigation**:
- Monitor background worker errors
- Disable WAL for critical writes (synchronous mode)
- Implement write acknowledgment levels (not implemented)

**InfluxDB Compatibility Note**: InfluxDB v1 also has eventual consistency with writes

#### 3. No Data Validation on Replay

**Issue**: WAL entries written before schema changes may be incompatible

**Example**:
1. WAL entry written: `cpu value=85.3`
2. Schema changed: `value` column dropped
3. WAL replay fails: column doesn't exist

**Impact**: WAL replay errors, data loss for affected entries

**Mitigation**:
- Schema changes are rare in time-series workloads
- Additive schema changes only (add columns, don't drop)
- Monitor `wal.replay_errors` metric

**Recommendation**: Implement schema versioning in WAL entries

### Network Security

#### 1. No TLS Support

**Issue**: All traffic in plaintext (HTTP only)

**Risk**: Credential interception, data exposure, MITM attacks

**Mitigation**: Deploy behind HTTPS reverse proxy (see [Network Security](#network-security))

**Blind Spot**: Internal Timeflux-to-TimescaleDB connection also unencrypted by default

#### 2. No Rate Limiting

**Issue**: No built-in DoS protection

**Risk**: Resource exhaustion from malicious or misconfigured clients

**Mitigation**: Use reverse proxy rate limiting (Nginx, Caddy, cloud load balancer)

**Blind Spot**: Rate limiting by IP is ineffective against distributed attacks

#### 3. No Connection Limits Per User

**Issue**: A single user can exhaust connection pool

**Risk**: Denial of service for other users

**Mitigation**: Configure `max_connections` in `config.yaml` (applies globally)

**Recommendation**: Implement per-user connection limits

### Compliance & Privacy

#### 1. No Data Retention Policies

**Issue**: Data retained indefinitely (TimescaleDB default)

**Compliance Risk**: GDPR, CCPA may require data deletion after retention period

**Mitigation**: Implement manual retention policies
```sql
-- Example: Drop old data
SELECT drop_chunks('mydb.measurement', INTERVAL '90 days');
```

**Recommendation**: Implement automatic retention policy configuration

#### 2. No Data Masking or Redaction

**Issue**: Query results return all data (no field-level access control)

**Risk**: Sensitive fields exposed to users with read permission

**Example**: User with `mydb.users:r` can see `password_hash` column

**Mitigation**:
- Don't store sensitive data in Timeflux
- Use separate databases for sensitive measurements
- Implement field-level permissions (not supported)

#### 3. No Audit Logging for Compliance

**Issue**: Basic logging insufficient for SOC2, HIPAA, PCI-DSS compliance

**Gap**: No immutable audit trail, no log integrity verification

**Recommendation**: Integrate with external audit logging system (Splunk, ELK, cloud logging)

### Operational Security

#### 1. No Secrets Management Integration

**Issue**: Database passwords in plaintext YAML config

**Risk**: Config file compromise exposes database credentials

**Mitigation Options**:
- File permissions (`chmod 600 config.yaml`)
- Environment variable substitution (not implemented)
- Secrets management integration (Vault, AWS Secrets Manager) - not implemented

**Workaround**:
```bash
# Use environment variables in docker-compose
services:
  timeflux:
    environment:
      DB_PASSWORD: ${DB_PASSWORD}
```

#### 2. No Secure Communication Between Timeflux Instances

**Issue**: No multi-instance support or clustering

**Risk**: N/A (single instance only)

**Future Consideration**: If clustering implemented, need secure inter-node communication

#### 3. No Binary Signing or Checksum Verification

**Issue**: Timeflux binaries not signed, no published checksums

**Risk**: Supply chain attacks, tampered binaries

**Mitigation**: Build from source, verify Git commit signatures

**Recommendation**: Publish signed releases with checksums (SHA256)

---

## Threat Model Considerations

### Threat Actors

#### 1. Unauthenticated External Attacker

**Goals**: Data theft, service disruption, privilege escalation

**Attack Vectors**:
- SQL injection via query endpoint
- DoS via expensive queries or large writes
- Authentication bypass
- Exploit unpatched vulnerabilities

**Mitigations**:
- ✅ Authentication required for all endpoints
- ✅ SQL injection prevention (parameterized queries, identifier sanitization)
- ⚠️ DoS protection (external rate limiting recommended)
- ⚠️ Regular dependency updates (manual process)

**Residual Risk**: Medium (DoS attacks possible, dependency vulnerabilities)

#### 2. Authenticated Low-Privilege User

**Goals**: Privilege escalation, unauthorized data access

**Attack Vectors**:
- Authorization bypass via measurement extraction gaps
- Access auth tables via query endpoint
- Brute-force other users' passwords (no rate limiting)

**Mitigations**:
- ✅ Granular permission system
- ✅ Auth table query blocking
- ⚠️ Measurement-level extraction limited
- ❌ No rate limiting on authentication

**Residual Risk**: Low-Medium (measurement-level bypass possible in edge cases)

#### 3. Authenticated High-Privilege User (Malicious Insider)

**Goals**: Data exfiltration, data destruction, cover tracks

**Attack Vectors**:
- Bulk export all accessible data
- Expensive queries to degrade service
- Delete data if write permissions exist

**Mitigations**:
- ✅ Permission system limits scope of access
- ⚠️ Audit logging (basic, no integrity protection)
- ❌ No query result size limits
- ❌ No data export rate limiting

**Residual Risk**: High (trusted users have broad access within permissions)

#### 4. Network Attacker (Man-in-the-Middle)

**Goals**: Credential theft, data interception, session hijacking

**Attack Vectors**:
- Intercept HTTP traffic (plaintext)
- Steal credentials from Basic Auth headers
- Modify queries or responses in transit

**Mitigations**:
- ❌ No TLS in Timeflux (external reverse proxy required)
- ✅ Stateless auth reduces session hijacking impact
- ⚠️ Timeflux-to-PostgreSQL connection may be unencrypted

**Residual Risk**: High (without HTTPS reverse proxy), Low (with HTTPS)

#### 5. Infrastructure Attacker (Compromised Server)

**Goals**: Steal database credentials, extract all data, establish persistence

**Attack Vectors**:
- Read `config.yaml` for database credentials
- Read WAL files for recent data
- Query TimescaleDB directly
- Modify Timeflux binary or config

**Mitigations**:
- ⚠️ File permissions (manual configuration)
- ⚠️ PostgreSQL connection credentials in plaintext config
- ❌ No WAL encryption
- ❌ No binary integrity verification

**Residual Risk**: Critical (full compromise likely if server breached)

**Defense in Depth**: Limit blast radius with network segmentation, PostgreSQL user permissions

#### 6. Database Attacker (Compromised TimescaleDB)

**Goals**: Extract password hashes, modify data, escalate to Timeflux host

**Attack Vectors**:
- Extract bcrypt hashes from `_timeflux_users`
- Modify permission table to grant unauthorized access
- Read all time-series data

**Mitigations**:
- ✅ Bcrypt hashes (cost 12) slow brute-force
- ✅ Unique salts per password
- ⚠️ Timeflux trusts database content (no integrity checks on permissions)
- ❌ No database-level access control (Timeflux user has broad permissions)

**Residual Risk**: Critical (database compromise is full data compromise)

**Recommendation**: Configure PostgreSQL with role-based access control, audit logging enabled

---

## Security Best Practices

### Deployment

1. **Always Use HTTPS**
   ```nginx
   server {
       listen 443 ssl http2;
       ssl_certificate /path/to/cert.pem;
       ssl_certificate_key /path/to/key.pem;
       location / {
           proxy_pass http://127.0.0.1:8086;
       }
   }
   ```

2. **Bind Timeflux to Localhost**
   ```yaml
   server:
     host: 127.0.0.1
     port: 8086
   ```

3. **Use PostgreSQL SSL**
   ```yaml
   database:
     sslmode: verify-full
     sslrootcert: /path/to/ca.pem
   ```

4. **Restrict File Permissions**
   ```bash
   chmod 600 config.yaml
   chmod 700 /var/lib/timeflux/wal
   chown timeflux:timeflux -R /var/lib/timeflux
   ```

5. **Enable Rate Limiting** (Nginx example)
   ```nginx
   limit_req_zone $binary_remote_addr zone=timeflux:10m rate=10r/s;
   ```

### User Management

1. **Enforce Strong Passwords** (manual verification)
   - Minimum 12 characters
   - Require mix of uppercase, lowercase, numbers, symbols

2. **Principle of Least Privilege**
   - Grant minimum required permissions
   - Use measurement-specific permissions where possible
   - Avoid wildcard `*:rw` unless necessary

3. **Regular Permission Audits**
   ```bash
   bin/timeflux user:list
   bin/timeflux user:show <username>
   ```

4. **Dedicated Service Accounts**
   - Separate users for different applications
   - Easier to track usage and revoke access

### Database Configuration

1. **Minimal PostgreSQL User Permissions**
   ```sql
   CREATE USER timeflux WITH PASSWORD 'strongpassword';
   GRANT CONNECT ON DATABASE timeseries TO timeflux;
   GRANT USAGE, CREATE ON SCHEMA mydb TO timeflux;
   GRANT SELECT, INSERT ON ALL TABLES IN SCHEMA mydb TO timeflux;
   -- Revoke access to auth tables
   REVOKE ALL ON TABLE _timeflux_users FROM PUBLIC;
   REVOKE ALL ON TABLE _timeflux_user_permissions FROM PUBLIC;
   ```

2. **Enable PostgreSQL Audit Logging**
   ```sql
   -- postgresql.conf
   log_connections = on
   log_disconnections = on
   log_statement = 'all'  # Or 'ddl' for schema changes only
   ```

3. **Configure Resource Limits**
   ```sql
   ALTER ROLE timeflux SET statement_timeout = '30s';
   ALTER ROLE timeflux SET idle_in_transaction_session_timeout = '60s';
   ```

### Monitoring

1. **Alert on Critical Metrics**
   ```bash
   # Prometheus/Grafana alerting (convert metrics to Prometheus format)
   - alert: WALCorruption
     expr: timeflux_wal_corruption_count > 0
     for: 1m
     annotations:
       summary: "WAL corruption detected"
   ```

2. **Log Monitoring**
   - Alert on authentication failures (potential brute-force)
   - Alert on authorization failures (potential privilege escalation attempt)
   - Alert on WAL replay errors (data loss risk)

3. **Database Monitoring**
   ```sql
   -- Check for unusual permission changes
   SELECT * FROM _timeflux_user_permissions
   ORDER BY username DESC LIMIT 100;
   ```

### Backup & Recovery

1. **Regular TimescaleDB Backups**
   ```bash
   pg_dump -U postgres -d timeseries -f backup.sql
   ```

2. **Include Auth Tables in Backups**
   ```bash
   pg_dump -U postgres -d timeseries -t _timeflux_users -t _timeflux_user_permissions -f auth_backup.sql
   ```

3. **WAL Directory Backups** (optional, for crash recovery)
   ```bash
   tar -czf wal_backup.tar.gz /var/lib/timeflux/wal/
   ```

4. **Test Recovery Procedures**
   - Simulate database loss
   - Restore from backup
   - Verify data integrity and authentication

### Incident Response Preparation

1. **Document Procedures**
   - Credential rotation process
   - User account lockout
   - Database restoration
   - Forensic data collection

2. **Maintain Contact List**
   - Database administrator
   - Infrastructure team
   - Security team

3. **Preserve Logs**
   - Centralized log collection (Splunk, ELK)
   - Immutable log storage (S3, WORM media)
   - Retention period: 90+ days

---

## Incident Response

### Suspected Credential Compromise

**Immediate Actions**:
1. Reset affected user password
   ```bash
   bin/timeflux user:reset-password <username> <new-strong-password>
   ```

2. Review user's recent activity in logs
   ```bash
   grep "username:<username>" /var/log/timeflux.log
   ```

3. Check for unauthorized data access
   ```sql
   -- Review PostgreSQL logs for suspicious queries
   SELECT * FROM pg_stat_activity WHERE usename = 'timeflux';
   ```

4. Revoke permissions if necessary
   ```bash
   bin/timeflux user:revoke <username> <database>
   ```

**Follow-Up**:
- Determine compromise vector (phishing, MITM, brute-force)
- Reset passwords for other users if shared credential storage compromised
- Implement additional controls (MFA, IP allowlisting)

### Suspected Database Breach

**Immediate Actions**:
1. **Isolate Database**: Block network access via firewall
   ```bash
   iptables -A INPUT -p tcp --dport 5432 -j DROP
   ```

2. **Rotate Database Credentials**
   ```sql
   ALTER USER timeflux WITH PASSWORD 'new-strong-password';
   ```
   Update `config.yaml` and restart Timeflux

3. **Review Database Logs**: Check for unauthorized queries
   ```sql
   SELECT * FROM pg_stat_activity;
   SELECT * FROM pg_stat_statements ORDER BY calls DESC;
   ```

4. **Check for Backdoors**
   ```sql
   -- Look for unexpected users
   SELECT * FROM pg_user;
   -- Look for unexpected permissions
   SELECT * FROM _timeflux_user_permissions ORDER BY username;
   ```

**Follow-Up**:
- Forensic analysis of database logs
- Restore from clean backup if data modified
- Implement additional database access controls

### Suspected DoS Attack

**Immediate Actions**:
1. **Identify Attack Vector**: Check metrics and logs
   ```bash
   curl http://localhost:8086/metrics | jq
   ```

2. **Block Malicious IPs** (if identifiable)
   ```bash
   iptables -A INPUT -s <attacker-ip> -j DROP
   ```

3. **Enable Rate Limiting** (if not already enabled)
   - Configure reverse proxy rate limiting
   - Restart services

4. **Scale Resources** (if legitimate traffic spike)
   - Increase PostgreSQL connection pool
   - Add more Timeflux instances (with load balancer)

**Follow-Up**:
- Implement permanent rate limiting
- Add monitoring alerts for traffic spikes
- Consider DDoS mitigation service (Cloudflare, AWS Shield)

### Data Loss or Corruption

**Immediate Actions**:
1. **Check WAL Corruption Metrics**
   ```bash
   curl http://localhost:8086/metrics | jq '.wal.corruption_count'
   ```

2. **Review WAL Replay Errors**
   ```bash
   grep "WAL replay error" /var/log/timeflux.log
   ```

3. **Verify PostgreSQL Data Integrity**
   ```sql
   -- Check for corrupted indexes
   REINDEX DATABASE timeseries;
   ```

4. **Restore from Backup** (if corruption widespread)
   ```bash
   psql -U postgres -d timeseries -f backup.sql
   ```

**Follow-Up**:
- Investigate root cause (hardware failure, software bug)
- Implement additional integrity checks
- Increase WAL backup frequency

---

## Security Checklist for Deployments

### Pre-Production

- [ ] Generate strong database password (16+ characters, random)
- [ ] Configure PostgreSQL SSL (`sslmode: verify-full`)
- [ ] Enable PostgreSQL audit logging
- [ ] Configure minimal PostgreSQL user permissions
- [ ] Create admin user with strong password
- [ ] Configure firewall rules (restrict to known IPs)
- [ ] Set up HTTPS reverse proxy (Nginx, Caddy, cloud load balancer)
- [ ] Configure rate limiting (reverse proxy or cloud WAF)
- [ ] Set WAL directory permissions (`chmod 700`)
- [ ] Configure log rotation and retention
- [ ] Test backup and restore procedures
- [ ] Document incident response procedures
- [ ] Set up monitoring and alerting (metrics, logs)

### Production Launch

- [ ] Verify HTTPS certificate validity
- [ ] Verify TLS configuration (A+ on SSL Labs)
- [ ] Test authentication (Basic Auth, Token, query params)
- [ ] Test authorization (verify users can't access unauthorized databases)
- [ ] Verify auth tables are inaccessible via query endpoint
- [ ] Load test to establish baseline performance
- [ ] Verify monitoring alerts fire correctly
- [ ] Test fail-over procedures (if HA configured)

### Ongoing Maintenance

- [ ] Review access logs weekly for suspicious activity
- [ ] Review user permissions monthly (remove unused accounts)
- [ ] Rotate database credentials quarterly
- [ ] Update dependencies monthly (Go modules, Docker images)
- [ ] Test backup restoration monthly
- [ ] Review security advisories (GitHub, CVE databases)
- [ ] Audit logging configuration (ensure logs not tampered)

---

## Responsible Disclosure

If you discover a security vulnerability in Timeflux, please report it responsibly:

**Contact**: (Add appropriate contact information)

**Please Include**:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if available)

**Response Timeline**:
- Acknowledgment: Within 48 hours
- Initial assessment: Within 7 days
- Fix timeline: Depends on severity (critical: <7 days, high: <30 days)

**Coordinated Disclosure**: We request 90 days before public disclosure to allow users to patch.

---

## Conclusion

Timeflux implements multiple security layers (authentication, authorization, SQL injection prevention, data integrity checks), but has known limitations (no TLS, no rate limiting, limited audit logging). For production deployments:

**Minimum Security Requirements**:
1. Deploy behind HTTPS reverse proxy
2. Use PostgreSQL SSL connections
3. Configure rate limiting (reverse proxy or cloud WAF)
4. Restrict network access (firewall, VPC)
5. Use strong passwords (12+ characters)
6. Monitor metrics and logs for anomalies

**Risk Profile**:
- **Low-Risk Deployments**: Internal networks, trusted users, non-sensitive data → Basic configuration acceptable
- **Medium-Risk Deployments**: Internet-exposed, authenticated users, business-critical data → Implement all minimum requirements
- **High-Risk Deployments**: Public internet, sensitive data, compliance requirements → Minimum requirements + additional hardening (audit logging, secrets management, network segmentation)

**Key Takeaway**: Timeflux is a facade layer, not a complete security boundary. Underlying PostgreSQL security, network security, and operational practices are equally critical to overall security posture.

---

**Document Version**: 1.0
**Last Review Date**: 2026-03-03
**Next Review Date**: 2026-06-03 (or upon significant code changes)
