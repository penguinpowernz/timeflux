package query

import (
	"fmt"
	"strings"
	"time"

	"github.com/influxdata/influxql"
	"github.com/jackc/pgx/v5"
)

// Translator converts InfluxQL to PostgreSQL SQL
type Translator struct {
	database string
}

// NewTranslator creates a new InfluxQL to SQL translator
func NewTranslator(database string) *Translator {
	return &Translator{
		database: database,
	}
}

// Translate converts an InfluxQL query to PostgreSQL SQL
func (t *Translator) Translate(query string) (string, error) {
	// Parse InfluxQL
	q, err := influxql.ParseQuery(query)
	if err != nil {
		return "", fmt.Errorf("failed to parse InfluxQL: %w", err)
	}

	if len(q.Statements) == 0 {
		return "", fmt.Errorf("no statements in query")
	}

	// For now, handle only the first statement
	stmt := q.Statements[0]

	switch s := stmt.(type) {
	case *influxql.SelectStatement:
		return t.translateSelect(s)
	case *influxql.ShowMeasurementsStatement:
		return t.translateShowMeasurements(s)
	case *influxql.ShowTagKeysStatement:
		return t.translateShowTagKeys(s)
	case *influxql.ShowTagValuesStatement:
		return t.translateShowTagValues(s)
	case *influxql.ShowFieldKeysStatement:
		return t.translateShowFieldKeys(s)
	case *influxql.ShowDatabasesStatement:
		return t.translateShowDatabases(s)
	case *influxql.CreateDatabaseStatement:
		return t.translateCreateDatabase(s)
	case *influxql.ShowSeriesStatement:
		return t.translateShowSeries(s)
	case *influxql.DropSeriesStatement:
		return t.translateDropSeries(s)
	case *influxql.DropMeasurementStatement:
		return t.translateDropMeasurement(s)
	case *influxql.DropDatabaseStatement:
		return t.translateDropDatabase(s)
	default:
		return "", fmt.Errorf("unsupported statement type: %T", stmt)
	}
}

func (t *Translator) translateSelect(stmt *influxql.SelectStatement) (string, error) {
	var sql strings.Builder

	// SELECT clause
	sql.WriteString("SELECT ")

	// Add time_bucket as first column if grouping by time
	if t.hasTimeBucket(stmt.Dimensions) {
		interval := t.getTimeBucketInterval(stmt.Dimensions)
		sql.WriteString(fmt.Sprintf("time_bucket(%s, time) AS time", interval))
		if len(stmt.Fields) > 0 {
			sql.WriteString(", ")
		}
	}

	if err := t.translateFields(stmt, &sql); err != nil {
		return "", err
	}

	// FROM clause
	if len(stmt.Sources) == 0 {
		return "", fmt.Errorf("no sources in query")
	}
	measurement, err := t.getMeasurementName(stmt.Sources[0])
	if err != nil {
		return "", err
	}

	sql.WriteString(" FROM ")
	sql.WriteString(pgx.Identifier{t.database, measurement}.Sanitize())

	// WHERE clause
	if stmt.Condition != nil {
		sql.WriteString(" WHERE ")
		if err := t.translateCondition(stmt.Condition, &sql); err != nil {
			return "", err
		}
	}

	// GROUP BY clause
	if len(stmt.Dimensions) > 0 {
		sql.WriteString(" GROUP BY ")
		if err := t.translateGroupBy(stmt.Dimensions, &sql); err != nil {
			return "", err
		}
	}

	// ORDER BY clause
	if len(stmt.SortFields) > 0 {
		sql.WriteString(" ORDER BY ")
		for i, sort := range stmt.SortFields {
			if i > 0 {
				sql.WriteString(", ")
			}
			sql.WriteString(pgx.Identifier{sort.Name}.Sanitize())
			if sort.Ascending {
				sql.WriteString(" ASC")
			} else {
				sql.WriteString(" DESC")
			}
		}
	} else if t.hasTimeBucket(stmt.Dimensions) {
		// Default: order by time only if we have GROUP BY time()
		sql.WriteString(" ORDER BY time")
	}

	// LIMIT clause
	if stmt.Limit > 0 {
		sql.WriteString(fmt.Sprintf(" LIMIT %d", stmt.Limit))
	}

	// OFFSET clause
	if stmt.Offset > 0 {
		sql.WriteString(fmt.Sprintf(" OFFSET %d", stmt.Offset))
	}

	return sql.String(), nil
}

func (t *Translator) translateFields(stmt *influxql.SelectStatement, sql *strings.Builder) error {
	if len(stmt.Fields) == 0 {
		return fmt.Errorf("no fields in SELECT")
	}

	for i, field := range stmt.Fields {
		if i > 0 {
			sql.WriteString(", ")
		}

		// Translate the field expression
		fieldSQL := t.translateExpr(field.Expr)
		sql.WriteString(fieldSQL)

		// Add alias if present
		if field.Alias != "" {
			sql.WriteString(" AS ")
			sql.WriteString(pgx.Identifier{field.Alias}.Sanitize())
		} else {
			// Auto-generate alias for aggregate functions
			if call, ok := field.Expr.(*influxql.Call); ok {
				alias := strings.ToLower(call.Name)
				sql.WriteString(" AS ")
				sql.WriteString(pgx.Identifier{alias}.Sanitize())
			}
		}
	}

	return nil
}

func (t *Translator) translateExpr(expr influxql.Expr) string {
	switch e := expr.(type) {
	case *influxql.VarRef:
		return pgx.Identifier{e.Val}.Sanitize()

	case *influxql.Call:
		return t.translateCall(e)

	case *influxql.BinaryExpr:
		return t.translateBinaryExpr(e)

	case *influxql.NumberLiteral:
		return fmt.Sprintf("%v", e.Val)

	case *influxql.StringLiteral:
		return "'" + strings.ReplaceAll(e.Val, "'", "''") + "'"

	case *influxql.BooleanLiteral:
		if e.Val {
			return "TRUE"
		}
		return "FALSE"

	case *influxql.TimeLiteral:
		return "'" + strings.ReplaceAll(e.Val.Format(time.RFC3339Nano), "'", "''") + "'"

	case *influxql.DurationLiteral:
		// Convert InfluxDB duration to PostgreSQL interval
		// e.Val is a time.Duration, convert directly to seconds/minutes/hours
		d := e.Val
		if d%(24*time.Hour) == 0 {
			return fmt.Sprintf("INTERVAL '%d days'", d/(24*time.Hour))
		} else if d%time.Hour == 0 {
			return fmt.Sprintf("INTERVAL '%d hours'", d/time.Hour)
		} else if d%time.Minute == 0 {
			return fmt.Sprintf("INTERVAL '%d minutes'", d/time.Minute)
		} else if d%time.Second == 0 {
			return fmt.Sprintf("INTERVAL '%d seconds'", d/time.Second)
		} else {
			return fmt.Sprintf("INTERVAL '%d milliseconds'", d/time.Millisecond)
		}

	case *influxql.Wildcard:
		return "*"

	case *influxql.Distinct:
		// Distinct is handled differently in different InfluxQL versions
		// For now, just return DISTINCT keyword
		return "DISTINCT"

	default:
		return fmt.Sprintf("UNSUPPORTED(%T)", expr)
	}
}

func (t *Translator) translateCall(call *influxql.Call) string {
	switch strings.ToLower(call.Name) {
	case "mean":
		if len(call.Args) > 0 {
			return "AVG(" + t.translateExpr(call.Args[0]) + ")"
		}
		return "AVG(*)"

	case "count":
		if len(call.Args) > 0 {
			return "COUNT(" + t.translateExpr(call.Args[0]) + ")"
		}
		return "COUNT(*)"

	case "sum":
		if len(call.Args) > 0 {
			return "SUM(" + t.translateExpr(call.Args[0]) + ")"
		}
		return "SUM(*)"

	case "max":
		if len(call.Args) > 0 {
			return "MAX(" + t.translateExpr(call.Args[0]) + ")"
		}
		return "MAX(*)"

	case "min":
		if len(call.Args) > 0 {
			return "MIN(" + t.translateExpr(call.Args[0]) + ")"
		}
		return "MIN(*)"

	case "first":
		if len(call.Args) > 0 {
			return fmt.Sprintf("FIRST(%s, time)", t.translateExpr(call.Args[0]))
		}
		return "FIRST(*, time)"

	case "last":
		if len(call.Args) > 0 {
			return fmt.Sprintf("LAST(%s, time)", t.translateExpr(call.Args[0]))
		}
		return "LAST(*, time)"

	case "percentile":
		if len(call.Args) >= 2 {
			// Args: [field, percentile_value]
			// PostgreSQL percentile_cont expects a fraction (0.0-1.0), not 0-100
			percentileExpr := t.translateExpr(call.Args[1])
			// If it's a number literal > 1, divide by 100
			return fmt.Sprintf("percentile_cont(%s / 100.0) WITHIN GROUP (ORDER BY %s)",
				percentileExpr,
				t.translateExpr(call.Args[0]))
		}

	case "now":
		return "NOW()"

	default:
		// Generic function call
		var args []string
		for _, arg := range call.Args {
			args = append(args, t.translateExpr(arg))
		}
		return fmt.Sprintf("%s(%s)", strings.ToUpper(call.Name), strings.Join(args, ", "))
	}

	return ""
}

func (t *Translator) translateBinaryExpr(expr *influxql.BinaryExpr) string {
	lhs := t.translateExpr(expr.LHS)
	rhs := t.translateExpr(expr.RHS)

	switch expr.Op {
	case influxql.ADD:
		return fmt.Sprintf("(%s + %s)", lhs, rhs)
	case influxql.SUB:
		return fmt.Sprintf("(%s - %s)", lhs, rhs)
	case influxql.MUL:
		return fmt.Sprintf("(%s * %s)", lhs, rhs)
	case influxql.DIV:
		return fmt.Sprintf("(%s / %s)", lhs, rhs)
	case influxql.EQ:
		return fmt.Sprintf("%s = %s", lhs, rhs)
	case influxql.NEQ:
		return fmt.Sprintf("%s != %s", lhs, rhs)
	case influxql.LT:
		return fmt.Sprintf("%s < %s", lhs, rhs)
	case influxql.LTE:
		return fmt.Sprintf("%s <= %s", lhs, rhs)
	case influxql.GT:
		return fmt.Sprintf("%s > %s", lhs, rhs)
	case influxql.GTE:
		return fmt.Sprintf("%s >= %s", lhs, rhs)
	case influxql.AND:
		return fmt.Sprintf("(%s AND %s)", lhs, rhs)
	case influxql.OR:
		return fmt.Sprintf("(%s OR %s)", lhs, rhs)
	default:
		return fmt.Sprintf("(%s %s %s)", lhs, expr.Op.String(), rhs)
	}
}

func (t *Translator) translateCondition(expr influxql.Expr, sql *strings.Builder) error {
	sql.WriteString(t.translateExpr(expr))
	return nil
}

func (t *Translator) translateGroupBy(dimensions influxql.Dimensions, sql *strings.Builder) error {
	for i, dim := range dimensions {
		if i > 0 {
			sql.WriteString(", ")
		}

		switch d := dim.Expr.(type) {
		case *influxql.Call:
			// Handle time() function for time bucketing
			if strings.ToLower(d.Name) == "time" {
				if len(d.Args) > 0 {
					// translateExpr already returns INTERVAL 'X units' for DurationLiteral
					interval := t.translateExpr(d.Args[0])
					timeBucket := fmt.Sprintf("time_bucket(%s, time)", interval)
					sql.WriteString(timeBucket)
				}
			} else {
				sql.WriteString(t.translateExpr(d))
			}

		case *influxql.VarRef:
			sql.WriteString(pgx.Identifier{d.Val}.Sanitize())

		default:
			sql.WriteString(t.translateExpr(d))
		}
	}

	return nil
}

func (t *Translator) durationToInterval(duration string) string {
	// Remove quotes if present
	duration = strings.Trim(duration, "'\"")

	// DurationLiteral.String() returns formats like "5m0s", "2h0m0s", etc.
	// We need to parse this and convert to PostgreSQL interval format

	// If it already contains spaces (like "5m0 second"), it's already partially formatted
	// Just clean it up by removing the trailing unit and standardizing
	if strings.Contains(duration, " ") {
		// Strip trailing "second", "seconds", etc and rebuild
		duration = strings.TrimSuffix(strings.TrimSpace(duration), "s")
		duration = strings.TrimSuffix(strings.TrimSpace(duration), "second")
	}

	// Simple approach: handle common single-unit formats first
	// Common InfluxDB duration formats: 1s, 5m, 1h, 1d, 1w
	if len(duration) >= 2 && !strings.Contains(duration, " ") {
		value := duration[:len(duration)-1]
		unit := duration[len(duration)-1:]

		switch unit {
		case "s":
			return value + " seconds"
		case "m":
			return value + " minutes"
		case "h":
			return value + " hours"
		case "d":
			return value + " days"
		case "w":
			return value + " weeks"
		default:
			// If it doesn't match simple pattern, return as-is
			return duration
		}
	}

	return duration
}

func (t *Translator) hasTimeBucket(dimensions influxql.Dimensions) bool {
	for _, dim := range dimensions {
		if call, ok := dim.Expr.(*influxql.Call); ok {
			if strings.ToLower(call.Name) == "time" {
				return true
			}
		}
	}
	return false
}

func (t *Translator) getTimeBucketInterval(dimensions influxql.Dimensions) string {
	for _, dim := range dimensions {
		if call, ok := dim.Expr.(*influxql.Call); ok {
			if strings.ToLower(call.Name) == "time" && len(call.Args) > 0 {
				return t.translateExpr(call.Args[0])
			}
		}
	}
	return "INTERVAL '1 minute'" // default fallback
}

func (t *Translator) getMeasurementName(source influxql.Source) (string, error) {
	switch s := source.(type) {
	case *influxql.Measurement:
		return s.Name, nil
	default:
		return "", fmt.Errorf("unsupported source type: %T", source)
	}
}

func (t *Translator) translateShowMeasurements(stmt *influxql.ShowMeasurementsStatement) (string, error) {
	return fmt.Sprintf(`
		SELECT DISTINCT measurement AS name
		FROM %s
		ORDER BY name
	`, pgx.Identifier{t.database, "_timeflux_metadata"}.Sanitize()), nil
}

func (t *Translator) translateShowTagKeys(stmt *influxql.ShowTagKeysStatement) (string, error) {
	var sql strings.Builder
	sql.WriteString(fmt.Sprintf(`
		SELECT DISTINCT column_name AS tagKey
		FROM %s
		WHERE is_tag = TRUE
	`, pgx.Identifier{t.database, "_timeflux_metadata"}.Sanitize()))

	if len(stmt.Sources) > 0 {
		measurement, err := t.getMeasurementName(stmt.Sources[0])
		if err != nil {
			return "", err
		}
		sql.WriteString(fmt.Sprintf(" AND measurement = '%s'", strings.ReplaceAll(measurement, "'", "''")))
	}

	sql.WriteString(" ORDER BY tagKey")
	return sql.String(), nil
}

func (t *Translator) translateShowTagValues(stmt *influxql.ShowTagValuesStatement) (string, error) {
	if len(stmt.Sources) == 0 {
		return "", fmt.Errorf("SHOW TAG VALUES requires a measurement")
	}

	measurement, err := t.getMeasurementName(stmt.Sources[0])
	if err != nil {
		return "", err
	}

	// Extract tag key from the condition
	// SHOW TAG VALUES typically has a WITH KEY clause
	// For simplicity, we'll look at the Condition field
	tagName := ""
	if stmt.Condition != nil {
		if binaryExpr, ok := stmt.Condition.(*influxql.BinaryExpr); ok {
			if varRef, ok := binaryExpr.LHS.(*influxql.VarRef); ok {
				if varRef.Val == "_tagKey" && binaryExpr.Op == influxql.EQ {
					if strLit, ok := binaryExpr.RHS.(*influxql.StringLiteral); ok {
						tagName = strLit.Val
					}
				}
			}
		}
	}

	if tagName == "" {
		// Fallback: return all distinct values from all tag columns
		return "", fmt.Errorf("SHOW TAG VALUES requires WITH KEY clause")
	}

	return fmt.Sprintf(`
		SELECT DISTINCT %s AS value
		FROM %s
		WHERE %s IS NOT NULL
		ORDER BY value
	`,
		pgx.Identifier{tagName}.Sanitize(),
		pgx.Identifier{t.database, measurement}.Sanitize(),
		pgx.Identifier{tagName}.Sanitize(),
	), nil
}

func (t *Translator) translateShowFieldKeys(stmt *influxql.ShowFieldKeysStatement) (string, error) {
	var sql strings.Builder
	sql.WriteString(fmt.Sprintf(`
		SELECT column_name AS fieldKey, column_type AS fieldType
		FROM %s
		WHERE is_tag = FALSE
	`, pgx.Identifier{t.database, "_timeflux_metadata"}.Sanitize()))

	if len(stmt.Sources) > 0 {
		measurement, err := t.getMeasurementName(stmt.Sources[0])
		if err != nil {
			return "", err
		}
		sql.WriteString(fmt.Sprintf(" AND measurement = '%s'", strings.ReplaceAll(measurement, "'", "''")))
	}

	sql.WriteString(" ORDER BY fieldKey")
	return sql.String(), nil
}

func (t *Translator) translateShowDatabases(stmt *influxql.ShowDatabasesStatement) (string, error) {
	// Query PostgreSQL schemas (excluding system schemas)
	return `
		SELECT nspname AS name
		FROM pg_namespace
		WHERE nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast', 'timescaledb_information', 'timescaledb_experimental')
		  AND nspname NOT LIKE 'pg_temp_%'
		  AND nspname NOT LIKE 'pg_toast_temp_%'
		  AND nspname NOT LIKE '_timescaledb_%'
		ORDER BY name
	`, nil
}

func (t *Translator) translateCreateDatabase(stmt *influxql.CreateDatabaseStatement) (string, error) {
	// Sanitize the database name
	dbName := pgx.Identifier{stmt.Name}.Sanitize()
	return fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", dbName), nil
}

func (t *Translator) translateShowSeries(stmt *influxql.ShowSeriesStatement) (string, error) {
	var sql strings.Builder

	// Get measurement name if specified
	var measurement string
	if len(stmt.Sources) > 0 {
		var err error
		measurement, err = t.getMeasurementName(stmt.Sources[0])
		if err != nil {
			return "", err
		}
	}

	// Query to get distinct tag combinations (series)
	// A series in InfluxDB is a unique combination of measurement + tag set
	if measurement != "" {
		// Get tag columns for this measurement
		tableName := pgx.Identifier{t.database, measurement}.Sanitize()
		sql.WriteString(fmt.Sprintf(`
			SELECT DISTINCT
				'%s' || ',' || string_agg(column_name || '=' || COALESCE(pg_catalog.pg_get_expr(adbin, adrelid), ''), ',' ORDER BY column_name) AS key
			FROM %s
			CROSS JOIN (
				SELECT column_name
				FROM %s._timeflux_metadata
				WHERE measurement = '%s' AND is_tag = true
				ORDER BY column_name
			) tags
			GROUP BY time
			LIMIT 100
		`, measurement, tableName, pgx.Identifier{t.database}.Sanitize(), measurement))
	} else {
		// Show series across all measurements
		sql.WriteString(fmt.Sprintf(`
			SELECT measurement || ',' || column_name AS key
			FROM %s._timeflux_metadata
			WHERE is_tag = true
			ORDER BY measurement, column_name
		`, pgx.Identifier{t.database}.Sanitize()))
	}

	return sql.String(), nil
}

func (t *Translator) translateDropSeries(stmt *influxql.DropSeriesStatement) (string, error) {
	// DROP SERIES deletes all data points matching the WHERE condition
	var sql strings.Builder

	// Get measurement name if specified
	var measurement string
	if len(stmt.Sources) > 0 {
		var err error
		measurement, err = t.getMeasurementName(stmt.Sources[0])
		if err != nil {
			return "", err
		}
	} else {
		return "", fmt.Errorf("DROP SERIES requires FROM clause with measurement name")
	}

	tableName := pgx.Identifier{t.database, measurement}.Sanitize()
	sql.WriteString(fmt.Sprintf("DELETE FROM %s", tableName))

	// Add WHERE condition if specified
	if stmt.Condition != nil {
		sql.WriteString(" WHERE ")
		if err := t.translateCondition(stmt.Condition, &sql); err != nil {
			return "", err
		}
	} else {
		// If no condition, delete all data from the measurement
		// (but keep the table structure)
		sql.WriteString(" WHERE true")
	}

	return sql.String(), nil
}

func (t *Translator) translateDropMeasurement(stmt *influxql.DropMeasurementStatement) (string, error) {
	// DROP MEASUREMENT drops the entire table
	// Note: Metadata cleanup should be handled separately if needed
	tableName := pgx.Identifier{t.database, stmt.Name}.Sanitize()
	return fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", tableName), nil
}

func (t *Translator) translateDropDatabase(stmt *influxql.DropDatabaseStatement) (string, error) {
	// DROP DATABASE drops the entire schema
	dbName := pgx.Identifier{stmt.Name}.Sanitize()
	return fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", dbName), nil
}

