
# InfluxDB to TimescaleDB Facade - Product Brief

## Executive Summary

Build a Go-based HTTP service that implements the InfluxDB v1 API, translating requests to TimescaleDB on the backend. This allows existing systems using InfluxDB clients to seamlessly switch to TimescaleDB without code changes.

## Problem Statement

We have multiple systems and architectures configured to write to and query from InfluxDB. We want to migrate to TimescaleDB for its PostgreSQL ecosystem benefits, but rewriting all client code would be expensive and risky. We need a facade layer that makes TimescaleDB appear as InfluxDB to existing clients.

## Goals

1. **Zero client-side changes**: Existing InfluxDB clients continue working without modification
2. **Dynamic schema evolution**: Support InfluxDB's schemaless model where tags and fields can be added on-the-fly
3. **Native PostgreSQL migration path**: Eventually migrate services to native TimescaleDB/PostgreSQL queries without JSONB overhead
4. **Production-ready**: Handle concurrent writes safely in a single-instance deployment

## Non-Goals

- InfluxDB v2 API support (Flux query language)
- Authentication/authorization (can be added later)
- Clustering or distributed deployment
- Retention policies (use TimescaleDB native features)
- Continuous queries (use TimescaleDB continuous aggregates)
- High availability / replication

## Technical Architecture

### API Compatibility

**Supported:**
- InfluxDB v1 HTTP API
- Line protocol for writes
- InfluxQL for queries
- Database selection via query parameters

**Endpoints:**
- `POST /write?db={database}` - Write data in line protocol format
- `GET /query?db={database}&q={query}` - Execute InfluxQL query
- `POST /query?db={database}` - Execute InfluxQL query (body contains query)

### Data Model Strategy

**Core Principle:** Dynamic schema evolution with native PostgreSQL columns

Each InfluxDB concept maps to a TimescaleDB equivalent:

| InfluxDB Concept | TimescaleDB Implementation |
|---|---|
| Database | PostgreSQL schema |
| Measurement | Hypertable (one per measurement) |
| Tags (indexed metadata) | TEXT columns with indexes |
| Fields (measured values) | Typed columns (float, integer, text, boolean) |
| Timestamp | TIMESTAMPTZ column (makes it a hypertable) |

**Schema Evolution:**
When a write contains new tags or fields not seen before, the service automatically alters the table to add the new column, creates an index for any new tag columns, records the change in a metadata table, and updates its in-memory schema cache. This happens transparently, with no downtime or manual intervention required.

**Type Inference:**
InfluxDB's line protocol encodes type information directly in the data. The service reads this and maps it to the appropriate PostgreSQL column type — floats, integers, text strings, and booleans are all handled and stored as their native database types rather than as generic text.

### Tag vs Field Tracking

A small internal metadata table tracks which columns are tags and which are fields for each measurement. This is important because tags and fields behave differently — tags are indexed and can be grouped by, while fields hold the actual measured values and can be aggregated. The metadata table is consulted when translating queries and when evolving the schema.

### Concurrency Model

The service runs as a single Go process. Concurrent writes are coordinated using in-process locking with two levels of granularity:

- **Schema reads** (the common case) use a shared read lock, so many requests can check the schema simultaneously without blocking each other.
- **Schema changes** (adding new columns) use a per-measurement write lock, so DDL operations on different measurements don't interfere with each other.

A double-check pattern is used when a schema change is needed: the process acquires the write lock and checks again before issuing any DDL, preventing redundant operations if two requests race to add the same column.

### Query Translation Architecture

**Approach:** Use the official `influxql` Go package to parse InfluxQL, then translate the resulting AST into PostgreSQL SQL.

Rather than using regular expressions or string manipulation to parse InfluxQL queries, the service uses the `github.com/influxdata/influxql` package. This is the same parser extracted from the InfluxDB v1 source and published as a standalone library. It produces a fully structured abstract syntax tree (AST) that accurately represents the query's intent, including all edge cases in the InfluxQL grammar.

The translator then walks this AST and generates equivalent PostgreSQL SQL. The key translations are:

- Aggregate functions like `mean()`, `count()`, `sum()`, `max()`, and `min()` map to their SQL equivalents.
- `GROUP BY time(5m)` maps to TimescaleDB's `time_bucket()` function, which is purpose-built for this kind of time-series aggregation.
- Time range expressions like `now() - 1h` are resolved to concrete timestamps at parse time by the `influxql` library, so the translator receives absolute values rather than having to handle the relative time arithmetic itself.
- Tag and field references in WHERE clauses are translated to standard SQL column comparisons.

Using a proper AST-based approach rather than regex means the translator is robust to variations in query formatting and can be extended cleanly as more InfluxQL features need to be supported. Unsupported query features can be detected early and surfaced as clear error messages.

**Example Translation:**

```
InfluxQL:
  SELECT mean(usage) FROM cpu WHERE host='server1' AND time > now() - 1h GROUP BY time(5m)

PostgreSQL:
  SELECT time_bucket('5 minutes', time) AS time, AVG(usage) AS mean
  FROM cpu
  WHERE host = 'server1' AND time > NOW() - INTERVAL '1 hour'
  GROUP BY time_bucket('5 minutes', time)
  ORDER BY time
```

Schema introspection queries (`SHOW MEASUREMENTS`, `SHOW FIELD KEYS`, `SHOW TAG KEYS`, etc.) are translated into queries against the internal metadata table or PostgreSQL's information schema.

## Technology Stack

- **Language:** Go
- **Database Driver:** pgx/pgxpool (PostgreSQL/TimescaleDB)
- **HTTP Server:** Go standard library
- **Config Format:** YAML
- **Line Protocol Parser:** Custom implementation
- **Query Parser:** `github.com/influxdata/influxql` (official InfluxDB v1 parser)

## Project Structure

```
/
├── main.go
├── go.mod
├── go.sum
├── config/
│   ├── config.go
│   └── config.yaml.example
├── schema/
│   ├── manager.go          # Schema cache and DDL coordination
│   └── metadata.go         # Metadata table operations
├── write/
│   ├── handler.go          # HTTP write endpoint
│   ├── lineprotocol.go     # Line protocol parser
│   └── inserter.go         # Batch insert logic
├── query/
│   ├── handler.go          # HTTP query endpoint
│   ├── translator.go       # InfluxQL AST to SQL translator
│   └── executor.go         # Query execution and response formatting
└── README.md
```

## Implementation Phases

### Phase 1: Core Write Path
- [ ] Line protocol parser
- [ ] Schema manager with dynamic DDL
- [ ] Write endpoint handler
- [ ] Basic error handling and logging
- [ ] Manual testing with curl

### Phase 2: Basic Query Path
- [ ] InfluxQL parser integration
- [ ] Simple SELECT query translation
- [ ] WHERE clause translation
- [ ] Basic aggregation support (mean, count, sum)
- [ ] Query endpoint handler
- [ ] InfluxDB-compatible JSON response format

### Phase 3: Advanced Queries
- [ ] GROUP BY time() translation using time_bucket()
- [ ] Multiple GROUP BY dimensions (tags + time)
- [ ] Additional aggregations (max, min, percentile)
- [ ] Schema introspection queries (SHOW MEASUREMENTS, etc.)

### Phase 4: Production Readiness
- [ ] Comprehensive error handling
- [ ] Structured logging
- [ ] Metrics/observability
- [ ] Connection pool tuning
- [ ] Performance testing
- [ ] Documentation

## Configuration Example

```yaml
server:
  port: 8086  # Match InfluxDB default port

database:
  host: localhost
  port: 5432
  database: timeseries
  user: postgres
  password: secret
  pool_size: 32

logging:
  level: info  # debug, info, warn, error
  format: json
```

## API Examples

### Writing Data

```bash
curl -i -XPOST 'http://localhost:8086/write?db=mydb' \
  --data-binary 'cpu_usage,host=server1,region=us-east usage_percent=85.3,load_avg=2.5 1620000000000000000'
```

Response: `HTTP/1.1 204 No Content`

### Querying Data

```bash
curl -G 'http://localhost:8086/query?db=mydb' \
  --data-urlencode 'q=SELECT mean(usage_percent) FROM cpu_usage WHERE time > now() - 1h GROUP BY time(5m)'
```

Response:

```json
{
  "results": [
    {
      "statement_id": 0,
      "series": [
        {
          "name": "cpu_usage",
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

## Performance Considerations

**Write performance** is maximised by batching all points from a single request and using PostgreSQL's bulk copy mechanism rather than individual inserts. The in-memory schema cache means that for established measurements, no database lookups are needed before writing.

**Query performance** relies on TimescaleDB's automatic time-based partitioning, indexed tag columns, and the `time_bucket()` function which is optimised for time-series aggregations.

**Schema evolution** involves DDL operations that are inherently slower, but these are rare — they only occur when genuinely new tags or fields appear. Indexes are created concurrently to avoid blocking ongoing writes.

## Migration Strategy

1. Deploy the facade alongside the existing InfluxDB instance
2. Test with one non-critical system first
3. Gradually migrate other systems to point at the facade
4. Monitor performance and adjust as needed
5. Eventually retire the original InfluxDB instance
6. Migrate services to native TimescaleDB queries over time (since data is stored in regular PostgreSQL columns, this is straightforward)

## Success Metrics

- ✅ Existing InfluxDB clients work without modification
- ✅ Write latency < 50ms p99 for batches of 1000 points
- ✅ Query latency comparable to InfluxDB for common patterns
- ✅ Zero data loss during writes
- ✅ Schema evolution handles 100+ new tags/fields without issues

## Risk Mitigation

**Risk:** Query translation doesn't cover all InfluxQL features
**Mitigation:** The `influxql` parser identifies unsupported query types early. Start with the commonly-used subset, log unsupported queries, and add features incrementally. The AST-based approach makes adding new translations much lower risk than a regex approach.

**Risk:** Concurrent DDL operations cause deadlocks
**Mitigation:** Per-measurement locking and the double-check pattern prevent redundant DDL. All schema changes use `IF NOT EXISTS` clauses as a safety net.

**Risk:** Performance degradation vs native InfluxDB
**Mitigation:** Benchmark early, use TimescaleDB best practices (compression, indexing), profile under realistic write loads.

**Risk:** Complex nested queries fail translation
**Mitigation:** Keep initial scope to common query patterns, document the supported InfluxQL subset clearly, and log anything that falls outside it.

## Future Enhancements

- Authentication (basic auth, token-based)
- Multiple facade instances with shared metadata (Redis or PostgreSQL-backed)
- Query result caching
- Rate limiting
- Admin API for schema inspection
- Prometheus metrics endpoint
- Broader InfluxQL support (subqueries, regex matching, etc.)
- Automatic query optimisation based on observed usage patterns
