# Timeflux Authentication System

## Overview

Timeflux includes a built-in authentication and authorization system that provides user management and granular permissions for database and measurement access. The system supports InfluxDB-compatible authentication methods while storing credentials securely in PostgreSQL.

## Table of Contents

- [How It Works](#how-it-works)
- [Enabling Authentication](#enabling-authentication)
- [User Management CLI](#user-management-cli)
- [API Authentication](#api-authentication)
- [Permission Model](#permission-model)
- [Security Features](#security-features)

## How It Works

### Architecture

The authentication system consists of several components:

1. **User Storage** (`_timeflux_users` table)
   - Stores usernames and bcrypt-hashed passwords
   - Uses bcrypt with cost factor 12 for secure password hashing
   - Tracks creation and update timestamps

2. **Permission Storage** (`_timeflux_user_permissions` table)
   - Stores granular access control for databases and measurements
   - Supports read and write permissions separately
   - Supports wildcard (`*`) for all databases
   - Allows measurement-specific or database-wide permissions

3. **Credential Parsing** (`auth/auth.go`)
   - Extracts credentials from HTTP requests
   - Supports multiple authentication methods (URL params, Basic Auth, Token header)
   - **Note:** Bearer token authentication is not currently supported

4. **Authentication Middleware** (`auth/middleware.go`)
   - Validates user credentials on every request
   - Enforces permission checks for read/write operations
   - Prevents access to internal authentication tables

5. **User Management** (`auth/user_manager.go`, `cmd/user.go`)
   - CLI commands for managing users and permissions
   - Automatic password generation when not specified
   - Comprehensive logging of all user modifications

### Database Schema

**Users Table:**
```sql
CREATE TABLE _timeflux_users (
    username TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
```

**Permissions Table:**
```sql
CREATE TABLE _timeflux_user_permissions (
    username TEXT NOT NULL,
    database TEXT NOT NULL,           -- Can be '*' for wildcard (all databases)
    measurement TEXT NOT NULL DEFAULT '',  -- Empty string means all measurements
    can_read BOOLEAN NOT NULL DEFAULT false,
    can_write BOOLEAN NOT NULL DEFAULT false,
    PRIMARY KEY (username, database, measurement),
    FOREIGN KEY (username) REFERENCES _timeflux_users(username) ON DELETE CASCADE
);
```

**Wildcard Support:**
- `database = '*'` grants access to all databases (current and future)
- `measurement = ''` grants access to all measurements in the database
- Combined: `database='*', measurement=''` grants access to all databases and measurements

### Request Flow

1. HTTP request arrives at Timeflux
2. Authentication middleware (`auth.Middleware`) validates credentials
3. Username is stored in request context
4. Authorization middleware (`auth.RequirePermission`) checks database permissions
5. Query handler prevents access to `_timeflux_*` tables
6. Request proceeds to query/write handler if authorized

## Enabling Authentication

### Configuration

Add the following to your `config.yaml`:

```yaml
auth:
  enabled: true  # Set to false to disable authentication
```

When authentication is disabled, all requests are allowed (useful for development).

### Complete Example Configuration

```yaml
server:
  port: 8086

database:
  host: localhost
  port: 5432
  database: timeseries
  user: postgres
  password: password
  pool_size: 32
  auto_create_databases: true

auth:
  enabled: true

logging:
  level: info
  format: json

wal:
  enabled: true
  path: /tmp/timeflux/wal
  num_workers: 8
```

### First Time Setup

1. Enable authentication in config.yaml
2. Start Timeflux (auth tables are created automatically)
3. Stop Timeflux
4. Create your first admin user via CLI
5. Grant permissions to the user
6. Restart Timeflux

## User Management CLI

All user management is performed via command-line interface while Timeflux is **not running** (to avoid port conflicts).

### Create a User

**With auto-generated password:**
```bash
./timeflux user:add alice
# Output:
# User 'alice' created with generated password: aB3dE5fG7hI9jK1mN3oP
# Please save this password securely - it will not be displayed again.
```

**With specified password:**
```bash
./timeflux user:add bob mySecurePassword123
# Output:
# User 'bob' created successfully.
```

### Delete a User

```bash
./timeflux user:delete alice
# Output:
# User 'alice' deleted successfully.
```

All permissions for the user are automatically deleted (CASCADE).

### Reset Password

**With auto-generated password:**
```bash
./timeflux user:reset-password bob
# Output:
# Password reset for user 'bob': xY9zW8vU7tS6rQ5pO4nM
# Please save this password securely - it will not be displayed again.
```

**With specified password:**
```bash
./timeflux user:reset-password bob newPassword456
# Output:
# Password reset for user 'bob' successfully.
```

### Grant Permissions

**Grant read access to entire database:**
```bash
./timeflux user:grant alice mydb:r
# or
./timeflux user:grant alice mydb.*:r
# Output:
# Permission granted: alice -> mydb.* (read)
```

**Grant write access to specific measurement:**
```bash
./timeflux user:grant alice mydb.cpu:w
# Output:
# Permission granted: alice -> mydb.cpu (write)
```

**Grant read and write access:**
```bash
./timeflux user:grant alice mydb.memory:rw
# Output:
# Permission granted: alice -> mydb.memory (read/write)
```

**Grant access to ALL databases (wildcard):**
```bash
# Write to all databases (useful with auto_create_databases)
./timeflux user:grant collector *:w
# Output:
# Permission granted: collector -> *.* (write)

# Read/write all databases and measurements (super admin)
./timeflux user:grant admin *:rw
# Output:
# Permission granted: admin -> *.* (read/write)
```

**Grant access to specific measurement across ALL databases:**
```bash
# Read CPU measurement from any database
./timeflux user:grant monitor *.cpu:r
# Output:
# Permission granted: monitor -> *.cpu (read)

# Write to memory measurement in any database
./timeflux user:grant collector *.memory:w
# Output:
# Permission granted: collector -> *.memory (write)
```

**Permission format:**
- `database:r` - Read-only access to all measurements in specific database
- `database:w` - Write-only access to all measurements in specific database
- `database:rw` - Read/write access to all measurements in specific database
- `database.measurement:r` - Read-only access to specific measurement in specific database
- `database.measurement:w` - Write-only access to specific measurement in specific database
- `database.measurement:rw` - Read/write access to specific measurement in specific database
- `*:r` - Read-only access to all databases and all measurements
- `*:w` - Write-only access to all databases and all measurements
- `*:rw` - Read/write access to all databases and all measurements (super admin)
- `*.measurement:r` - Read-only access to specific measurement across all databases
- `*.measurement:w` - Write-only access to specific measurement across all databases
- `*.measurement:rw` - Read/write access to specific measurement across all databases

### Revoke Permissions

```bash
./timeflux user:revoke alice mydb.cpu
# Output:
# Permission revoked: alice -> mydb.cpu
```

### List Users

```bash
./timeflux user:list
# Output:
# Users:
#   - alice
#   - bob
```

### Show User Details

```bash
./timeflux user:show alice
# Output:
# User: alice
# Permissions:
#   Database    Measurement    Read    Write
#   --------    -----------    ----    -----
#   mydb        *              true    false
#   mydb        cpu            false   true
#   mydb        memory         true    true
```

## API Authentication

Timeflux supports multiple InfluxDB-compatible authentication methods. **Bearer tokens are not supported.**

### Method 1: URL Query Parameters

```bash
curl -G 'http://localhost:8086/query?db=mydb&u=alice&p=password123' \
  --data-urlencode 'q=SELECT * FROM cpu'
```

### Method 2: HTTP Basic Authentication

```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --user alice:password123 \
  --data-urlencode 'q=SELECT * FROM cpu'
```

### Method 3: Token Header

```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  -H 'Authorization: Token alice:password123' \
  --data-urlencode 'q=SELECT * FROM cpu'
```

### Write Example

```bash
curl -XPOST 'http://localhost:8086/write?db=mydb&u=alice&p=password123' \
  --data-binary 'cpu,host=server1 value=85.3'
```

### Authentication Errors

**401 Unauthorized:**
```json
{
  "error": "authentication required"
}
```

**403 Forbidden:**
```json
{
  "error": "insufficient permissions"
}
```

## Permission Model

### Permission Resolution

Permissions are checked in order of specificity (most specific wins):

1. **Specific database, specific measurement** (`database.measurement`)
   - Example: `mydb.cpu` - Highest priority

2. **Specific database, all measurements** (`database.*` or `database`)
   - Example: `mydb.*` - Applies to all measurements in `mydb`

3. **All databases, specific measurement** (`*.measurement`)
   - Example: `*.cpu` - CPU measurement across any database

4. **All databases, all measurements** (`*.*` or `*`)
   - Example: `*.*` - Super admin, lowest priority (catch-all)

**Example resolution for query on `newdb.cpu`:**
```
1. Check: newdb.cpu (specific) → Not found
2. Check: newdb.* (database wildcard) → Not found
3. Check: *.cpu (measurement wildcard) → Found! ✓ Use this permission
4. Check: *.* (global wildcard) → (Not reached, already found match)
```

**Wildcard with Auto-Create Databases:**

When `auto_create_databases: true` is enabled:
- User with `*:w` permission can write to any database name
- Database is automatically created on first write
- No need to pre-grant permissions for each new database
- Perfect for dynamic multi-tenant scenarios

### Read vs Write Permissions

- **Read permission** (`can_read=true`)
  - Required for: `SELECT`, `SHOW SERIES`, query operations
  - Endpoint: `/query` (GET and POST)

- **Write permission** (`can_write=true`)
  - Required for: Line protocol writes
  - Endpoint: `/write` (POST)

- **Both permissions** (`can_read=true, can_write=true`)
  - Full access to database/measurement
  - Can query and write data

### System Operations

The following operations don't require database-specific permissions:
- `SHOW DATABASES` - Lists databases (no db parameter needed)
- `CREATE DATABASE` - Creates new database
- `DROP DATABASE` - Deletes database

**Note:** Currently, all authenticated users can perform system operations. Future versions may add admin roles.

### Protection Against Auth Table Access

The system **automatically blocks** any queries attempting to access authentication tables:
- `_timeflux_users`
- `_timeflux_user_permissions`

Attempts to query these tables via the HTTP API will return:
```json
{
  "results": [{
    "statement_id": 0,
    "error": "Access to authentication tables is forbidden"
  }]
}
```

This protection applies even to queries translated from InfluxQL.

## Security Features

### Password Security

1. **Bcrypt Hashing**
   - All passwords hashed using bcrypt with cost factor 12
   - Industry-standard password hashing algorithm
   - Resistant to brute-force attacks

2. **Automatic Password Generation**
   - 20-character random passwords using cryptographic RNG
   - Base64-URL encoding for safe characters
   - Displayed only once during creation/reset

3. **No Password Storage**
   - Only password hashes stored in database
   - Original passwords never logged or stored

### Access Control

1. **Request-Level Authentication**
   - Every request validated before processing
   - Credentials checked against database on each request
   - No session persistence (stateless authentication)

2. **Permission Enforcement**
   - Read/write permissions checked separately
   - Database parameter required for permission checks
   - Granular control at measurement level

3. **Auth Table Protection**
   - HTTP API cannot access `_timeflux_*` tables
   - String matching prevents indirect access
   - Logged security events for audit

### Audit Logging

All user management operations are logged:

```
User created: alice
Permission granted: alice -> mydb.* (read/write)
Password reset for user: bob
Permission revoked: alice -> mydb.cpu
User deleted: bob
```

Additional security events logged:
```
Failed authentication attempt for user: alice
Permission denied: user bob attempted write on database mydb
Attempt to query auth tables blocked: SELECT * FROM _timeflux_users
```

## Best Practices

### User Management

1. **Use Generated Passwords**
   - More secure than user-chosen passwords
   - Omit password parameter to auto-generate

2. **Principle of Least Privilege**
   - Grant minimum permissions required
   - Use measurement-specific permissions when possible
   - Separate read-only and write-only users

3. **Regular Password Rotation**
   - Reset passwords periodically
   - Use `user:reset-password` command

### API Usage

1. **Prefer Basic Auth**
   - More secure than URL parameters
   - Credentials not logged in web server access logs

2. **Use HTTPS in Production**
   - Timeflux doesn't provide TLS
   - Use reverse proxy (nginx, Apache) with TLS termination

3. **Avoid Hardcoded Credentials**
   - Use environment variables or secrets management
   - Don't commit credentials to version control

### Operations

1. **Backup Auth Tables**
   - Include `_timeflux_users` and `_timeflux_user_permissions` in backups
   - Export users before major upgrades

2. **Monitor Logs**
   - Watch for failed authentication attempts
   - Alert on permission denied events
   - Track user modifications

3. **Test Permissions**
   - Verify user access before granting production permissions
   - Use `user:show` to review current permissions

## Troubleshooting

### "authentication required" Error

**Cause:** No credentials provided or invalid format

**Solution:**
- Verify credentials are included in request
- Check authentication method (Basic Auth, URL params, Token header)
- Ensure username and password are correct

### "invalid credentials" Error

**Cause:** Username or password incorrect

**Solution:**
- Verify username exists: `./timeflux user:list`
- Reset password: `./timeflux user:reset-password <username>`
- Check for typos in credentials

### "insufficient permissions" Error

**Cause:** User authenticated but lacks required permission

**Solution:**
- Check user permissions: `./timeflux user:show <username>`
- Grant appropriate permission:
  - Specific database: `./timeflux user:grant <username> <database>:<r|w|rw>`
  - All databases: `./timeflux user:grant <username> *:<r|w|rw>`
  - Specific measurement across databases: `./timeflux user:grant <username> *.measurement:<r|w|rw>`
- Verify correct database name in request
- If using `auto_create_databases`, grant wildcard permission: `user:grant <username> *:w`

### "database parameter required" Error

**Cause:** No `db` parameter in request

**Solution:**
- Add `?db=<database>` to request URL
- Required for all query and write operations (except system queries)

### Cannot Connect to Database (CLI)

**Cause:** Database connection details in config.yaml incorrect

**Solution:**
- Verify `database.host`, `database.port`, `database.database` in config.yaml
- Ensure TimescaleDB is running
- Test connection: `psql -h <host> -p <port> -U <user> -d <database>`

## Migration from Unauthenticated Setup

If you're adding authentication to an existing Timeflux deployment:

1. **Backup your data** (PostgreSQL database)

2. **Update configuration:**
   ```yaml
   auth:
     enabled: true
   ```

3. **Start Timeflux once** to create auth tables:
   ```bash
   ./timeflux
   # Wait for "Authentication enabled" log message
   # Press Ctrl+C to stop
   ```

4. **Create users and grant permissions:**
   ```bash
   ./timeflux user:add admin
   ./timeflux user:grant admin mydb:rw
   ```

5. **Update clients** to include credentials

6. **Restart Timeflux** and test

7. **Monitor logs** for authentication issues

## Examples

### Complete User Setup Workflow

```bash
# 1. Create a read-only user for dashboards
./timeflux user:add dashboard_reader
# Save generated password: aB3dE5fG7hI9jK1mN3oP

# 2. Grant read access to metrics database
./timeflux user:grant dashboard_reader metrics:r

# 3. Create a write-only user for collectors
./timeflux user:add collector_writer s3cr3tP@ssw0rd

# 4. Grant write access to specific measurements
./timeflux user:grant collector_writer metrics.cpu:w
./timeflux user:grant collector_writer metrics.memory:w
./timeflux user:grant collector_writer metrics.disk:w

# 5. Create an admin user with full access to ALL databases
./timeflux user:add admin
# Save generated password: xY9zW8vU7tS6rQ5pO4nM

# 6. Grant wildcard access (all current and future databases)
./timeflux user:grant admin *:rw

# 7. Create a multi-tenant collector (writes to any database)
./timeflux user:add tenant_collector
./timeflux user:grant tenant_collector *:w

# 8. Create a monitoring user (reads CPU across all databases)
./timeflux user:add monitor
./timeflux user:grant monitor *.cpu:r
./timeflux user:grant monitor *.memory:r

# 9. Review all users
./timeflux user:list

# 10. Review specific user permissions
./timeflux user:show admin
./timeflux user:show tenant_collector
./timeflux user:show monitor
```

### Wildcard Permission Use Cases

**Use Case 1: Multi-Tenant SaaS with Auto-Create**
```bash
# Scenario: Each customer gets their own database (customer_123, customer_456, etc.)
# Solution: Use wildcard write permission with auto_create_databases

# Config: auto_create_databases: true

# Create collector with wildcard write
./timeflux user:add saas_collector MySecretPass123
./timeflux user:grant saas_collector *:w

# Now collector can write to ANY database:
curl -XPOST 'http://localhost:8086/write?db=customer_123' \
  --user saas_collector:MySecretPass123 \
  --data-binary 'metrics,host=web1 requests=100'

# Database 'customer_123' is auto-created on first write!
```

**Use Case 2: Cross-Database Monitoring**
```bash
# Scenario: Monitor CPU/memory across production, staging, and dev databases
# Solution: Use measurement wildcard

./timeflux user:add ops_monitor
./timeflux user:grant ops_monitor *.cpu:r
./timeflux user:grant ops_monitor *.memory:r
./timeflux user:grant ops_monitor *.disk:r

# Can now query CPU from any database:
curl -G 'http://localhost:8086/query?db=production' \
  --user ops_monitor:password \
  --data-urlencode 'q=SELECT mean(usage) FROM cpu'

curl -G 'http://localhost:8086/query?db=staging' \
  --user ops_monitor:password \
  --data-urlencode 'q=SELECT mean(usage) FROM cpu'
```

**Use Case 3: Super Admin**
```bash
# Scenario: Need full access to everything
# Solution: Global wildcard

./timeflux user:add superadmin
./timeflux user:grant superadmin *:rw

# Can read/write any database, any measurement
```

### Using Credentials with InfluxDB Client Libraries

**Python (influxdb-client):**
```python
from influxdb_client import InfluxDBClient

client = InfluxDBClient(
    url="http://localhost:8086",
    token="alice:password123",  # username:password format
    org="-",  # Not used by Timeflux
)

# Query
query = 'SELECT * FROM cpu'
result = client.query_api().query(query, database="mydb")

# Write (requires write permission)
from influxdb_client.client.write_api import SYNCHRONOUS
write_api = client.write_api(write_options=SYNCHRONOUS)
write_api.write("mydb", record="cpu,host=server1 value=85.3")
```

**Go (influxdb1-client):**
```go
package main

import (
    "github.com/influxdata/influxdb1-client/v2"
)

func main() {
    c, _ := client.NewHTTPClient(client.HTTPConfig{
        Addr:     "http://localhost:8086",
        Username: "alice",
        Password: "password123",
    })
    defer c.Close()

    // Query
    q := client.NewQuery("SELECT * FROM cpu", "mydb", "")
    response, _ := c.Query(q)

    // Write
    bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
        Database: "mydb",
    })
    pt, _ := client.NewPoint("cpu",
        map[string]string{"host": "server1"},
        map[string]interface{}{"value": 85.3})
    bp.AddPoint(pt)
    c.Write(bp)
}
```

**cURL with Basic Auth:**
```bash
# Query
curl -G 'http://localhost:8086/query?db=mydb' \
  --user alice:password123 \
  --data-urlencode 'q=SELECT * FROM cpu WHERE time > now() - 1h'

# Write
curl -XPOST 'http://localhost:8086/write?db=mydb' \
  --user alice:password123 \
  --data-binary 'cpu,host=server1,region=us-west value=85.3,load=0.64'
```

## Future Enhancements

Potential improvements for future versions:

- [ ] Measurement-level permission enforcement for writes
- [ ] API keys (tokens) as alternative to passwords

## See Also

- [Main README](README.md) - General Timeflux documentation
- [CLAUDE.md](CLAUDE.md) - Development guide and architecture
- [config.yaml](config.yaml) - Configuration reference
