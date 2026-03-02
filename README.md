# Timeflux - InfluxDB v1 to TimescaleDB Facade

Timeflux is a Go-based HTTP service that implements the InfluxDB v1 API, translating requests to TimescaleDB on the backend. This allows existing systems using InfluxDB clients to seamlessly switch to TimescaleDB without code changes.

## Features

- **InfluxDB v1 API Compatible**: Supports write and query endpoints
- **Line Protocol Support**: Parse and write InfluxDB line protocol data
- **InfluxQL Query Support**: Translate InfluxQL queries to PostgreSQL SQL
- **Write-Ahead Log (WAL)**: 10x faster writes with crash recovery and CRC32 checksums
- **Dynamic Schema Evolution**: Automatically creates tables and columns as new measurements, tags, and fields appear
- **Background Index Creation**: Tag indexes created asynchronously to avoid blocking writes
- **Concurrent Write Safety**: Handles multiple concurrent writes with proper locking
- **Authentication & Authorization**: User management with granular database and measurement permissions
- **TimescaleDB Native**: Data stored in native PostgreSQL columns for easy migration
- **Production-Ready**: Comprehensive metrics, error handling, and graceful shutdown

## Architecture

### Data Model

| InfluxDB Concept | TimescaleDB Implementation |
|---|---|
| Database | PostgreSQL schema |
| Measurement | Hypertable (one per measurement) |
| Tags (indexed metadata) | TEXT columns with indexes |
| Fields (measured values) | Typed columns (DOUBLE PRECISION, BIGINT, TEXT, BOOLEAN) |
| Timestamp | TIMESTAMPTZ column |

### Schema Evolution

When a write contains new tags or fields not seen before, the service automatically:
1. Alters the table to add the new column
2. Creates an index for new tag columns
3. Records the change in a metadata table
4. Updates its in-memory schema cache

## Setup

### Prerequisites

- Go 1.21 or later
- PostgreSQL 12+ with TimescaleDB extension
- Docker (optional, for running TimescaleDB)

### Quick Start with Docker Compose

The easiest way to get started is using Docker Compose, which will run both TimescaleDB and Timeflux:

```bash
# Clone the repository
git clone https://github.com/penguinpowernz/timeflux.git
cd timeflux

# Start both services
make up

# Check logs
make logs

# Stop services
make down
```

The `config.yaml` file is already configured to connect to the TimescaleDB container. Timeflux will be available at `http://localhost:8086`.

### Manual Installation

#### Install TimescaleDB with Docker

```bash
docker run -d \
  --name timescaledb \
  -p 5432:5432 \
  -e POSTGRES_PASSWORD=secret \
  -e POSTGRES_DB=timeseries \
  timescale/timescaledb:latest-pg16
```

#### Build and Run Locally

1. Clone the repository:
```bash
git clone https://github.com/penguinpowernz/timeflux.git
cd timeflux
```

2. Create configuration file:
```bash
cp config.yaml.example config.yaml
```

3. Edit `config.yaml` to match your TimescaleDB connection settings:
```yaml
server:
  port: 8086

database:
  host: localhost  # Use 'timescaledb' if running in Docker
  port: 5432
  database: timeseries
  user: postgres
  password: secret
  pool_size: 32

logging:
  level: info
  format: json

wal:
  enabled: true
  path: /tmp/timeflux/wal
  num_workers: 8
  fsync_interval_ms: 100
  segment_size_mb: 64
  segment_cache_size: 2
  no_sync: false  # set to true for testing only (faster but no crash safety)

auth:
  enabled: false  # set to true to require authentication
```

4. Build the application:
```bash
make build
```

5. Run the application:
```bash
make run
# or
bin/timeflux -config config.yaml
```

The server will start on port 8086 (or the port specified in your config).

## Authentication

Timeflux supports user-based authentication and authorization with granular permissions at the database and measurement level.

### Enabling Authentication

Set `auth.enabled: true` in your `config.yaml` file:

```yaml
auth:
  enabled: true
```

### User Management

User management is performed via command-line interface:

**Add a user:**
```bash
# With auto-generated password
bin/timeflux user:add alice
# With specified password
bin/timeflux user:add alice mypassword
```

**Grant permissions:**
```bash
# Grant read/write to entire database
bin/timeflux user:grant alice mydb:rw

# Grant read-only to specific measurement
bin/timeflux user:grant alice mydb.cpu:r

# Grant write to all databases (wildcard)
bin/timeflux user:grant alice "*:w"

# Grant read to specific measurement across all databases
bin/timeflux user:grant alice "*.cpu:r"
```

**List users and permissions:**
```bash
# List all users
bin/timeflux user:list

# Show user details and permissions
bin/timeflux user:show alice
```

**Other commands:**
```bash
# Reset password
bin/timeflux user:reset-password alice newpassword

# Revoke permission
bin/timeflux user:revoke alice mydb.cpu

# Delete user
bin/timeflux user:delete alice
```

### Authentication Methods

Timeflux supports multiple authentication methods compatible with InfluxDB clients:

**Query parameters:**
```bash
curl 'http://localhost:8086/query?u=alice&p=password&db=mydb&q=SELECT+*+FROM+cpu'
```

**Basic Auth:**
```bash
curl -u alice:password 'http://localhost:8086/query?db=mydb&q=SELECT+*+FROM+cpu'
```

**Token header (username:password):**
```bash
curl -H 'Authorization: Token alice:password' \
  'http://localhost:8086/query?db=mydb&q=SELECT+*+FROM+cpu'
```

### Permission Model

Permissions are granted at two levels:
- **Database level**: Access to all measurements in a database (`database:rw`)
- **Measurement level**: Access to specific measurements (`database.measurement:r`)

Wildcards are supported:
- `*:rw` - All databases, all measurements
- `*.cpu:r` - CPU measurement across all databases
- `mydb.*:w` - All measurements in mydb (implied when using `mydb:w`)

Permission precedence (most specific wins):
1. `database.measurement` (specific measurement)
2. `database.*` (all measurements in database)
3. `*.measurement` (specific measurement across databases)
4. `*.*` (global wildcard)

## Usage

### Writing Data

Write data using InfluxDB line protocol:

```bash
curl -i -XPOST 'http://localhost:8086/write?db=mydb' \
  --data-binary 'cpu_usage,host=server1,region=us-east usage_percent=85.3,load_avg=2.5 1620000000000000000'
```

Multiple lines can be written in a single request:

```bash
curl -i -XPOST 'http://localhost:8086/write?db=mydb' \
  --data-binary 'cpu_usage,host=server1 usage_percent=85.3 1620000000000000000
cpu_usage,host=server2 usage_percent=72.1 1620000000000000000
memory,host=server1 used_percent=68.5 1620000000000000000'
```

**Response**: `HTTP/1.1 204 No Content`

### Querying Data

Execute InfluxQL queries:

```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SELECT mean(usage_percent) FROM cpu_usage WHERE time > now() - 1h GROUP BY time(5m)'
```

**Response**:
```json
{
  "results": [
    {
      "statement_id": 0,
      "series": [
        {
          "columns": ["time", "mean"],
          "values": [
            ["2024-02-24T10:00:00Z", 82.5],
            ["2024-02-24T10:05:00Z", 85.3],
            ["2024-02-24T10:10:00Z", 88.1]
          ]
        }
      ]
    }
  ]
}
```

### Database Management

Create a database:
```bash
curl -G 'http://localhost:8086/query' \
  --data-urlencode 'q=CREATE DATABASE mydb'
```

Show all databases:
```bash
curl -G 'http://localhost:8086/query' \
  --data-urlencode 'q=SHOW DATABASES'
```

Drop a database:
```bash
curl -G 'http://localhost:8086/query' \
  --data-urlencode 'q=DROP DATABASE mydb'
```

### Schema Introspection

Show all measurements:
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SHOW MEASUREMENTS'
```

Show series (unique tag combinations):
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SHOW SERIES'
```

Show tag keys for a measurement:
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SHOW TAG KEYS FROM cpu_usage'
```

Show field keys for a measurement:
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SHOW FIELD KEYS FROM cpu_usage'
```

### Data Management

Drop series matching specific tags:
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=DROP SERIES FROM cpu_usage WHERE host='\''server1'\'''
```

Drop an entire measurement:
```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=DROP MEASUREMENT cpu_usage'
```

## Supported InfluxQL Features

### Aggregate Functions
- `mean()` → `AVG()`
- `count()` → `COUNT()`
- `sum()` → `SUM()`
- `max()` → `MAX()`
- `min()` → `MIN()`

### Time Functions
- `GROUP BY time(5m)` → `time_bucket('5 minutes', time)`
- `now()` → `NOW()`

### Query Features
- SELECT with fields and aggregations
- WHERE clauses with comparison operators
- GROUP BY time and tags
- ORDER BY
- LIMIT and OFFSET

### Schema Queries
- `SHOW MEASUREMENTS`
- `SHOW TAG KEYS`
- `SHOW FIELD KEYS`
- `SHOW DATABASES`
- `SHOW SERIES`

### Database Management
- `CREATE DATABASE {name}`
- `DROP DATABASE {name}`

### Data Management
- `DROP SERIES FROM {measurement} WHERE {condition}` - Delete rows matching tag filters
- `DROP MEASUREMENT {name}` - Delete entire measurement table

## Type Inference

Field types are inferred from InfluxDB line protocol:

| Line Protocol | PostgreSQL Type |
|---|---|
| `field=42.5` | DOUBLE PRECISION |
| `field=42i` | BIGINT |
| `field="value"` | TEXT |
| `field=true` or `field=t` | BOOLEAN |

## API Endpoints

- `POST /write?db={database}` - Write data in line protocol format
- `GET /query?db={database}&q={query}` - Execute InfluxQL query
- `POST /query?db={database}` - Execute InfluxQL query (body contains query)
- `GET /ping` - Health check (returns 204)
- `GET /health` - Health status (returns JSON)
- `GET /metrics` - Prometheus-style metrics (JSON format)

## Project Structure

```
/
├── main.go                 # Entry point, HTTP server setup
├── Makefile               # Build automation
├── go.mod
├── go.sum
├── config.yaml            # Configuration file
├── bin/                   # Built binaries
├── config/
│   └── config.go          # Configuration management
├── schema/
│   └── manager.go         # Schema cache and DDL coordination
├── write/
│   ├── handler.go         # Write endpoint handler
│   ├── lineprotocol.go    # Line protocol parser
│   ├── wal_buffer.go      # WAL buffer and worker pool
│   └── wal_entry.go       # WAL entry format with CRC32 checksums
├── query/
│   ├── handler.go         # Query endpoint handler
│   └── translator.go      # InfluxQL to SQL translator
├── auth/
│   ├── auth.go            # Credential parsing
│   ├── middleware.go      # Authentication and authorization middleware
│   └── user_manager.go    # User and permission management
├── usercli/
│   └── user.go            # User management CLI commands
├── metrics/
│   └── metrics.go         # Metrics collection
└── README.md
```

## Limitations and Future Enhancements

### Current Limitations
- InfluxDB v2 API not supported (Flux query language)
- Bearer token (JWT) authentication not yet implemented
- Single instance only (no clustering)
- Subset of InfluxQL features supported
- Measurement-level permissions extracted at database level (not from query content)

### Future Enhancements
- JWT/Bearer token authentication
- Fine-grained measurement extraction from queries for permission checks
- Multiple instances with shared metadata
- Query result caching
- Rate limiting
- Broader InfluxQL support
- Prometheus metrics endpoint (currently JSON)
- Admin API for schema inspection
- User session management and audit logging

## Example: Using with Telegraf

You can use Timeflux as a drop-in replacement for InfluxDB in Telegraf:

```toml
# telegraf.conf
[[outputs.influxdb]]
  urls = ["http://localhost:8086"]
  database = "telegraf"
  skip_database_creation = true
```

Telegraf will write data to Timeflux, which will store it in TimescaleDB.

## Migration Strategy

1. Deploy Timeflux alongside your existing InfluxDB instance
2. Test with one non-critical system first
3. Gradually migrate other systems to point at Timeflux
4. Monitor performance and adjust as needed
5. Eventually retire the original InfluxDB instance
6. Migrate services to native TimescaleDB queries over time

Since data is stored in regular PostgreSQL columns, migrating to native SQL queries is straightforward.

## Performance Considerations

### Write Performance

**With WAL Enabled (Default):**
- Write requests append to WAL and return immediately (~1-2ms)
- Background workers process WAL entries in batches
- 10x faster than synchronous writes
- Typical throughput: 500+ batches/second (vs 50 batches/second synchronous)
- Query lag: 1-5 seconds (acceptable for metrics/observability)

**Synchronous Mode (WAL Disabled):**
- Each write blocks until data is committed to TimescaleDB
- Uses PostgreSQL COPY for bulk inserts
- Parallel writes across different measurements
- Typical throughput: 50 batches/second

**Schema Evolution:**
- Tag indexes created asynchronously in background (non-blocking)
- DDL operations batched in transactions
- Per-measurement locking prevents contention
- Schema cache minimizes database round-trips

**Query Performance:**
- TimescaleDB automatic time-based partitioning
- Indexed tag columns for fast filtering
- Background index creation doesn't block writes

## Troubleshooting

### Connection Refused
Ensure TimescaleDB is running and accessible:
```bash
docker ps | grep timescaledb
psql -h localhost -U postgres -d timeseries -c "SELECT version();"
```

### Schema Not Found
Check that the database parameter is correct:
```bash
curl 'http://localhost:8086/write?db=testdb' -d 'test value=1'
```

### Query Translation Errors
Check logs for unsupported InfluxQL features. The translator currently supports a subset of InfluxQL.

## License

MIT License

## Development

### Build Commands

```bash
make build        # Build binary to bin/timeflux
make clean        # Remove built binaries
make test         # Run tests
make run          # Build and run

make up           # Start Docker Compose services
make down         # Stop Docker Compose services
make logs         # Follow timeflux logs
make reup         # Rebuild and restart timeflux container
make dcl          # Stop and remove volumes
```

For developers and AI assistants working on this project, see [CLAUDE.md](CLAUDE.md) for:
- Architecture overview
- Development guidelines
- Common issues and solutions
- Code patterns and conventions

## Contributing

Contributions are welcome! Please:
1. Read [CLAUDE.md](CLAUDE.md) for architecture guidance
2. Open an issue to discuss major changes
3. Submit pull requests with clear descriptions
4. Include tests where applicable
