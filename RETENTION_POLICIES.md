# Retention Policies - Investigation and Implementation Notes

This document covers InfluxDB v1 retention policies (RPs) and how they could be implemented in Timeflux on top of TimescaleDB/PostgreSQL.

## What Are Retention Policies?

A retention policy is a database-level configuration that defines:
- **DURATION** — how long data is kept before automatic deletion
- **REPLICATION** — number of data copies (OSS: always 1, Enterprise only)
- **SHARD DURATION** — time range covered by each shard group (affects deletion granularity)
- **DEFAULT** — whether this RP is used when none is explicitly specified

Key characteristics:
- Scoped to a single database; one database can have multiple RPs
- Only one RP per database can be marked `DEFAULT`
- The same measurement name can exist in multiple RPs but as separate data stores
- Data is deleted in shard-group-sized chunks (efficient bulk delete, not row-by-row)
- InfluxDB enforces retention every 30 minutes (configurable); data can exist slightly beyond its duration
- Every database is created with an `autogen` RP (infinite duration, DEFAULT)

---

## InfluxQL Syntax

### CREATE RETENTION POLICY

```sql
CREATE RETENTION POLICY <retention_policy_name>
  ON <database_name>
  DURATION <duration>
  REPLICATION <n>
  [SHARD DURATION <duration>]
  [DEFAULT]
```

Parameters:
- `DURATION` — how long data is kept. Format: `<number><unit>` where unit = `m`, `h`, `d`, `w`. Use `INF` or `0s` for infinite.
- `REPLICATION` — integer >= 1. OSS ignores values > 1 but stores them.
- `SHARD DURATION` — optional. If omitted or `0s`, auto-calculated (see table below).
- `DEFAULT` — optional. Makes this the default RP for the database.

Auto-calculated shard duration:

| Retention Duration           | Auto Shard Duration |
|------------------------------|---------------------|
| <= 1 day                     | 6 hours             |
| > 1 day and <= 7 days        | 1 day               |
| > 7 days and <= 3 months     | 7 days              |
| > 3 months and < INF         | 52 weeks            |
| INF (infinite)               | 7 days              |

Example:
```sql
CREATE RETENTION POLICY "30d"
  ON "telegraf"
  DURATION 30d
  REPLICATION 1
  SHARD DURATION 1d
  DEFAULT
```

### ALTER RETENTION POLICY

```sql
ALTER RETENTION POLICY <retention_policy_name>
  ON <database_name>
  [DURATION <duration>]
  [REPLICATION <n>]
  [SHARD DURATION <duration>]
  [DEFAULT]
```

- Must specify at least one attribute
- Setting `DEFAULT` automatically unsets the previous default RP
- Existing data is not moved when DEFAULT changes

### DROP RETENTION POLICY

```sql
DROP RETENTION POLICY <retention_policy_name> ON <database_name>
```

- Permanently deletes all data in the RP
- Cannot drop the last RP on a database

### SHOW RETENTION POLICIES

```sql
SHOW RETENTION POLICIES [ON <database_name>]
```

Returns columns: `name`, `duration`, `shardGroupDuration`, `replicaN`, `default`

Example output:
```
name     duration   shardGroupDuration  replicaN  default
----     --------   ------------------  --------  -------
autogen  0s         168h0m0s            1         true
30d      720h0m0s   24h0m0s             1         false
1y       8760h0m0s  168h0m0s            1         false
```

---

## InfluxQL AST Node Types

From `vendor/github.com/influxdata/influxql/ast.go`:

### CreateRetentionPolicyStatement

```go
type CreateRetentionPolicyStatement struct {
    Name               string
    Database           string
    Duration           time.Duration    // 0 means INF
    Replication        int
    Default            bool
    ShardGroupDuration time.Duration    // 0 means auto-calculate
    FutureWriteLimit   time.Duration    // reject writes too far in future
    PastWriteLimit     time.Duration    // reject writes too far in past
}
```

### AlterRetentionPolicyStatement

```go
type AlterRetentionPolicyStatement struct {
    Name               string
    Database           string
    Duration           *time.Duration   // nil = unchanged
    Replication        *int             // nil = unchanged
    Default            bool
    ShardGroupDuration *time.Duration   // nil = unchanged
    FutureWriteLimit   *time.Duration
    PastWriteLimit     *time.Duration
}
```

Note: all modifiable fields are pointers to allow partial updates.

### DropRetentionPolicyStatement

```go
type DropRetentionPolicyStatement struct {
    Name     string
    Database string
}
```

### ShowRetentionPoliciesStatement

```go
type ShowRetentionPoliciesStatement struct {
    Database string   // empty = use default/current database
}
```

---

## Relationship Between RPs, Databases, and Measurements

**Database ↔ RP**: One-to-Many. A database has multiple RPs; each RP belongs to one database.

**RP ↔ Measurement**: The same measurement name can exist in multiple RPs, but they hold completely separate data.

Example:
```
Database: "telegraf"
  ├── RP "autogen" (DEFAULT, INF duration)
  │   ├── Measurement "cpu"   ← all historical data
  │   └── Measurement "mem"
  └── RP "30d" (30-day retention)
      ├── Measurement "cpu"   ← last 30 days only (different table!)
      └── Measurement "mem"
```

Writing to a specific RP:
```bash
# Default RP (autogen)
curl -XPOST 'http://localhost:8086/write?db=telegraf' \
  --data-binary 'cpu,host=server1 value=85.3'

# Specific RP
curl -XPOST 'http://localhost:8086/write?db=telegraf&rp=30d' \
  --data-binary 'cpu,host=server1 value=85.3'
```

Querying a specific RP:
```sql
-- Default RP
SELECT mean(value) FROM cpu WHERE time > now() - 1h

-- Specific RP
SELECT mean(value) FROM "30d"."cpu" WHERE time > now() - 1h
```

---

## TimescaleDB/PostgreSQL Implementation Strategies

### Option A: TimescaleDB Native Retention Policies (Recommended)

TimescaleDB drops entire **chunks** (analogous to InfluxDB shard groups) using `add_retention_policy()`. This is the most efficient approach and closely mirrors InfluxDB's shard-group-based deletion.

**Table naming convention:**

To support multiple RPs per database, include the RP name in the table name:
```
{schema}.{rp_name}__{measurement}

Examples:
  telegraf.autogen__cpu
  telegraf.30d__cpu
  telegraf.1y__cpu
```

**Metadata table:**
```sql
CREATE TABLE _timeflux_retention_policies (
    database TEXT NOT NULL,
    rp_name TEXT NOT NULL,
    duration INTERVAL NOT NULL,        -- 0 means INF
    replication INTEGER NOT NULL DEFAULT 1,
    shard_duration INTERVAL,           -- NULL means auto-calculated
    is_default BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    PRIMARY KEY (database, rp_name)
);

CREATE INDEX idx_rp_default ON _timeflux_retention_policies (database, is_default)
WHERE is_default = true;
```

**Creating an RP:**
```sql
-- 1. Store metadata
INSERT INTO _timeflux_retention_policies (database, rp_name, duration, replication, shard_duration, is_default)
VALUES ('telegraf', '30d', INTERVAL '30 days', 1, INTERVAL '1 day', true)
ON CONFLICT (database, rp_name) DO UPDATE
SET duration = EXCLUDED.duration,
    replication = EXCLUDED.replication,
    shard_duration = EXCLUDED.shard_duration,
    is_default = EXCLUDED.is_default;

-- 2. If DEFAULT, unset previous default
UPDATE _timeflux_retention_policies
SET is_default = false
WHERE database = 'telegraf' AND rp_name != '30d';

-- 3. For each existing or newly written measurement, create the hypertable
CREATE TABLE telegraf.autogen__cpu (
    time TIMESTAMPTZ NOT NULL,
    host TEXT,
    value DOUBLE PRECISION
);

SELECT create_hypertable('telegraf.autogen__cpu', 'time',
    chunk_time_interval => INTERVAL '1 day');

-- 4. Add TimescaleDB retention policy (skip if duration = INF/0)
SELECT add_retention_policy(
    'telegraf."30d__cpu"',
    drop_after => INTERVAL '30 days',
    schedule_interval => INTERVAL '1 hour'
);
```

**Altering an RP:**
```sql
-- Update metadata
UPDATE _timeflux_retention_policies
SET duration = INTERVAL '90 days', is_default = true
WHERE database = 'telegraf' AND rp_name = '30d';

-- Update TimescaleDB retention policy on each measurement table
SELECT remove_retention_policy('telegraf."30d__cpu"');
SELECT add_retention_policy('telegraf."30d__cpu"', drop_after => INTERVAL '90 days');
```

**Dropping an RP:**
```sql
-- Drop all hypertables for this RP
DROP TABLE IF EXISTS telegraf."30d__cpu";
DROP TABLE IF EXISTS telegraf."30d__mem";

-- Remove metadata
DELETE FROM _timeflux_retention_policies
WHERE database = 'telegraf' AND rp_name = '30d';
```

**SHOW RETENTION POLICIES:**
```sql
SELECT
    rp_name AS name,
    CASE WHEN duration = '0'::INTERVAL THEN '0s' ELSE duration::TEXT END AS duration,
    COALESCE(shard_duration, '168h'::INTERVAL)::TEXT AS "shardGroupDuration",
    replication AS "replicaN",
    is_default AS default
FROM _timeflux_retention_policies
WHERE database = $1
ORDER BY rp_name;
```

**Chunk time interval mapping (shard duration):**
```go
func shardDurationToChunkInterval(rpDuration, shardDuration time.Duration) time.Duration {
    if shardDuration != 0 {
        return shardDuration
    }
    // Auto-calculate per InfluxDB rules
    switch {
    case rpDuration == 0:             // INF
        return 7 * 24 * time.Hour     // 7 days
    case rpDuration <= 24*time.Hour:
        return 6 * time.Hour
    case rpDuration <= 7*24*time.Hour:
        return 24 * time.Hour
    case rpDuration <= 90*24*time.Hour:
        return 7 * 24 * time.Hour
    default:
        return 52 * 7 * 24 * time.Hour  // 52 weeks
    }
}
```

**Write path integration:**
```go
// Determine which table to write to
rp := req.URL.Query().Get("rp")
if rp == "" {
    rp = getDefaultRP(database)  // query metadata table
}
tableName := fmt.Sprintf("%s__%s", rp, measurement)

// Ensure table exists with correct retention policy
ensureHypertableWithRetention(database, rp, measurement)

// Write using existing COPY logic
writePoints(database, tableName, points)
```

**Query path integration:**
```go
// FROM "30d"."cpu"  →  rp="30d", measurement="cpu"
// FROM cpu          →  rp=default, measurement="cpu"
rp, measurement := parseRPAndMeasurement(fromClause, defaultRP)
tableName := fmt.Sprintf("%s__%s", rp, measurement)
```

### Option B: PostgreSQL Partitioning + Partition Dropping

Create range-partitioned tables by time, and drop old partitions on a schedule.

**Pros:** No TimescaleDB required; partition-wise pruning for queries.
**Cons:** Must create partitions in advance; requires external scheduler (pg_cron); complex maintenance.

### Option C: pg_cron + DELETE

Schedule periodic `DELETE FROM table WHERE time < NOW() - interval` operations.

**Pros:** Simple; works on any PostgreSQL.
**Cons:** Row-by-row deletion is very slow on large datasets; causes table bloat requiring `VACUUM`; not suitable for production time-series workloads. **Not recommended.**

---

## Implementation Plan for Timeflux

### Phase 1 — Metadata and SHOW/DROP (no data path changes)

1. Create `_timeflux_retention_policies` metadata table on startup.
2. Add `QueryType` constants: `QueryTypeCreateRetentionPolicy`, `QueryTypeAlterRetentionPolicy`, `QueryTypeDropRetentionPolicy`, `QueryTypeShowRetentionPolicies`.
3. Add case handlers in `TranslateWithType()` for all four statement types.
4. `SHOW RETENTION POLICIES` — query metadata table, return InfluxDB series format.
5. `DROP RETENTION POLICY` — drop associated hypertables and remove metadata.
6. Auto-create `autogen` RP (INF, DEFAULT) when a database is created.

### Phase 2 — CREATE / ALTER with write path integration

1. `CREATE RETENTION POLICY` — insert metadata, calculate shard duration, add `add_retention_policy()` on new measurements as they are created.
2. `ALTER RETENTION POLICY` — update metadata, update or replace TimescaleDB retention policies.
3. Update write handler to read `rp` query parameter and route to correct table name.
4. Update schema manager to include RP in table naming convention.
5. Update query translator to parse `"rp"."measurement"` fully qualified references.

### Phase 3 — Optional Enhancements

- `FUTURE LIMIT` / `PAST LIMIT` enforcement in write handler
- Per-RP metrics (data volume, row count) exposed via `/metrics`
- Admin endpoint for manual shard expiry (`drop_chunks`)
- Backfilling support via explicit time-range queries

---

## Edge Cases and Limitations

| Edge Case | Notes |
|-----------|-------|
| **autogen RP** | Must be auto-created with infinite duration and set as DEFAULT when a database is created. Cannot be dropped if it is the only RP. |
| **DEFAULT flag exclusivity** | Only one RP per database can be DEFAULT. Setting a new default must unset the previous one atomically. |
| **Shard group granularity** | TimescaleDB drops entire chunks, not individual rows — exactly matches InfluxDB semantics. Data can survive up to `DURATION + chunk_interval` before being dropped. |
| **Shard duration vs duration mismatch** | If `SHARD DURATION > DURATION`, data will be kept longer than expected (entire chunks must age out). Best practice: shard duration <= 50% of retention duration. |
| **Replication factor** | TimescaleDB is single-node (or has its own HA). Store the value in metadata for compatibility but ignore it in implementation, same as InfluxDB OSS. |
| **Changing DEFAULT** | Does not move existing data; only affects where future writes (without explicit RP) are routed. |
| **Duration format parsing** | `INF` and `0s` both mean infinite. Store as `'0'::INTERVAL` in PostgreSQL. Skip `add_retention_policy()` for infinite-duration RPs. |
| **Cross-database writes from CQs** | When a CQ writes to a different database/RP, the target RP must exist. The write handler should create the destination hypertable if needed. |
| **Dropping last RP** | Must reject `DROP RETENTION POLICY` if it would leave the database with no RPs. |
| **Table naming collisions** | If RP name or measurement name contains `__`, the naming convention `{rp}__{measurement}` could collide. Consider using a metadata lookup table instead of encoding RP in the table name. |
| **Schema cache** | The schema manager's in-memory cache must be keyed by `(database, rp, measurement)` to correctly handle multiple RPs with the same measurement name. |

---

## Required Privileges

| Statement | InfluxDB Privilege |
|-----------|--------------------|
| `CREATE RETENTION POLICY` | Admin |
| `ALTER RETENTION POLICY` | Admin |
| `DROP RETENTION POLICY` | WritePrivilege on the database |
| `SHOW RETENTION POLICIES` | ReadPrivilege on the database |

---

## Impact on Existing Timeflux Architecture

Implementing retention policies requires changes across multiple components:

| Component | Change Required |
|-----------|-----------------|
| `schema/manager.go` | Include RP in table name; look up default RP; per-RP schema cache |
| `write/handler.go` | Parse `rp` query parameter; route to correct table |
| `query/translator.go` | Parse `"rp"."measurement"` syntax; translate to correct table name |
| `query/handler.go` | Handle multi-step DDL responses for CREATE/ALTER RP |
| `main.go` | Create `_timeflux_retention_policies` table on startup; create `autogen` RP when database is created |
| `config/config.go` | Optional: configurable default RP name (defaults to `autogen`) |

---

## References

- [InfluxDB v1.8 Retention Policy Management](https://docs.influxdata.com/influxdb/v1.8/query_language/manage-database/#retention-policy-management)
- [TimescaleDB Retention Policies](https://docs.timescale.com/use-timescale/latest/data-retention/)
- [TimescaleDB add_retention_policy()](https://docs.timescale.com/api/latest/data-retention/add_retention_policy/)
- [TimescaleDB drop_chunks()](https://docs.timescale.com/api/latest/hypertable/drop_chunks/)
- `vendor/github.com/influxdata/influxql/ast.go` — `CreateRetentionPolicyStatement`, `AlterRetentionPolicyStatement`, `DropRetentionPolicyStatement`, `ShowRetentionPoliciesStatement`
