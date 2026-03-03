# InfluxQL Function Support in Timeflux

This document provides a comprehensive mapping of InfluxQL functions to their PostgreSQL/TimescaleDB analogs, organized by implementation difficulty.

## Currently Supported Functions

The following functions are implemented in `query/translator.go`.

### Original (pre-Phase 1)

- **MEAN()** → `AVG()`
- **COUNT()** → `COUNT()`
- **SUM()** → `SUM()`
- **MAX()** → `MAX()`
- **MIN()** → `MIN()`
- **FIRST()** → `FIRST(value, time)` (TimescaleDB function)
- **LAST()** → `LAST(value, time)` (TimescaleDB function)
- **PERCENTILE()** → `percentile_cont() WITHIN GROUP (ORDER BY ...)`
- **NOW()** → `NOW()`

### Phase 1 (implemented)

**Aggregations:**
- **STDDEV()** → `STDDEV()`
- **MEDIAN()** → `percentile_cont(0.5) WITHIN GROUP (ORDER BY field)`
- **SPREAD()** → `(MAX(field) - MIN(field))`
- **MODE()** → `MODE() WITHIN GROUP (ORDER BY field)`

**Math:**
- **ABS()** → `ABS()`
- **CEIL()** → `CEIL()`
- **FLOOR()** → `FLOOR()`
- **ROUND()** → `ROUND()`
- **SQRT()** → `SQRT()`
- **POW(field, exp)** → `POWER(field, exp)`
- **EXP()** → `EXP()`
- **LN()** → `LN()`
- **LOG(field, base)** → `LOG(base::numeric, field::numeric)` ⚠️ arg order is swapped AND both args must be cast to `numeric`
- **LOG2()** → `LOG(2, field)`
- **LOG10()** → `LOG(10, field)`

**Trigonometry:**
- **SIN()** → `SIN()`
- **COS()** → `COS()`
- **TAN()** → `TAN()`
- **ASIN()** → `ASIN()`
- **ACOS()** → `ACOS()`
- **ATAN()** → `ATAN()`
- **ATAN2(y, x)** → `ATAN2(y, x)` (same arg order)

## Function Mapping by Implementation Difficulty

### Phase 1: Direct Mapping ✅ COMPLETE

These functions have direct PostgreSQL equivalents. All implemented.

| InfluxQL Function | PostgreSQL Analog | Notes | Status |
|-------------------|-------------------|-------|--------|
| **STDDEV(field)** | `STDDEV(field)` | Standard deviation | ✅ **DONE** |
| **MEDIAN(field)** | `percentile_cont(0.5) WITHIN GROUP (ORDER BY field)` | 50th percentile | ✅ **DONE** |
| **ABS(field)** | `ABS(field)` | Absolute value | ✅ **DONE** |
| **CEIL(field)** | `CEIL(field)` | Ceiling | ✅ **DONE** |
| **FLOOR(field)** | `FLOOR(field)` | Floor | ✅ **DONE** |
| **ROUND(field)** | `ROUND(field)` | Round to nearest integer | ✅ **DONE** |
| **SQRT(field)** | `SQRT(field)` | Square root | ✅ **DONE** |
| **POW(field, exponent)** | `POWER(field, exponent)` | Power/exponentiation | ✅ **DONE** |
| **EXP(field)** | `EXP(field)` | Exponential (e^x) | ✅ **DONE** |
| **LN(field)** | `LN(field)` | Natural logarithm | ✅ **DONE** |
| **LOG(field, base)** | `LOG(base, field)` | Logarithm with base (**note arg order swap from InfluxQL**) | ✅ **DONE** |
| **LOG2(field)** | `LOG(2, field)` | Base-2 logarithm | ✅ **DONE** |
| **LOG10(field)** | `LOG(10, field)` | Base-10 logarithm | ✅ **DONE** |
| **SIN(field)** | `SIN(field)` | Sine (radians) | ✅ **DONE** |
| **COS(field)** | `COS(field)` | Cosine (radians) | ✅ **DONE** |
| **TAN(field)** | `TAN(field)` | Tangent (radians) | ✅ **DONE** |
| **ASIN(field)** | `ASIN(field)` | Arcsine; input must be in [-1, 1] | ✅ **DONE** |
| **ACOS(field)** | `ACOS(field)` | Arccosine; input must be in [-1, 1] | ✅ **DONE** |
| **ATAN(field)** | `ATAN(field)` | Arctangent | ✅ **DONE** |
| **ATAN2(y, x)** | `ATAN2(y, x)` | Two-argument arctangent (same arg order) | ✅ **DONE** |

### Phase 2: Simple Aggregations (Easy - 5-15 lines of code)

These require basic window functions or aggregation logic.

| InfluxQL Function | PostgreSQL Analog | Notes | Status |
|-------------------|-------------------|-------|--------|
| **SPREAD(field)** | `MAX(field) - MIN(field)` | Range of values | ✅ **DONE** (Phase 1) |
| **MODE(field)** | `MODE() WITHIN GROUP (ORDER BY field)` | Most frequent value | ✅ **DONE** (Phase 1) |
| **DISTINCT(field)** | `COUNT(DISTINCT field)` | Count distinct values; InfluxQL allows `SELECT DISTINCT field` syntax | 🟨 **NEXT** |
| **TOP(field, N)** | `(SELECT field FROM tbl ORDER BY field DESC LIMIT N)` | Top N values (needs subquery) | 🟨 **MEDIUM** |
| **BOTTOM(field, N)** | `(SELECT field FROM tbl ORDER BY field ASC LIMIT N)` | Bottom N values (needs subquery) | 🟨 **MEDIUM** |
| **SAMPLE(field, N)** | `(SELECT field FROM tbl ORDER BY RANDOM() LIMIT N)` | Random N samples | 🟨 **MEDIUM** |

### Phase 3: Time-Based Transformations (Medium - 15-40 lines of code)

These require window functions with proper partitioning over time.

| InfluxQL Function | PostgreSQL Analog | Notes | Status |
|-------------------|-------------------|-------|--------|
| **CUMULATIVE_SUM(field)** | `SUM(field) OVER (ORDER BY time ROWS BETWEEN UNBOUNDED PRECEDING AND CURRENT ROW)` | Running total | 🟨 **MEDIUM** |
| **MOVING_AVERAGE(field, N)** | `AVG(field) OVER (ORDER BY time ROWS BETWEEN N-1 PRECEDING AND CURRENT ROW)` | N-point moving average | 🟨 **MEDIUM** |
| **DIFFERENCE(field)** | `field - LAG(field, 1) OVER (ORDER BY time)` | Difference between consecutive values | 🟨 **MEDIUM** |
| **NON_NEGATIVE_DIFFERENCE(field)** | `GREATEST(field - LAG(field, 1) OVER (ORDER BY time), 0)` | Non-negative difference | 🟨 **MEDIUM** |
| **DERIVATIVE(field[, unit])** | `(field - LAG(field) OVER (ORDER BY time)) / EXTRACT(EPOCH FROM (time - LAG(time) OVER (ORDER BY time)))` | Rate of change per unit time | 🟨 **MEDIUM** |
| **NON_NEGATIVE_DERIVATIVE(field[, unit])** | `GREATEST(derivative, 0)` | Non-negative derivative | 🟨 **MEDIUM** |
| **ELAPSED(field[, unit])** | `EXTRACT(EPOCH FROM (time - LAG(time) OVER (ORDER BY time))) * unit_multiplier` | Time elapsed between points | 🟨 **MEDIUM** |

### Phase 4: Advanced Aggregations (Medium-Hard - 30-60 lines of code)

These require more complex calculations or custom logic.

| InfluxQL Function | PostgreSQL Analog | Notes | Status |
|-------------------|-------------------|-------|--------|
| **INTEGRAL(field[, unit])** | `SUM((field + LAG(field) OVER (ORDER BY time)) / 2 * time_diff)` | Trapezoidal rule integration | 🟧 **MEDIUM-HARD** |
| **HISTOGRAM(field, min, max, count)** | `WIDTH_BUCKET(field, min, max, count)` with GROUP BY | Histogram bucketing | 🟧 **MEDIUM-HARD** |
| **EXPONENTIAL_MOVING_AVERAGE(field, N)** | Custom function using recursive CTE or plpgsql | EMA calculation | 🟥 **HARD** |
| **DOUBLE_EXPONENTIAL_MOVING_AVERAGE(field, N)** | Custom function (DEMA = 2*EMA - EMA(EMA)) | Double smoothing | 🟥 **HARD** |
| **TRIPLE_EXPONENTIAL_MOVING_AVERAGE(field, N)** | Custom function (TEMA calculation) | Triple smoothing | 🟥 **HARD** |
| **TRIPLE_EXPONENTIAL_DERIVATIVE(field, N)** | Custom function based on TEMA | TRIX indicator | 🟥 **HARD** |

### Phase 5: Technical Analysis Functions (Hard - Requires Custom Functions)

These require complex stateful calculations and may need PostgreSQL stored procedures or CTEs.

| InfluxQL Function | PostgreSQL Analog | Notes | Status |
|-------------------|-------------------|-------|--------|
| **RELATIVE_STRENGTH_INDEX(field, N)** | Custom function | RSI = 100 - (100 / (1 + RS)) where RS = avg_gain/avg_loss | 🟥 **HARD** |
| **CHANDE_MOMENTUM_OSCILLATOR(field, N)** | Custom function | CMO = ((sum_ups - sum_downs) / (sum_ups + sum_downs)) * 100 | 🟥 **HARD** |
| **KAUFMANS_EFFICIENCY_RATIO(field, N)** | Custom function | ER = direction/volatility over N periods | 🟥 **HARD** |
| **KAUFMANS_ADAPTIVE_MOVING_AVERAGE(field, N)** | Custom function | KAMA uses efficiency ratio for adaptive smoothing | 🟥 **HARD** |
| **HOLT_WINTERS(field, N, m)** | **NO DIRECT ANALOG** | Forecasting with seasonal decomposition | 🟥 **VERY HARD** |

---

## Functions with NO DIRECT ANALOG

These functions require custom PostgreSQL implementations, likely as stored procedures or complex CTEs.

### HOLT_WINTERS(field, N, season_length)

**InfluxQL Behavior:** Time series forecasting using triple exponential smoothing with seasonal adjustment.

**PostgreSQL Translation Strategy:**
```sql
-- Requires a PL/pgSQL function or complex recursive CTE
-- Components needed:
-- 1. Level smoothing (alpha)
-- 2. Trend smoothing (beta)
-- 3. Seasonal smoothing (gamma)

CREATE FUNCTION holt_winters_forecast(
    data DOUBLE PRECISION[],
    timestamps TIMESTAMPTZ[],
    alpha DOUBLE PRECISION DEFAULT 0.5,
    beta DOUBLE PRECISION DEFAULT 0.5,
    gamma DOUBLE PRECISION DEFAULT 0.5,
    season_length INTEGER DEFAULT 24,
    forecast_periods INTEGER DEFAULT 10
) RETURNS TABLE(time TIMESTAMPTZ, forecast DOUBLE PRECISION)
AS $$
-- Implementation would include:
-- - Initial seasonal components calculation
-- - Level, trend, and seasonal updates
-- - Forecast generation
$$ LANGUAGE plpgsql;
```

**Implementation Difficulty:** 🟥 **VERY HARD** (200+ lines of code)

**Alternative Approach:**
- Use Python or R via PL/Python or PL/R extensions
- Pre-compute in application layer and store results
- Use TimescaleDB continuous aggregates with custom forecasting

---

## Translation Strategies for Complex Functions

### Strategy 1: Window Functions with LAG/LEAD

Many time-series transformations can be implemented using window functions:

```sql
-- DERIVATIVE example
SELECT
    time,
    field,
    (field - LAG(field) OVER (ORDER BY time)) /
    EXTRACT(EPOCH FROM (time - LAG(time) OVER (ORDER BY time))) AS derivative
FROM measurement
WHERE time > NOW() - INTERVAL '1 hour'
```

### Strategy 2: Recursive CTEs for Moving Calculations

Exponential moving averages and similar functions:

```sql
-- EXPONENTIAL_MOVING_AVERAGE(field, 10)
WITH RECURSIVE ema AS (
    SELECT
        time,
        field,
        field AS ema_value,
        ROW_NUMBER() OVER (ORDER BY time) AS rn
    FROM measurement
    WHERE rn = 1

    UNION ALL

    SELECT
        m.time,
        m.field,
        (m.field * (2.0 / (10 + 1))) + (e.ema_value * (1 - (2.0 / (10 + 1)))),
        m.rn
    FROM measurement m
    JOIN ema e ON m.rn = e.rn + 1
)
SELECT time, ema_value FROM ema;
```

### Strategy 3: Custom PL/pgSQL Functions

For complex technical indicators:

```sql
-- RELATIVE_STRENGTH_INDEX(field, 14)
CREATE FUNCTION rsi(field_name TEXT, periods INTEGER DEFAULT 14)
RETURNS TABLE(time TIMESTAMPTZ, rsi DOUBLE PRECISION)
AS $$
BEGIN
    RETURN QUERY
    WITH changes AS (
        SELECT
            time,
            field - LAG(field) OVER (ORDER BY time) AS change
        FROM measurement
    ),
    gains_losses AS (
        SELECT
            time,
            CASE WHEN change > 0 THEN change ELSE 0 END AS gain,
            CASE WHEN change < 0 THEN ABS(change) ELSE 0 END AS loss
        FROM changes
    ),
    avg_gains_losses AS (
        SELECT
            time,
            AVG(gain) OVER (ORDER BY time ROWS BETWEEN periods-1 PRECEDING AND CURRENT ROW) AS avg_gain,
            AVG(loss) OVER (ORDER BY time ROWS BETWEEN periods-1 PRECEDING AND CURRENT ROW) AS avg_loss
        FROM gains_losses
    )
    SELECT
        time,
        100 - (100 / (1 + (avg_gain / NULLIF(avg_loss, 0)))) AS rsi
    FROM avg_gains_losses;
END;
$$ LANGUAGE plpgsql;
```

### Strategy 4: Application-Layer Computation

For extremely complex functions like HOLT_WINTERS:
- Compute in Go application layer
- Use dedicated time-series analysis libraries
- Cache results in TimescaleDB
- Update on-demand or via scheduled jobs

---

## Implementation Phases

### **Phase 1: Math & Basic Aggregations** (1-2 days)
**Priority:** HIGH | **Difficulty:** LOW

Add direct 1:1 mappings for math functions and simple aggregations:
- STDDEV, MEDIAN, SPREAD, DISTINCT, MODE
- ABS, CEIL, FLOOR, ROUND, SQRT, POW, EXP
- LN, LOG, LOG2, LOG10
- SIN, COS, TAN, ASIN, ACOS, ATAN, ATAN2

**Implementation:** Add cases to `translateCall()` in `query/translator.go`

**Testing:**
```bash
# STDDEV
SELECT STDDEV(value) FROM cpu WHERE time > now() - 1h GROUP BY time(5m)

# MEDIAN
SELECT MEDIAN(value) FROM cpu WHERE time > now() - 1h

# Math functions
SELECT ABS(value), SQRT(value), LOG10(value) FROM cpu LIMIT 10
```

---

### **Phase 2: Selector Functions** (2-3 days)
**Priority:** HIGH | **Difficulty:** MEDIUM

Implement TOP, BOTTOM, SAMPLE which require subquery handling:

**Challenge:** These functions return multiple rows and may need special handling in GROUP BY context.

**Implementation Approach:**
```go
case "top":
    // SELECT DISTINCT ON approach or subquery
    field := t.translateExpr(call.Args[0])
    n := t.translateExpr(call.Args[1])
    return fmt.Sprintf("(SELECT %s ORDER BY %s DESC LIMIT %s)", field, field, n)
```

**Testing:**
```bash
# TOP
SELECT TOP(value, 5) FROM cpu

# BOTTOM
SELECT BOTTOM(value, 3), host FROM cpu GROUP BY host
```

---

### **Phase 3: Time-Based Transformations** (4-5 days)
**Priority:** MEDIUM | **Difficulty:** MEDIUM

Implement DERIVATIVE, DIFFERENCE, CUMULATIVE_SUM, MOVING_AVERAGE using window functions:

**Challenge:** Requires proper ORDER BY time and PARTITION BY for grouped queries.

**Implementation Approach:**
```go
case "derivative":
    field := t.translateExpr(call.Args[0])
    unit := "1s" // default
    if len(call.Args) > 1 {
        unit = parseUnit(call.Args[1])
    }

    return fmt.Sprintf(
        `(%s - LAG(%s) OVER (ORDER BY time)) /
         (EXTRACT(EPOCH FROM (time - LAG(time) OVER (ORDER BY time))) / %s)`,
        field, field, unitToSeconds(unit))
```

**Special Considerations:**
- Must detect if query has GROUP BY tags and add PARTITION BY
- Handle NULL values from LAG() appropriately
- ELAPSED() needs unit conversion (ns, u, ms, s, m, h)

**Testing:**
```bash
# DERIVATIVE
SELECT DERIVATIVE(value, 1s) FROM cpu WHERE time > now() - 1h

# MOVING_AVERAGE
SELECT MOVING_AVERAGE(value, 5) FROM cpu WHERE time > now() - 1h
```

---

### **Phase 4: Advanced Aggregations** (5-7 days)
**Priority:** LOW | **Difficulty:** MEDIUM-HARD

Implement INTEGRAL, HISTOGRAM, and basic EMA:

**INTEGRAL Implementation:**
```go
case "integral":
    // Trapezoidal rule: sum of (avg_height * width)
    field := t.translateExpr(call.Args[0])
    unit := parseUnitOrDefault(call.Args, 1, "1s")

    return fmt.Sprintf(`
        SUM(
            (((%s + LAG(%s) OVER (ORDER BY time)) / 2.0) *
            (EXTRACT(EPOCH FROM (time - LAG(time) OVER (ORDER BY time))) / %s))
        )`, field, field, unitToSeconds(unit))
```

**HISTOGRAM Implementation:**
```sql
-- Requires understanding of WHERE clause to get min/max
-- Or use dynamic bucketing
WIDTH_BUCKET(field, min_val, max_val, num_buckets)
```

**Testing:**
```bash
# INTEGRAL
SELECT INTEGRAL(value) FROM cpu WHERE time > now() - 1h GROUP BY time(10m)

# HISTOGRAM
SELECT HISTOGRAM(value, 0, 100, 10) FROM cpu
```

---

### **Phase 5: Technical Analysis Functions** (10-15 days)
**Priority:** LOW | **Difficulty:** HARD

Implement RSI, CHANDE_MOMENTUM_OSCILLATOR, KAUFMANS functions:

**Approach:** Create PostgreSQL stored functions in a migration or via schema manager.

**Implementation Location:**
- Create `schema/functions.sql` with PL/pgSQL implementations
- Load during schema initialization
- Reference in translator as `rsi(field, N)`

**Example RSI Function:**
```sql
CREATE OR REPLACE FUNCTION rsi(
    schema_name TEXT,
    measurement_name TEXT,
    field_name TEXT,
    periods INTEGER DEFAULT 14
) RETURNS TABLE(time TIMESTAMPTZ, rsi DOUBLE PRECISION)
AS $$
DECLARE
    query TEXT;
BEGIN
    query := format($q$
        WITH changes AS (
            SELECT
                time,
                %I - LAG(%I) OVER (ORDER BY time) AS change
            FROM %I.%I
        ),
        gains_losses AS (
            SELECT
                time,
                CASE WHEN change > 0 THEN change ELSE 0 END AS gain,
                CASE WHEN change < 0 THEN ABS(change) ELSE 0 END AS loss
            FROM changes
        ),
        avg_gl AS (
            SELECT
                time,
                AVG(gain) OVER (ORDER BY time ROWS BETWEEN %s PRECEDING AND CURRENT ROW) AS avg_gain,
                AVG(loss) OVER (ORDER BY time ROWS BETWEEN %s PRECEDING AND CURRENT ROW) AS avg_loss
            FROM gains_losses
        )
        SELECT
            time,
            100 - (100 / (1 + (avg_gain / NULLIF(avg_loss, 0)))) AS rsi
        FROM avg_gl
        WHERE avg_gain IS NOT NULL AND avg_loss IS NOT NULL
    $q$, field_name, field_name, schema_name, measurement_name, periods-1, periods-1);

    RETURN QUERY EXECUTE query;
END;
$$ LANGUAGE plpgsql;
```

**Testing:**
```bash
# RSI
SELECT RSI(value, 14) FROM cpu WHERE time > now() - 1d

# CHANDE_MOMENTUM_OSCILLATOR
SELECT CHANDE_MOMENTUM_OSCILLATOR(value, 20) FROM cpu
```

---

### **Phase 6: HOLT_WINTERS (Future/Optional)** (20+ days)
**Priority:** VERY LOW | **Difficulty:** VERY HARD

**Recommendation:** Defer or implement in application layer.

**Alternative Solutions:**
1. Use Python/R via PL/Python with statsmodels/forecast libraries
2. Implement as a microservice that queries TimescaleDB
3. Pre-compute forecasts and store as materialized views
4. Document as "not supported - use external forecasting tools"

---

## Summary Statistics

| Category | Total Functions | Implemented | Remaining Phase 2 | Phase 3-4 | Phase 5-6 |
|----------|----------------|-------------|-----------|-----------|-----------|
| **Aggregations** | 9 | 8 | 1 | 0 | 0 |
| **Selectors** | 7 | 3 | 3 | 1 | 0 |
| **Transformations** | 25 | 21 | 0 | 2 | 2 |
| **Predictors** | 1 | 0 | 0 | 0 | 1 |
| **Technical Analysis** | 8 | 0 | 0 | 0 | 8 |
| **TOTAL** | **50** | **32** | **4** | **3** | **11** |

**Coverage:** 64% implemented (up from 20% before Phase 1)

---

## Quick Reference: Currently Supported

```sql
-- Aggregations (8/9)
COUNT(*), SUM(field), AVG(field)/MEAN(field), MIN(field), MAX(field),
STDDEV(field), MEDIAN(field), SPREAD(field), MODE(field)

-- Selectors (3/7)
FIRST(field), LAST(field), PERCENTILE(field, N)

-- Math / Transformations (22/25)
NOW()
ABS(field), CEIL(field), FLOOR(field), ROUND(field), SQRT(field)
POW(field, exp), EXP(field), LN(field)
LOG(field, base), LOG2(field), LOG10(field)
SIN(field), COS(field), TAN(field)
ASIN(field), ACOS(field), ATAN(field), ATAN2(y, x)

-- Technical Analysis (0/8)
-- None yet

-- Predictors (0/1)
-- None yet
```

---

## Implementation Notes

### Adding a New Function

1. **Edit `query/translator.go`** in the `translateCall()` function
2. **Add a case** for the function name (lowercase)
3. **Return the PostgreSQL translation**
4. **Handle arguments** properly with `t.translateExpr(call.Args[i])`
5. **Add tests** to `query/translator_test.go`

### Example Template

```go
case "stddev":
    if len(call.Args) > 0 {
        return "STDDEV(" + t.translateExpr(call.Args[0]) + ")"
    }
    return "STDDEV(*)"
```

### Handling Window Functions

For functions like DERIVATIVE that need ORDER BY time:

```go
case "derivative":
    field := t.translateExpr(call.Args[0])

    // Check if we're in a GROUP BY context
    partition := ""
    if t.hasGroupByTags() {
        partition = "PARTITION BY " + t.getTagColumns() + " "
    }

    return fmt.Sprintf(
        `((%s - LAG(%s) OVER (%sORDER BY time)) /
          EXTRACT(EPOCH FROM (time - LAG(time) OVER (%sORDER BY time))))`,
        field, field, partition, partition)
```

### Testing Strategy

For each implemented function:

1. **Unit tests** in `translator_test.go` checking SQL translation
2. **Integration tests** with actual TimescaleDB queries
3. **Edge cases**: NULL values, single row, empty results
4. **GROUP BY tests**: Ensure PARTITION BY works correctly

---

## References

- [InfluxDB 1.8 Functions Docs](https://docs.influxdata.com/influxdb/v1/query_language/functions/)
- [PostgreSQL Window Functions](https://www.postgresql.org/docs/current/tutorial-window.html)
- [PostgreSQL Aggregate Functions](https://www.postgresql.org/docs/current/functions-aggregate.html)
- [TimescaleDB Hyperfunctions](https://docs.timescale.com/api/latest/hyperfunctions/)
- [PostgreSQL PL/pgSQL](https://www.postgresql.org/docs/current/plpgsql.html)
