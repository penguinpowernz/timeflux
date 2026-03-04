# Continuous Queries - Investigation and Implementation Notes

This document covers InfluxDB v1 continuous queries (CQs) and how they could be implemented in Timeflux on top of TimescaleDB/PostgreSQL.

## What Are Continuous Queries?

Continuous queries are InfluxQL statements that run automatically and periodically within a database. InfluxDB stores the results in a specified measurement. They are used for:

- **Downsampling**: Automatically aggregating high-resolution data to lower resolutions
- **Pre-computing aggregations**: Improving query performance
- **Data pipeline**: Combined with retention policies to tier data across time ranges

Key characteristics:
- Require a function in the `SELECT` clause (e.g., `MEAN`, `SUM`, `COUNT`)
- Must include a `GROUP BY time()` clause
- Results written to a measurement specified in the `INTO` clause
- Only process data written after the CQ was created (no automatic backfilling)
- Execution is stateless — each run is a standalone query

---

## InfluxQL Syntax

### CREATE CONTINUOUS QUERY

Basic syntax:
```sql
CREATE CONTINUOUS QUERY <cq_name> ON <database_name>
BEGIN
  SELECT <function[s]> INTO <destination_measurement>
  FROM <measurement> [WHERE <stuff>]
  GROUP BY time(<interval>)[,<tag_key[s]>]
END
```

Example:
```sql
CREATE CONTINUOUS QUERY "cq_1h_avg" ON "telegraf"
BEGIN
  SELECT mean("value")
  INTO "downsampled_cpu"
  FROM "cpu"
  GROUP BY time(1h)
END
```

Advanced syntax with RESAMPLE:
```sql
CREATE CONTINUOUS QUERY <cq_name> ON <database_name>
RESAMPLE EVERY <interval> [FOR <interval>]
BEGIN
  <cq_query>
END
```

- `EVERY <interval>`: How often the CQ runs (execution frequency)
- `FOR <interval>`: How far back in time each run looks (lookback window)
- `FOR` must be >= `GROUP BY time()` interval (or `EVERY` interval if specified)

Example:
```sql
CREATE CONTINUOUS QUERY "cq_30m" ON "telegraf"
RESAMPLE EVERY 1h FOR 2h
BEGIN
  SELECT mean("value")
  INTO "downsampled_cpu"
  FROM "cpu"
  GROUP BY time(30m), host
END
```

To preserve tags in the destination measurement, use `GROUP BY *, ...`:
```sql
GROUP BY time(5m), *     -- preserve all tags
GROUP BY time(5m), host  -- preserve specific tags
```

### DROP CONTINUOUS QUERY

```sql
DROP CONTINUOUS QUERY <cq_name> ON <database_name>
```

Note: CQs cannot be altered — must DROP and re-CREATE.

### SHOW CONTINUOUS QUERIES

```sql
SHOW CONTINUOUS QUERIES
```

Returns all CQs across all databases.

---

## InfluxQL AST Node Types

From `vendor/github.com/influxdata/influxql/ast.go`:

### CreateContinuousQueryStatement

```go
type CreateContinuousQueryStatement struct {
    Name          string
    Database      string
    Source        *SelectStatement  // the SELECT ... INTO ... GROUP BY time() statement
    ResampleEvery time.Duration
    ResampleFor   time.Duration
}
```

### DropContinuousQueryStatement

```go
type DropContinuousQueryStatement struct {
    Name     string
    Database string
}
```

### ShowContinuousQueriesStatement

```go
type ShowContinuousQueriesStatement struct{}
```

These are handled in the translator via a type switch on the parsed statement.

---

## TimescaleDB/PostgreSQL Implementation Strategies

### Option A: TimescaleDB Continuous Aggregates (Recommended)

TimescaleDB continuous aggregates are materialized views with an automatic refresh policy, matching the semantics of InfluxDB CQs closely.

**Concept mapping:**

| InfluxDB CQ           | TimescaleDB                                              |
|-----------------------|----------------------------------------------------------|
| `CREATE CQ`           | `CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous)` + `add_continuous_aggregate_policy()` |
| `GROUP BY time(5m)`   | `time_bucket('5 minutes', time)`                         |
| `INTO measurement`    | Materialized view name                                   |
| `RESAMPLE EVERY 1h`   | `schedule_interval => INTERVAL '1 hour'`                 |
| `RESAMPLE FOR 2h`     | `start_offset => INTERVAL '2 hours'`                     |
| `DROP CQ`             | `DROP MATERIALIZED VIEW`                                 |
| `SHOW CQs`            | `SELECT * FROM timescaledb_information.continuous_aggregates` |

**Implementation sketch:**

```go
func (t *Translator) translateCreateContinuousQuery(stmt *influxql.CreateContinuousQueryStatement) ([]string, error) {
    viewName := stmt.Name
    schema := stmt.Database

    // 1. Create continuous aggregate view
    createView := fmt.Sprintf(
        `CREATE MATERIALIZED VIEW %s.%s WITH (timescaledb.continuous) AS %s`,
        pgx.Identifier{schema}.Sanitize(),
        pgx.Identifier{viewName}.Sanitize(),
        translateSelectForContinuousAggregate(stmt.Source),
    )

    // 2. Determine schedule interval from RESAMPLE EVERY or GROUP BY time()
    scheduleInterval := stmt.ResampleEvery
    if scheduleInterval == 0 {
        scheduleInterval = stmt.Source.GroupByInterval()
    }

    // 3. Determine start offset from RESAMPLE FOR
    startOffset := stmt.ResampleFor
    if startOffset == 0 {
        startOffset = scheduleInterval * 2
    }

    // 4. Add refresh policy
    addPolicy := fmt.Sprintf(
        `SELECT add_continuous_aggregate_policy('%s.%s', start_offset => INTERVAL '%s', end_offset => INTERVAL '1 minute', schedule_interval => INTERVAL '%s')`,
        schema, viewName, formatDuration(startOffset), formatDuration(scheduleInterval),
    )

    return []string{createView, addPolicy}, nil
}
```

**Advantages:**
- Native TimescaleDB feature optimised for time-series
- Incremental refresh (only processes new data)
- Real-time queries (combines materialised + recent raw data)
- Built-in job scheduling (no external dependencies)
- ~60% storage reduction compared to raw data
- Can stack aggregates on top of each other

**Limitations:**
- Requires TimescaleDB (already a dependency)
- Cannot use `ORDER BY` or `DISTINCT` in the aggregate view
- Must include `time_bucket()` on the time column
- Multi-step DDL (CREATE VIEW + ADD POLICY) needs special query handler support

### Option B: pg_cron Scheduled Queries

Store the CQ as a scheduled `INSERT INTO ... SELECT ...` job using pg_cron.

**Concept mapping:**

| InfluxDB CQ           | pg_cron                                          |
|-----------------------|--------------------------------------------------|
| `RESAMPLE EVERY 1h`   | `'0 * * * *'` cron expression                   |
| `RESAMPLE FOR 2h`     | `WHERE time >= now() - INTERVAL '2 hours'`       |
| `CREATE CQ`           | `cron.schedule_in_database()`                    |
| `DROP CQ`             | `cron.unschedule(jobid)`                         |
| `SHOW CQs`            | `SELECT * FROM cron.job`                         |

**Advantages:**
- Simpler to implement
- Works without TimescaleDB
- Full SQL flexibility (any query structure)

**Limitations:**
- Requires pg_cron extension (external dependency)
- No incremental refresh or real-time overlay
- Harder to align with InfluxDB's preset time boundaries (Unix epoch 0)
- No built-in duplicate handling if jobs overlap

### Option C: Background Go Worker (Most Control)

Store CQ definitions in a metadata table and implement scheduling in the Timeflux Go process.

**Metadata table:**
```sql
CREATE TABLE _timeflux_continuous_queries (
    name TEXT NOT NULL,
    database TEXT NOT NULL,
    query TEXT NOT NULL,          -- original InfluxQL
    resample_every INTERVAL,
    resample_for INTERVAL,
    last_run TIMESTAMPTZ,
    enabled BOOLEAN DEFAULT true,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (database, name)
);
```

**Advantages:**
- Full control — can replicate InfluxDB's preset time boundary behaviour exactly
- No external dependencies (pg_cron, etc.)
- Easier to add InfluxDB-specific features (offset, backfilling helpers)
- Custom retry and error handling

**Limitations:**
- Most complex to implement
- Worker must survive crashes and resume from `last_run`
- Needs monitoring and alerting

---

## Implementation Plan for Timeflux

### Phase 1 — SHOW / DROP (low effort, high compatibility value)

1. Add new `QueryType` constants in `query/translator.go`:
   ```go
   QueryTypeCreateContinuousQuery QueryType = "create_continuous_query"
   QueryTypeDropContinuousQuery   QueryType = "drop_continuous_query"
   QueryTypeShowContinuousQueries QueryType = "show_continuous_queries"
   ```

2. Add case handlers in `TranslateWithType()`:
   ```go
   case *influxql.CreateContinuousQueryStatement:
       sql, err := t.translateCreateContinuousQuery(s)
       return sql, QueryTypeCreateContinuousQuery, err
   case *influxql.DropContinuousQueryStatement:
       sql, err := t.translateDropContinuousQuery(s)
       return sql, QueryTypeDropContinuousQuery, err
   case *influxql.ShowContinuousQueriesStatement:
       sql, err := t.translateShowContinuousQueries(s)
       return sql, QueryTypeShowContinuousQueries, err
   ```

3. `SHOW CONTINUOUS QUERIES` — query a metadata table, return in InfluxDB series format.

4. `DROP CONTINUOUS QUERY` — drop the TimescaleDB continuous aggregate view and remove from metadata table.

### Phase 2 — CREATE with TimescaleDB Continuous Aggregates

1. Parse and validate the `SelectStatement` inside the CQ.
2. Translate the inner SELECT to use `time_bucket()` instead of `GROUP BY time()`.
3. Execute multi-step DDL: `CREATE MATERIALIZED VIEW ... WITH (timescaledb.continuous)` followed by `add_continuous_aggregate_policy()`.
4. Store original InfluxQL in metadata table for `SHOW CONTINUOUS QUERIES` results.
5. Protect metadata table from HTTP query access (same as auth tables).

### Phase 3 — Optional Enhancements

- Support time offset in `GROUP BY time(interval, offset)`
- Manual refresh endpoint via `CALL refresh_continuous_aggregate()`
- Backfilling helper command
- CQ execution metrics and monitoring
- Cross-database CQ support with permission checks

---

## Edge Cases and Limitations

| Edge Case | Notes |
|-----------|-------|
| **Preset time boundaries** | InfluxDB aligns buckets to Unix epoch 0. TimescaleDB `time_bucket()` does the same by default, so alignment should match. |
| **Tag preservation** | Default `INTO` converts tags to fields. `GROUP BY *, tag1` is needed to preserve them. Must track tag vs field metadata for destination measurement. |
| **No backfilling** | CQs only process data written after creation. Backfilling requires manual `SELECT ... INTO` with explicit time range. |
| **Cross-database writes** | CQ can write to a different database/RP. Requires read on source and write on destination — permissions must be checked. |
| **FOR duration validation** | `FOR` must be >= `GROUP BY time()` interval. InfluxQL parser validates this; must replicate before creating backend resources. |
| **Cannot alter CQs** | Must DROP and re-CREATE to modify. This matches InfluxDB behaviour — keep the same limitation. |
| **Must have aggregate function** | `SELECT field` without a function is not valid. Validate before translation. |
| **Must have GROUP BY time()** | Required by InfluxDB; maps cleanly to `time_bucket()`. Validate before translation. |
| **TimescaleDB DST bug** | TimescaleDB 2.22.0 has a known bug with DST transitions for sub-hour continuous aggregates. Test carefully with time zones. |

---

## Required Privileges

| Statement | InfluxDB Privilege |
|-----------|--------------------|
| `CREATE CONTINUOUS QUERY` | ReadPrivilege on source DB, WritePrivilege on destination DB |
| `DROP CONTINUOUS QUERY` | WritePrivilege on the database |
| `SHOW CONTINUOUS QUERIES` | ReadPrivilege (no specific database required) |

---

## References

- [InfluxDB v1.8 Continuous Queries documentation](https://docs.influxdata.com/influxdb/v1.8/query_language/continuous_queries/)
- [TimescaleDB Continuous Aggregates](https://docs.timescale.com/use-timescale/latest/continuous-aggregates/)
- [TimescaleDB add_continuous_aggregate_policy()](https://docs.timescale.com/api/latest/continuous-aggregates/add_continuous_aggregate_policy/)
- `vendor/github.com/influxdata/influxql/ast.go` — `CreateContinuousQueryStatement`, `DropContinuousQueryStatement`, `ShowContinuousQueriesStatement`
