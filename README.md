# Timeflux - InfluxDB v1 to TimescaleDB Facade

Timeflux is a Go-based HTTP service that implements the InfluxDB v1 API, translating requests to TimescaleDB on the backend. This allows existing systems using InfluxDB clients to seamlessly switch to TimescaleDB without code changes.

You can use this to help migrate away from InfluxDB towards TimescaleDB or keep it as a structured layer on top of TimescaleDB.

## Features

- **InfluxDB v1 API Compatible**: Supports write and query endpoints
- **Line Protocol Support**: Parse and write InfluxDB line protocol data
- **InfluxQL Query Support**: Translate InfluxQL queries to PostgreSQL SQL
- **Influx CLI Support**: Use the Influx CLI tool to interact with TimescaleDB
- **Grafana Support**: Use with Grafana like you would Influx
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
- Docker (optional, for instantly running combined TimescaleDB and Timeflux)

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

### Functions

| | InfluxQL | PostgreSQL / TimescaleDB Analog | Notes | Difficulty |
|---|---|---|---|---|
| ✅ | `mean(field)` | `AVG(field)` | | Easy |
| ✅ | `count(field)` | `COUNT(field)` | | Easy |
| ✅ | `sum(field)` | `SUM(field)` | | Easy |
| ✅ | `min(field)` | `MIN(field)` | | Easy |
| ✅ | `max(field)` | `MAX(field)` | | Easy |
| ✅ | `stddev(field)` | `STDDEV(field)` | | Easy |
| ✅ | `median(field)` | `percentile_cont(0.5) WITHIN GROUP (ORDER BY field)` | | Easy |
| ✅ | `spread(field)` | `MAX(field) - MIN(field)` | | Easy |
| ✅ | `mode(field)` | `MODE() WITHIN GROUP (ORDER BY field)` | | Easy |
| ❌ | `distinct(field)` | `COUNT(DISTINCT field)` | | Easy |
| ✅ | `first(field)` | `FIRST(field, time)` | TimescaleDB function | Easy |
| ✅ | `last(field)` | `LAST(field, time)` | TimescaleDB function | Easy |
| ✅ | `percentile(field, N)` | `percentile_cont(N/100) WITHIN GROUP (ORDER BY field)` | N is 0–100 in InfluxQL | Easy |
| ❌ | `top(field, N)` | Subquery with `ORDER BY field DESC LIMIT N` | | Medium |
| ❌ | `bottom(field, N)` | Subquery with `ORDER BY field ASC LIMIT N` | | Medium |
| ❌ | `sample(field, N)` | Subquery with `ORDER BY RANDOM() LIMIT N` | | Medium |
| ✅ | `now()` | `NOW()` | | Easy |
| ✅ | `abs(field)` | `ABS(field)` | | Easy |
| ✅ | `ceil(field)` | `CEIL(field)` | | Easy |
| ✅ | `floor(field)` | `FLOOR(field)` | | Easy |
| ✅ | `round(field)` | `ROUND(field)` | | Easy |
| ✅ | `sqrt(field)` | `SQRT(field)` | | Easy |
| ✅ | `pow(field, exp)` | `POWER(field, exp)` | | Easy |
| ✅ | `exp(field)` | `EXP(field)` | | Easy |
| ✅ | `ln(field)` | `LN(field)` | | Easy |
| ✅ | `log(field, base)` | `LOG(base::numeric, field::numeric)` | Arg order swapped; both args cast to numeric | Easy |
| ✅ | `log2(field)` | `LOG(2::numeric, field::numeric)` | | Easy |
| ✅ | `log10(field)` | `LOG(10::numeric, field::numeric)` | | Easy |
| ✅ | `sin(field)` | `SIN(field)` | Input in radians | Easy |
| ✅ | `cos(field)` | `COS(field)` | Input in radians | Easy |
| ✅ | `tan(field)` | `TAN(field)` | Input in radians | Easy |
| ✅ | `asin(field)` | `ASIN(field)` | Input must be in [-1, 1] | Easy |
| ✅ | `acos(field)` | `ACOS(field)` | Input must be in [-1, 1] | Easy |
| ✅ | `atan(field)` | `ATAN(field)` | | Easy |
| ✅ | `atan2(y, x)` | `ATAN2(y, x)` | Same arg order | Easy |
| ❌ | `cumulative_sum(field)` | Window function with `SUM() OVER (ORDER BY time ROWS UNBOUNDED PRECEDING)` | | Medium |
| ❌ | `derivative(field[, unit])` | `(field - LAG(field) OVER (...)) / time_diff` | Window function | Medium |
| ❌ | `non_negative_derivative(field)` | `GREATEST(derivative, 0)` | | Medium |
| ❌ | `difference(field)` | `field - LAG(field) OVER (ORDER BY time)` | Window function | Medium |
| ❌ | `non_negative_difference(field)` | `GREATEST(difference, 0)` | | Medium |
| ❌ | `moving_average(field, N)` | `AVG(field) OVER (ORDER BY time ROWS BETWEEN N-1 PRECEDING AND CURRENT ROW)` | Window function | Medium |
| ❌ | `elapsed(field[, unit])` | `EXTRACT(EPOCH FROM time - LAG(time) OVER (...))` | Window function | Medium |
| ❌ | `integral(field[, unit])` | Trapezoidal rule via window functions | | Medium–Hard |
| ❌ | `histogram(field, min, max, N)` | `WIDTH_BUCKET(field, min, max, N)` with GROUP BY | | Medium–Hard |
| ❌ | `exponential_moving_average(field, N)` | Recursive CTE or PL/pgSQL | | Hard |
| ❌ | `double_exponential_moving_average(field, N)` | Custom function | | Hard |
| ❌ | `triple_exponential_moving_average(field, N)` | Custom function | | Hard |
| ❌ | `triple_exponential_derivative(field, N)` | Custom function | | Hard |
| ❌ | `relative_strength_index(field, N)` | Custom PL/pgSQL function | | Hard |
| ❌ | `chande_momentum_oscillator(field, N)` | Custom PL/pgSQL function | | Hard |
| ❌ | `kaufmans_efficiency_ratio(field, N)` | Custom PL/pgSQL function | | Hard |
| ❌ | `kaufmans_adaptive_moving_average(field, N)` | Custom PL/pgSQL function | | Hard |
| ❌ | `holt_winters(field, N, m)` | No direct analog — application-layer or PL/Python | Forecasting function | Very Hard |

See [FUNCTIONS.md](FUNCTIONS.md) for translation strategies and implementation phases.

### Query Features

| | Feature | Notes |
|---|---|---|
| ✅ | `SELECT` with fields and aggregations | |
| ✅ | `WHERE` with comparison operators (`=`, `!=`, `<`, `>`, `<=`, `>=`) | |
| ✅ | `WHERE` with `AND` / `OR` and parenthesized expressions | |
| ✅ | `GROUP BY time(interval)` | Maps to `time_bucket()` |
| ✅ | `GROUP BY tag` | |
| ✅ | `ORDER BY` | |
| ✅ | `LIMIT` / `OFFSET` | |
| ✅ | `SHOW MEASUREMENTS` | |
| ✅ | `SHOW TAG KEYS` | |
| ✅ | `SHOW TAG VALUES WITH KEY = ...` | |
| ✅ | `SHOW FIELD KEYS` | |
| ✅ | `SHOW DATABASES` | |
| ✅ | `SHOW SERIES` | |
| ✅ | `CREATE DATABASE` | Creates a PostgreSQL schema |
| ✅ | `DROP DATABASE` | Drops the PostgreSQL schema |
| ✅ | `DROP SERIES FROM ... WHERE ...` | Translates to `DELETE FROM` |
| ✅ | `DROP MEASUREMENT` | Drops the hypertable |
| ❌ | Subqueries | Not yet supported |
| ❌ | `FILL()` | No equivalent translation yet |
| ❌ | `INTO` (write query results) | Not yet supported |

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

## Feature Compatibility

| Status | Test | Description | Duration |
|--------|------|-------------|----------|
| ✅ | BasicWrite | Write single point with one field | 6.801163ms |
| ✅ | MultiFieldWrite | Write point with multiple fields of different types | 5.709709ms |
| ✅ | TaggedWrite | Write points with tags | 5.225282ms |
| ✅ | BatchWrite | Write batch of 100 points | 5.968928ms |
| ✅ | AllDataTypes | Write point with all supported data types | 2.576475ms |
| ✅ | SimpleSelect | SELECT * FROM measurement | 2.728004ms |
| ✅ | SelectWithWhere | SELECT with WHERE clause filtering tags | 2.804248ms |
| ✅ | SelectMean | SELECT MEAN() aggregation | 4.23132ms |
| ✅ | GroupByTime | SELECT with GROUP BY time(5m) | 7.667817ms |
| ✅ | GroupByTag | SELECT with GROUP BY tag | 6.060589ms |
| ✅ | Count | SELECT COUNT(*) | 2.649679ms |
| ✅ | Sum | SELECT SUM() | 3.559284ms |
| ✅ | MinMax | SELECT MIN() and MAX() | 4.839366ms |
| ✅ | ShowMeasurements | SHOW MEASUREMENTS | 1.762354ms |
| ✅ | ShowTagKeys | SHOW TAG KEYS | 1.027113ms |
| ✅ | ShowFieldKeys | SHOW FIELD KEYS | 1.048455ms |
| ✅ | CreateDatabase | CREATE DATABASE | 660.542µs |
| ✅ | ShowDatabases | SHOW DATABASES | 2.262632ms |
| ✅ | ShowSeries | SHOW SERIES | 1.269345ms |
| ✅ | DropSeries | DROP SERIES with WHERE clause | 112.11249ms |
| ✅ | DropMeasurement | DROP MEASUREMENT | 104.275748ms |
| ✅ | FirstLast | SELECT FIRST() and LAST() functions | 2.221247ms |
| ✅ | Percentile | SELECT PERCENTILE() function | 4.179828ms |
| ✅ | MultipleAggregations | SELECT multiple aggregations in one query | 4.536375ms |
| ✅ | ArithmeticOperations | SELECT with arithmetic operations (+, -, *, /) | 2.36909ms |
| ✅ | ComplexWhere | SELECT with complex WHERE (AND, OR, comparison operators) | 18.835663ms |
| ✅ | ShowTagValues | SHOW TAG VALUES with KEY | 1.515577ms |
| ✅ | Limit | SELECT with LIMIT clause | 1.038193ms |
| ✅ | Offset | SELECT with OFFSET clause | 1.010519ms |
| ✅ | OrderBy | SELECT with ORDER BY clause | 1.647866ms |
| ✅ | TimeRange | SELECT with time range in WHERE | 11.813937ms |
| ✅ | GroupByTimeIntervals | Test different GROUP BY time() intervals (1m, 5m, 1h) | 4.580791ms |
| ✅ | GroupByMultipleTags | SELECT with GROUP BY multiple tags | 207.422398ms |
| ✅ | NowFunction | Use NOW() function in WHERE clause | 1.275609ms |
| ✅ | BooleanFields | Query boolean fields with WHERE | 3.110882ms |
| ✅ | StringFields | Query string fields with WHERE | 1.675472ms |
| ✅ | NegativeNumbers | Query with negative numbers and zero | 205.440891ms |
| ✅ | DropSeries | DROP SERIES with WHERE clause | 106.281272ms |
| ✅ | DropMeasurement | DROP MEASUREMENT | 103.553971ms |
| ✅ | DropDatabase | DROP DATABASE | 107.279086ms |
| ✅ | WriteNumericData | Write known numeric dataset for Phase 1 function tests | 3.750134ms |
| ✅ | Stddev | SELECT STDDEV() aggregation | 1.663358ms |
| ✅ | Median | SELECT MEDIAN() aggregation | 1.048993ms |
| ✅ | Spread | SELECT SPREAD() (MAX-MIN) aggregation | 1.606488ms |
| ✅ | Abs | SELECT ABS() on negative values | 1.15031ms |
| ✅ | Ceil | SELECT CEIL() ceiling function | 1.343904ms |
| ✅ | Floor | SELECT FLOOR() floor function | 1.42966ms |
| ✅ | Round | SELECT ROUND() rounding function | 2.390172ms |
| ✅ | Sqrt | SELECT SQRT() square root function | 3.822994ms |
| ✅ | Pow | SELECT POW(field, exponent) power function | 2.146658ms |
| ✅ | Exp | SELECT EXP() exponential function | 1.17573ms |
| ✅ | Ln | SELECT LN() natural logarithm | 1.061525ms |
| ✅ | Log2 | SELECT LOG2() base-2 logarithm | 1.360194ms |
| ✅ | Log10 | SELECT LOG10() base-10 logarithm | 1.08444ms |
| ✅ | LogBase | SELECT LOG(field, base) custom base logarithm | 1.051762ms |
| ✅ | Sin | SELECT SIN() sine function (radians) | 1.112369ms |
| ✅ | Cos | SELECT COS() cosine function (radians) | 1.198539ms |
| ✅ | Tan | SELECT TAN() tangent function (radians) | 1.451375ms |
| ✅ | Asin | SELECT ASIN() arcsine (input in [-1,1]) | 1.493281ms |
| ✅ | Acos | SELECT ACOS() arccosine (input in [-1,1]) | 1.561699ms |
| ✅ | Atan | SELECT ATAN() arctangent | 1.289703ms |
| ✅ | Atan2 | SELECT ATAN2(y, x) two-argument arctangent | 1.499374ms |

**Summary:** 62/62 passed in 2.1136693s

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
- Tag based permissions
- Multi instance for HA setups
- Replica awareness for distributed reads with writing to the master
- Support entire InfluxDB function set
- Store metrics in an "_internal" database like InfluxDB

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

## Motivation

I made this tool because I love the InfluxDB v1 interface - Influx Line Protocol and InfluxQL.  But I grew to dislike the
backing database, being too inflexible to delete rows without crashing my InfluxCloud cluster.  However PostgresQL can also
be a beast despite or because of its infinite utility.  Timeflux creates a nice layer providing the best of both worlds.

I hope this can be of help to others with aging InfluxDB installs who don't want to change all their infra and tooling to
switch databases.  I used claude to help develop this, monitoring it closely and driving it to make the correct architectural
decisions while relying on it for technical advice, and options.

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
