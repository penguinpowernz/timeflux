
I need you to build an InfluxDB v1 API-compatible facade that writes to TimescaleDB as the backend storage. This will allow existing systems using InfluxDB clients to write data to TimescaleDB without modification.

## Requirements

### API Compatibility
- Implement InfluxDB v1 HTTP API endpoints (write and query)
- Support InfluxDB line protocol for writes
- Support InfluxQL for queries (not Flux - this is v1 only)
- HTTP server in Go

### Data Model Strategy
- Use dynamic schema evolution: automatically create tables and add columns as new measurements, tags, and fields appear
- One TimescaleDB hypertable per InfluxDB measurement
- Tags become TEXT columns with indexes (indexed as: tag_name, time DESC)
- Fields become typed columns (DOUBLE PRECISION for floats, BIGINT for integers, TEXT for strings, BOOLEAN for booleans)
- Infer types from InfluxDB line protocol type suffixes (bare number=float, 'i' suffix=integer, quoted=string, t/f/true/false=boolean)
- Time column makes each table a hypertable

### Concurrency Model
- Single instance application (no distributed coordination needed)
- Must handle multiple concurrent write requests safely
- Use in-memory schema cache with sync.RWMutex for fast-path checks
- Per-measurement mutexes (sync.Map) to serialize DDL operations per table
- Double-check pattern: after acquiring lock, verify another goroutine didn't already add the column

### Schema Management Implementation
```go
type SchemaManager struct {
    mu sync.RWMutex
    schemas map[string]*MeasurementSchema // measurement -> schema
    measurementLocks sync.Map // measurement -> *sync.Mutex for DDL
    db *pgxpool.Pool
}

type MeasurementSchema struct {
    tags   map[string]bool      // tag name -> exists
    fields map[string]string    // field name -> SQL type
}
```

Flow for each write batch:
1. Parse line protocol to extract measurement, tags, fields, timestamp
2. Fast path: RLock and check if in-memory cache has all needed columns
3. If yes, proceed to insert
4. If no, acquire measurement-specific lock, double-check, then do DDL:
   - CREATE TABLE if measurement doesn't exist
   - SELECT create_hypertable() to make it a TimescaleDB hypertable
   - ALTER TABLE ADD COLUMN for missing tags/fields
   - CREATE INDEX for new tag columns
   - Update in-memory cache
5. Use COPY FROM for bulk inserts (best performance)

### Key Implementation Details
- Use pgx/pgxpool for PostgreSQL connection
- On startup, load existing table schemas from information_schema to populate cache
- Use `ALTER TABLE ADD COLUMN IF NOT EXISTS` to handle race conditions gracefully
- Create indexes with `CREATE INDEX IF NOT EXISTS`
- Use pgx.Identifier{}.Sanitize() for safe SQL identifier quoting
- Type inference from line protocol:
  - `field=42.5` → DOUBLE PRECISION
  - `field=42i` → BIGINT
  - `field="value"` → TEXT
  - `field=true` or `field=t` → BOOLEAN

### API Endpoints to Implement

**Write endpoint:**
- POST /write?db={database}
- Accept InfluxDB line protocol in request body
- Parse line protocol
- Ensure schema exists (create tables/columns if needed)
- Insert data using COPY FROM for performance
- Return 204 on success, appropriate errors otherwise

**Query endpoint:**
- GET or POST /query?db={database}&q={influxql_query}
- Parse InfluxQL query
- Translate to SQL:
  - InfluxDB measurement → TimescaleDB table name
  - Tags/fields → column names
  - InfluxDB time functions → TimescaleDB equivalents (e.g., GROUP BY time(5m) → time_bucket('5 minutes', time))
  - InfluxDB aggregations (mean, sum, count, etc.) → SQL aggregations
- Execute SQL query
- Format results back to InfluxDB JSON format
- Return results with proper InfluxDB response structure

### Translation Examples

InfluxQL:
```sql
SELECT mean(usage_percent) FROM cpu_usage 
WHERE host = 'server1' AND time > now() - 1h
GROUP BY time(5m)
```

Should become:
```sql
SELECT 
  time_bucket('5 minutes', time) AS time,
  AVG(usage_percent) AS mean
FROM cpu_usage
WHERE host = 'server1' 
  AND time > NOW() - INTERVAL '1 hour'
GROUP BY time_bucket('5 minutes', time)
ORDER BY time
```

### Project Structure
/
├── main.go                 # Entry point, HTTP server setup
├── go.mod
├── go.sum
├── config/
│   └── config.go          # Configuration (DB connection, port, etc.)
├── schema/
│   └── manager.go         # SchemaManager implementation
├── write/
│   ├── handler.go         # Write endpoint handler
│   └── lineprotocol.go    # Line protocol parser
├── query/
│   ├── handler.go         # Query endpoint handler
│   ├── influxql_parser.go # InfluxQL parser
│   └── translator.go      # InfluxQL to SQL translator
└── README.md

### Configuration
- PostgreSQL connection string (host, port, database, user, password)
- HTTP server port (default 8086 to match InfluxDB)
- Connection pool size
- Optional: logging level, metrics endpoint

### Error Handling
- Graceful handling of malformed line protocol
- Proper HTTP error codes (400 for bad requests, 500 for server errors)
- Log DDL operations for debugging
- Transaction rollback on insert failures

### Testing Considerations
- Include example curl commands for testing writes
- Include example InfluxQL queries for testing reads
- Document how to set up TimescaleDB (docker-compose example)

### Non-Requirements (Out of Scope)
- No retention policies (can be added later with TimescaleDB retention policies)
- No continuous queries (can be added later with TimescaleDB continuous aggregates)
- No authentication (can be added later)
- No clustering/replication (single instance)
- No Flux query language (only InfluxQL)
- Don't need to support InfluxDB v2 API

## Deliverables

1. Working Go application with all endpoints
2. README with:
   - How to build and run
   - How to set up TimescaleDB
   - Example write and query commands
   - Architecture overview
3. Well-commented code
4. Basic error handling and logging

Please implement this step by step, starting with the core schema management, then write endpoint, then query endpoint. Ask clarifying questions if anything is ambiguous.
