package metrics

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	internalDatabase = "_internal"
)

// Flusher periodically writes metrics to the _internal database
type Flusher struct {
	pool     *pgxpool.Pool
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewFlusher creates a new metrics flusher
func NewFlusher(pool *pgxpool.Pool, interval time.Duration) *Flusher {
	return &Flusher{
		pool:     pool,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the periodic flushing of metrics
func (f *Flusher) Start() {
	log.Printf("Starting metrics flusher (interval: %v, database: %s)", f.interval, internalDatabase)

	// Ensure _internal database exists
	if err := f.ensureInternalDatabaseExists(); err != nil {
		log.Printf("Warning: failed to create _internal database: %v", err)
		log.Printf("Metrics will not be stored")
		close(f.doneCh)
		return
	}

	go f.run()
}

// Shutdown stops the flusher and waits for completion
func (f *Flusher) Shutdown() {
	close(f.stopCh)
	<-f.doneCh
	log.Printf("Metrics flusher stopped")
}

// run is the main loop that periodically flushes metrics
func (f *Flusher) run() {
	defer close(f.doneCh)

	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			log.Printf("Flushing metrics to %s database...", internalDatabase)
			if err := f.flush(); err != nil {
				log.Printf("Error flushing metrics: %v", err)
			} else {
				log.Printf("Metrics flushed successfully")
			}
		case <-f.stopCh:
			// Final flush before shutdown
			if err := f.flush(); err != nil {
				log.Printf("Error during final metrics flush: %v", err)
			}
			return
		}
	}
}

// flush writes current metrics to the _internal database
func (f *Flusher) flush() error {
	ctx := context.Background()
	timestamp := time.Now().UnixNano()

	// Get current metrics snapshot
	m := Global()
	snapshot := m.Snapshot()

	// Convert metrics to line protocol
	var lines []string

	// httpd metrics
	writes := snapshot["writes"].(map[string]interface{})
	queries := snapshot["queries"].(map[string]interface{})
	lines = append(lines, fmt.Sprintf(
		"httpd write_requests=%di,write_errors=%di,query_requests=%di,query_errors=%di %d",
		writes["requests"],
		writes["errors"],
		queries["requests"],
		queries["errors"],
		timestamp,
	))

	// write metrics
	lines = append(lines, fmt.Sprintf(
		"write points_written=%di,duration_avg_ms=%di,duration_min_ms=%di,duration_max_ms=%di,duration_count=%di %d",
		writes["points_written"],
		writes["duration_avg_ms"],
		writes["duration_min_ms"],
		writes["duration_max_ms"],
		writes["duration_count"],
		timestamp,
	))

	// query metrics
	lines = append(lines, fmt.Sprintf(
		"query requests=%di,errors=%di,duration_avg_ms=%di,duration_min_ms=%di,duration_max_ms=%di,duration_count=%di %d",
		queries["requests"],
		queries["errors"],
		queries["duration_avg_ms"],
		queries["duration_min_ms"],
		queries["duration_max_ms"],
		queries["duration_count"],
		timestamp,
	))

	// schema metrics
	schemaMetrics := snapshot["schema"].(map[string]interface{})
	lines = append(lines, fmt.Sprintf(
		"schema evolutions=%di,cache_hits=%di,cache_misses=%di %d",
		schemaMetrics["evolutions"],
		schemaMetrics["cache_hits"],
		schemaMetrics["cache_misses"],
		timestamp,
	))

	// wal metrics
	walMetrics := snapshot["wal"].(map[string]interface{})
	lines = append(lines, fmt.Sprintf(
		"wal writes=%di,bytes=%di,write_errors=%di,corruptions=%di,replay_success=%di,replay_failures=%di,duration_avg_us=%di,duration_min_us=%di,duration_max_us=%di,duration_count=%di %d",
		walMetrics["writes"],
		walMetrics["bytes"],
		walMetrics["write_errors"],
		walMetrics["corruptions"],
		walMetrics["replay_success"],
		walMetrics["replay_failures"],
		walMetrics["duration_avg_us"],
		walMetrics["duration_min_us"],
		walMetrics["duration_max_us"],
		walMetrics["duration_count"],
		timestamp,
	))

	// runtime metrics
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	lines = append(lines, fmt.Sprintf(
		"runtime goroutines=%di,alloc_bytes=%di,sys_bytes=%di,heap_objects=%di,gc_runs=%di %d",
		runtime.NumGoroutine(),
		memStats.Alloc,
		memStats.Sys,
		memStats.HeapObjects,
		memStats.NumGC,
		timestamp,
	))

	// pool metrics
	poolMetrics := snapshot["pool"].(map[string]interface{})
	lines = append(lines, fmt.Sprintf(
		"database acquire_count=%di,acquire_avg_ms=%di,acquire_min_ms=%di,acquire_max_ms=%di %d",
		poolMetrics["acquire_count"],
		poolMetrics["acquire_avg_ms"],
		poolMetrics["acquire_min_ms"],
		poolMetrics["acquire_max_ms"],
		timestamp,
	))

	lineProtocol := strings.Join(lines, "\n")

	log.Printf("Generated %d lines of metrics line protocol", len(lines))
	// Write to _internal database using existing write infrastructure
	return f.writeLineProtocol(ctx, internalDatabase, lineProtocol)
}

// writeLineProtocol writes line protocol data to the specified database
func (f *Flusher) writeLineProtocol(ctx context.Context, database, lineProtocol string) error {
	log.Printf("Parsing metrics line protocol...")
	// Parse the line protocol (reuse existing parser)
	points, err := parseMetricsLineProtocol(lineProtocol)
	if err != nil {
		return fmt.Errorf("failed to parse metrics line protocol: %w", err)
	}
	log.Printf("Parsed %d metrics points", len(points))

	// Group points by measurement
	measurementPoints := make(map[string][]*metricsPoint)
	for _, point := range points {
		measurementPoints[point.measurement] = append(measurementPoints[point.measurement], point)
	}
	log.Printf("Grouped into %d measurements", len(measurementPoints))

	// Write each measurement
	for measurement, points := range measurementPoints {
		log.Printf("Writing measurement %s (%d points)...", measurement, len(points))
		if err := f.writeMeasurement(ctx, database, measurement, points); err != nil {
			return fmt.Errorf("failed to write measurement %s: %w", measurement, err)
		}
	}

	return nil
}

// metricsPoint represents a parsed metrics point
type metricsPoint struct {
	measurement string
	fields      map[string]interface{}
	timestamp   time.Time
}

// parseMetricsLineProtocol parses simplified line protocol for metrics
func parseMetricsLineProtocol(lineProtocol string) ([]*metricsPoint, error) {
	var points []*metricsPoint

	lines := strings.Split(lineProtocol, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Format: measurement field1=val1,field2=val2 timestamp
		parts := strings.Fields(line)
		if len(parts) != 3 {
			return nil, fmt.Errorf("invalid line protocol: %s", line)
		}

		measurement := parts[0]
		fieldsPart := parts[1]
		timestampNano := parts[2]

		// Parse timestamp
		var ts int64
		if _, err := fmt.Sscanf(timestampNano, "%d", &ts); err != nil {
			return nil, fmt.Errorf("invalid timestamp: %s", timestampNano)
		}
		timestamp := time.Unix(0, ts)

		// Parse fields
		fields := make(map[string]interface{})
		for _, fieldPair := range strings.Split(fieldsPart, ",") {
			kv := strings.SplitN(fieldPair, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid field: %s", fieldPair)
			}

			key := kv[0]
			value := kv[1]

			// Parse value (integer indicated by 'i' suffix)
			if strings.HasSuffix(value, "i") {
				var intVal int64
				if _, err := fmt.Sscanf(value, "%di", &intVal); err != nil {
					return nil, fmt.Errorf("invalid integer value: %s", value)
				}
				fields[key] = intVal
			} else {
				// Assume float
				var floatVal float64
				if _, err := fmt.Sscanf(value, "%f", &floatVal); err != nil {
					return nil, fmt.Errorf("invalid float value: %s", value)
				}
				fields[key] = floatVal
			}
		}

		points = append(points, &metricsPoint{
			measurement: measurement,
			fields:      fields,
			timestamp:   timestamp,
		})
	}

	return points, nil
}

// writeMeasurement writes metrics points to a specific measurement
func (f *Flusher) writeMeasurement(ctx context.Context, database, measurement string, points []*metricsPoint) error {
	if len(points) == 0 {
		return nil
	}

	// Get all unique field names
	fieldNames := make(map[string]bool)
	for _, point := range points {
		for field := range point.fields {
			fieldNames[field] = true
		}
	}

	// Build column list
	columns := []string{"time"}
	for field := range fieldNames {
		columns = append(columns, field)
	}

	// Ensure schema exists
	tableName := fmt.Sprintf("%s.%s", database, measurement)
	if err := f.ensureMetricsTable(ctx, database, measurement, columns); err != nil {
		return fmt.Errorf("failed to ensure table exists: %w", err)
	}

	// Build sanitized column identifiers for the INSERT statement
	sanitizedCols := make([]string, len(columns))
	sanitizedCols[0] = pgx.Identifier{"time"}.Sanitize()
	for i, col := range columns[1:] {
		sanitizedCols[i+1] = pgx.Identifier{col}.Sanitize()
	}

	// Use simple INSERT statements (metrics volume is low)
	log.Printf("Inserting %d points into %s", len(points), tableName)
	for pointIdx, point := range points {
		placeholders := make([]string, len(columns))
		values := make([]interface{}, len(columns))
		values[0] = point.timestamp
		placeholders[0] = "$1"

		idx := 1
		for i, col := range columns[1:] {
			idx++
			placeholders[i+1] = fmt.Sprintf("$%d", idx)
			if val, ok := point.fields[col]; ok {
				values[i+1] = val
			} else {
				values[i+1] = nil
			}
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s)",
			pgx.Identifier{database, measurement}.Sanitize(),
			strings.Join(sanitizedCols, ", "),
			strings.Join(placeholders, ", "),
		)

		log.Printf("Inserting point %d/%d into %s", pointIdx+1, len(points), tableName)
		if _, err := f.pool.Exec(ctx, insertSQL, values...); err != nil {
			return fmt.Errorf("failed to insert metrics: %w", err)
		}
	}
	log.Printf("Successfully inserted %d points into %s", len(points), tableName)

	return nil
}

// ensureMetricsTable ensures the metrics table exists with the required columns
func (f *Flusher) ensureMetricsTable(ctx context.Context, database, measurement string, columns []string) error {
	log.Printf("Ensuring metrics table %s.%s exists", database, measurement)

	sanitizedSchema := pgx.Identifier{database}.Sanitize()
	sanitizedTable := pgx.Identifier{database, measurement}.Sanitize()

	// Create schema if it doesn't exist
	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", sanitizedSchema)
	if _, err := f.pool.Exec(ctx, createSchemaSQL); err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}
	log.Printf("Schema %s ready", database)

	// Create table if it doesn't exist
	createTableSQL := fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (time TIMESTAMPTZ NOT NULL)",
		sanitizedTable,
	)
	if _, err := f.pool.Exec(ctx, createTableSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	log.Printf("Table %s.%s ready", database, measurement)

	// Ensure it's a hypertable — create_hypertable requires an unquoted string literal
	// for the table name argument; strip outer quotes from sanitized form
	unquotedTable := strings.ReplaceAll(sanitizedTable, `"`, ``)
	hypertableSQL := fmt.Sprintf(
		"SELECT create_hypertable('%s', 'time', if_not_exists => TRUE)",
		unquotedTable,
	)
	log.Printf("Creating hypertable for %s.%s...", database, measurement)
	if _, err := f.pool.Exec(ctx, hypertableSQL); err != nil {
		// Ignore error if already a hypertable
		if !strings.Contains(err.Error(), "already a hypertable") {
			log.Printf("Warning: failed to create hypertable for %s.%s: %v", database, measurement, err)
		}
	}
	log.Printf("Hypertable %s.%s ready", database, measurement)

	// Add missing columns
	for _, col := range columns[1:] { // Skip 'time' column
		alterSQL := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s BIGINT",
			sanitizedTable,
			pgx.Identifier{col}.Sanitize(),
		)
		if _, err := f.pool.Exec(ctx, alterSQL); err != nil {
			return fmt.Errorf("failed to add column %s: %w", col, err)
		}
	}
	log.Printf("All columns added to %s.%s", database, measurement)

	return nil
}

// ensureInternalDatabaseExists creates the _internal schema if it doesn't exist
func (f *Flusher) ensureInternalDatabaseExists() error {
	ctx := context.Background()
	createSchemaSQL := fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{internalDatabase}.Sanitize())
	if _, err := f.pool.Exec(ctx, createSchemaSQL); err != nil {
		return fmt.Errorf("failed to create _internal schema: %w", err)
	}
	return nil
}
