package schema

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/metrics"
)

// MeasurementSchema holds the schema for a single measurement
type MeasurementSchema struct {
	Tags   map[string]bool   // tag name -> exists
	Fields map[string]string // field name -> SQL type (DOUBLE PRECISION, BIGINT, TEXT, BOOLEAN)
}

// SchemaManager manages dynamic schema evolution with concurrency safety
type SchemaManager struct {
	mu               sync.RWMutex
	schemas          map[string]map[string]*MeasurementSchema // database -> measurement -> schema
	measurementLocks sync.Map                                 // key: "database.measurement" -> *sync.Mutex for DDL
	pool             *pgxpool.Pool
	indexQueue       chan indexJob // background index creation queue
	indexWorkers     sync.WaitGroup
}

type indexJob struct {
	database    string
	measurement string
	columnName  string
	isTag       bool
}

// NewSchemaManager creates a new SchemaManager
func NewSchemaManager(pool *pgxpool.Pool) *SchemaManager {
	sm := &SchemaManager{
		schemas:    make(map[string]map[string]*MeasurementSchema),
		pool:       pool,
		indexQueue: make(chan indexJob, 1000),
	}

	// Start background index workers
	numWorkers := 4
	for i := 0; i < numWorkers; i++ {
		sm.indexWorkers.Add(1)
		go sm.indexWorker()
	}

	return sm
}

// indexWorker processes background index creation jobs
func (sm *SchemaManager) indexWorker() {
	defer sm.indexWorkers.Done()

	for job := range sm.indexQueue {
		// Use a timeout to prevent a slow CREATE INDEX from stalling the worker indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		if job.isTag {
			indexName := fmt.Sprintf("%s_%s_idx", job.measurement, job.columnName)
			_, err := sm.pool.Exec(ctx, fmt.Sprintf(
				"CREATE INDEX IF NOT EXISTS %s ON %s (%s, time DESC)",
				pgx.Identifier{indexName}.Sanitize(),
				pgx.Identifier{job.database, job.measurement}.Sanitize(),
				pgx.Identifier{job.columnName}.Sanitize(),
			))
			if err != nil {
				log.Printf("Background index creation failed for %s.%s.%s: %v",
					job.database, job.measurement, job.columnName, err)
			} else {
				log.Printf("Created index on %s.%s(%s)", job.database, job.measurement, job.columnName)
			}
		}
		cancel()
	}
}

// Shutdown gracefully stops the schema manager
func (sm *SchemaManager) Shutdown() {
	close(sm.indexQueue)
	sm.indexWorkers.Wait()
}

// LoadExistingSchemas loads existing table schemas from PostgreSQL
func (sm *SchemaManager) LoadExistingSchemas(ctx context.Context) error {
	// Query all schemas (databases in InfluxDB terminology)
	rows, err := sm.pool.Query(ctx, `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast', 'timescaledb_information', 'timescaledb_experimental')
	`)
	if err != nil {
		return fmt.Errorf("failed to query schemas: %w", err)
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var schemaName string
		if err := rows.Scan(&schemaName); err != nil {
			return fmt.Errorf("failed to scan schema name: %w", err)
		}
		schemas = append(schemas, schemaName)
	}

	// Load tables and columns for all schemas in a single query
	if err := sm.loadAllSchemas(ctx, schemas); err != nil {
		return fmt.Errorf("failed to load schemas: %w", err)
	}

	return nil
}

func (sm *SchemaManager) loadAllSchemas(ctx context.Context, databases []string) error {
	if len(databases) == 0 {
		return nil
	}

	// Build parameterized query with placeholders
	placeholders := make([]string, len(databases))
	args := make([]interface{}, len(databases))
	for i, db := range databases {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = db
	}

	// Query all columns for all schemas at once using parameterized query
	query := fmt.Sprintf(`
		SELECT
			table_schema,
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_schema = ANY($1::text[])
			AND table_name != '_timeflux_metadata'
		ORDER BY table_schema, table_name, ordinal_position
	`)

	rows, err := sm.pool.Query(ctx, query, databases)
	if err != nil {
		return fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	// Build schema map without holding the write lock
	tempSchemas := make(map[string]map[string]*MeasurementSchema)

	for rows.Next() {
		var schemaName, tableName, columnName, dataType string
		if err := rows.Scan(&schemaName, &tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if columnName == "time" {
			continue // Skip the time column
		}

		if tempSchemas[schemaName] == nil {
			tempSchemas[schemaName] = make(map[string]*MeasurementSchema)
		}
		if tempSchemas[schemaName][tableName] == nil {
			tempSchemas[schemaName][tableName] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}

		// Heuristic: TEXT columns are likely tags
		sqlType := postgresTypeToSQL(dataType)
		if sqlType == "TEXT" {
			tempSchemas[schemaName][tableName].Tags[columnName] = true
		} else {
			tempSchemas[schemaName][tableName].Fields[columnName] = sqlType
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

	// Now update the schema map with write lock
	sm.mu.Lock()
	for schemaName, measurements := range tempSchemas {
		if sm.schemas[schemaName] == nil {
			sm.schemas[schemaName] = make(map[string]*MeasurementSchema)
		}
		for tableName, schema := range measurements {
			sm.schemas[schemaName][tableName] = schema
		}
	}
	sm.mu.Unlock()

	// Load metadata for all databases
	for _, database := range databases {
		if err := sm.loadMetadata(ctx, database); err != nil {
			log.Printf("Warning: failed to load metadata for database %s: %v", database, err)
		}
	}

	return nil
}

func (sm *SchemaManager) loadSchemaForDatabase(ctx context.Context, database string) error {
	// Query tables and columns
	rows, err := sm.pool.Query(ctx, `
		SELECT
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_schema = $1
			AND table_name != '_timeflux_metadata'
		ORDER BY table_name, ordinal_position
	`, database)
	if err != nil {
		return fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	// Build schema map without holding the write lock
	tempMeasurements := make(map[string]*MeasurementSchema)

	for rows.Next() {
		var tableName, columnName, dataType string
		if err := rows.Scan(&tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if columnName == "time" {
			continue // Skip the time column
		}

		if tempMeasurements[tableName] == nil {
			tempMeasurements[tableName] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}

		// Determine if it's a tag or field by checking metadata table
		// For now, we'll use a heuristic: TEXT columns without specific numeric types are likely tags
		sqlType := postgresTypeToSQL(dataType)
		if sqlType == "TEXT" {
			// Could be tag or field - check metadata table if it exists
			tempMeasurements[tableName].Tags[columnName] = true
		} else {
			tempMeasurements[tableName].Fields[columnName] = sqlType
		}
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("row iteration error: %w", err)
	}

	// Now update the schema map with write lock
	sm.mu.Lock()
	if sm.schemas[database] == nil {
		sm.schemas[database] = make(map[string]*MeasurementSchema)
	}
	for tableName, schema := range tempMeasurements {
		sm.schemas[database][tableName] = schema
	}
	sm.mu.Unlock()

	// Load actual tag/field distinctions from metadata table if it exists
	if err := sm.loadMetadata(ctx, database); err != nil {
		log.Printf("Warning: failed to load metadata for database %s: %v", database, err)
	}

	return nil
}

func (sm *SchemaManager) loadMetadata(ctx context.Context, database string) error {
	rows, err := sm.pool.Query(ctx, `
		SELECT measurement, column_name, column_type, is_tag
		FROM `+pgx.Identifier{database, "_timeflux_metadata"}.Sanitize())
	if err != nil {
		// Metadata table doesn't exist yet
		return nil
	}
	defer rows.Close()

	// Collect updates without holding the lock (avoids holding lock during I/O)
	type columnUpdate struct {
		columnType string
		isTag      bool
	}
	updates := make(map[string]map[string]columnUpdate) // measurement -> columnName -> update

	for rows.Next() {
		var measurement, columnName, columnType string
		var isTag bool
		if err := rows.Scan(&measurement, &columnName, &columnType, &isTag); err != nil {
			return fmt.Errorf("failed to scan metadata row: %w", err)
		}
		if updates[measurement] == nil {
			updates[measurement] = make(map[string]columnUpdate)
		}
		updates[measurement][columnName] = columnUpdate{columnType: columnType, isTag: isTag}
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Apply updates under write lock
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for measurement, cols := range updates {
		if sm.schemas[database] == nil {
			sm.schemas[database] = make(map[string]*MeasurementSchema)
		}
		if sm.schemas[database][measurement] == nil {
			sm.schemas[database][measurement] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}
		for columnName, upd := range cols {
			if upd.isTag {
				sm.schemas[database][measurement].Tags[columnName] = true
				delete(sm.schemas[database][measurement].Fields, columnName)
			} else {
				sm.schemas[database][measurement].Fields[columnName] = upd.columnType
				delete(sm.schemas[database][measurement].Tags, columnName)
			}
		}
	}

	return nil
}

// EnsureSchema ensures the schema exists for the given measurement with the specified tags and fields
func (sm *SchemaManager) EnsureSchema(ctx context.Context, database, measurement string, tags map[string]bool, fields map[string]string) error {
	m := metrics.Global()

	// Fast path: check if schema already has all needed columns
	sm.mu.RLock()
	if sm.hasAllColumns(database, measurement, tags, fields) {
		sm.mu.RUnlock()
		m.SchemaCacheHits.Add(1)
		return nil
	}
	sm.mu.RUnlock()
	m.SchemaCacheMisses.Add(1)

	// Slow path: acquire measurement-specific lock BEFORE checking again
	// This prevents race condition where two goroutines both see missing columns
	lockKey := database + "." + measurement

	// Use sync.Map to avoid lock contention
	lockIface, _ := sm.measurementLocks.LoadOrStore(lockKey, &sync.Mutex{})
	lock := lockIface.(*sync.Mutex)

	// Acquire lock BEFORE the double-check
	lock.Lock()
	defer lock.Unlock()

	// Double-check pattern: verify another goroutine didn't already do the work
	// Now safe because we hold the measurement lock
	sm.mu.RLock()
	if sm.hasAllColumns(database, measurement, tags, fields) {
		sm.mu.RUnlock()
		return nil
	}
	sm.mu.RUnlock()

	// Perform DDL operations
	err := sm.ensureSchemaSlow(ctx, database, measurement, tags, fields)
	if err == nil {
		m.SchemaEvolutions.Add(1)
	}
	return err
}

func (sm *SchemaManager) hasAllColumns(database, measurement string, tags map[string]bool, fields map[string]string) bool {
	dbSchemas, ok := sm.schemas[database]
	if !ok {
		return false
	}

	schema, ok := dbSchemas[measurement]
	if !ok {
		return false
	}

	for tag := range tags {
		if !schema.Tags[tag] {
			return false
		}
	}

	for field, fieldType := range fields {
		existingType, ok := schema.Fields[field]
		if !ok || existingType != fieldType {
			return false
		}
	}

	return true
}

func (sm *SchemaManager) ensureSchemaSlow(ctx context.Context, database, measurement string, tags map[string]bool, fields map[string]string) error {
	// Ensure database (schema) exists
	if err := sm.ensureDatabase(ctx, database); err != nil {
		return err
	}

	// Ensure measurement (table) exists
	if err := sm.ensureTable(ctx, database, measurement); err != nil {
		return err
	}

	// Get current schema to determine what's missing
	sm.mu.RLock()
	existingSchema, exists := sm.schemas[database][measurement]
	sm.mu.RUnlock()

	missingTags := make(map[string]bool)
	missingFields := make(map[string]string)

	if exists {
		for tag := range tags {
			if !existingSchema.Tags[tag] {
				missingTags[tag] = true
			}
		}
		for field, fieldType := range fields {
			if existingType, ok := existingSchema.Fields[field]; !ok || existingType != fieldType {
				missingFields[field] = fieldType
			}
		}
	} else {
		missingTags = tags
		missingFields = fields
	}

	// Batch all DDL operations in a single transaction
	if len(missingTags) > 0 || len(missingFields) > 0 {
		tx, err := sm.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction: %w", err)
		}
		defer tx.Rollback(ctx)

		// Add missing tags (without indexes in critical path)
		for tag := range missingTags {
			if err := sm.ensureTagColumnTx(ctx, tx, database, measurement, tag); err != nil {
				return err
			}
			// Queue index creation in background (non-blocking to avoid deadlock when queue is full)
			select {
			case sm.indexQueue <- indexJob{
				database:    database,
				measurement: measurement,
				columnName:  tag,
				isTag:       true,
			}:
			default:
				log.Printf("Warning: index queue full, background index for %s.%s(%s) will not be created",
					database, measurement, tag)
			}
		}

		// Add missing fields
		for field, fieldType := range missingFields {
			if err := sm.ensureFieldColumnTx(ctx, tx, database, measurement, field, fieldType); err != nil {
				return err
			}
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit schema changes: %w", err)
		}
	}

	// Update in-memory cache
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.schemas[database] == nil {
		sm.schemas[database] = make(map[string]*MeasurementSchema)
	}
	if sm.schemas[database][measurement] == nil {
		sm.schemas[database][measurement] = &MeasurementSchema{
			Tags:   make(map[string]bool),
			Fields: make(map[string]string),
		}
	}

	for tag := range tags {
		sm.schemas[database][measurement].Tags[tag] = true
	}
	for field, fieldType := range fields {
		sm.schemas[database][measurement].Fields[field] = fieldType
	}

	return nil
}

func (sm *SchemaManager) ensureDatabase(ctx context.Context, database string) error {
	// Create schema (database in InfluxDB terms)
	_, err := sm.pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{database}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to create schema: %w", err)
	}

	// Create metadata table
	tableName := pgx.Identifier{database, "_timeflux_metadata"}.Sanitize()
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			measurement TEXT NOT NULL,
			column_name TEXT NOT NULL,
			column_type TEXT NOT NULL,
			is_tag BOOLEAN NOT NULL,
			PRIMARY KEY (measurement, column_name)
		)
	`, tableName))
	if err != nil {
		return fmt.Errorf("failed to create metadata table: %w", err)
	}

	// Create index on is_tag for faster SHOW TAG KEYS / SHOW FIELD KEYS queries
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s ON %s (is_tag, measurement)
	`, pgx.Identifier{database + "_timeflux_metadata_is_tag_idx"}.Sanitize(), tableName))
	if err != nil {
		return fmt.Errorf("failed to create metadata index: %w", err)
	}

	return nil
}

func (sm *SchemaManager) ensureTable(ctx context.Context, database, measurement string) error {
	// Create table with time column
	_, err := sm.pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			time TIMESTAMPTZ NOT NULL
		)
	`, pgx.Identifier{database, measurement}.Sanitize()))
	if err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}

	// Convert to hypertable (idempotent - will succeed if already a hypertable)
	tableName := pgx.Identifier{database, measurement}.Sanitize()
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(`
		SELECT create_hypertable('%s', 'time', if_not_exists => TRUE)
	`, strings.ReplaceAll(tableName, `"`, ``)))
	if err != nil {
		// Check for specific error codes
		errStr := err.Error()
		if !strings.Contains(errStr, "already a hypertable") &&
			!strings.Contains(errStr, "TS110") { // TimescaleDB error code for already a hypertable
			return fmt.Errorf("failed to create hypertable for %s.%s: %w", database, measurement, err)
		}
		// Hypertable already exists, continue
	}

	return nil
}

func (sm *SchemaManager) ensureTagColumn(ctx context.Context, database, measurement, tag string) error {
	tx, err := sm.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := sm.ensureTagColumnTx(ctx, tx, database, measurement, tag); err != nil {
		return err
	}

	// Create index in critical path (old behavior, kept for compatibility)
	indexName := fmt.Sprintf("%s_%s_idx", measurement, tag)
	_, err = tx.Exec(ctx, fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (%s, time DESC)",
		pgx.Identifier{indexName}.Sanitize(),
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{tag}.Sanitize(),
	))
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	return tx.Commit(ctx)
}

// ensureTagColumnTx adds a tag column within a transaction (without index)
func (sm *SchemaManager) ensureTagColumnTx(ctx context.Context, tx pgx.Tx, database, measurement, tag string) error {
	// Add column
	_, err := tx.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT",
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{tag}.Sanitize(),
	))
	if err != nil {
		return fmt.Errorf("failed to add tag column: %w", err)
	}

	// Update metadata
	_, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (measurement, column_name, column_type, is_tag)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (measurement, column_name) DO UPDATE SET is_tag = $4
	`, pgx.Identifier{database, "_timeflux_metadata"}.Sanitize()),
		measurement, tag, "TEXT", true)
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	return nil
}

func (sm *SchemaManager) ensureFieldColumn(ctx context.Context, database, measurement, field, sqlType string) error {
	tx, err := sm.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := sm.ensureFieldColumnTx(ctx, tx, database, measurement, field, sqlType); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// ensureFieldColumnTx adds a field column within a transaction
func (sm *SchemaManager) ensureFieldColumnTx(ctx context.Context, tx pgx.Tx, database, measurement, field, sqlType string) error {
	// Add column
	_, err := tx.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{field}.Sanitize(),
		sqlType,
	))
	if err != nil {
		return fmt.Errorf("failed to add field column: %w", err)
	}

	// Update metadata
	_, err = tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (measurement, column_name, column_type, is_tag)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (measurement, column_name) DO UPDATE SET column_type = $3, is_tag = $4
	`, pgx.Identifier{database, "_timeflux_metadata"}.Sanitize()),
		measurement, field, sqlType, false)
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	return nil
}

// GetSchema returns the schema for a measurement (for query translation)
func (sm *SchemaManager) GetSchema(database, measurement string) (*MeasurementSchema, bool) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	dbSchemas, ok := sm.schemas[database]
	if !ok {
		return nil, false
	}

	schema, ok := dbSchemas[measurement]
	return schema, ok
}

// GetAllMeasurements returns all measurements for a database
func (sm *SchemaManager) GetAllMeasurements(database string) []string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	dbSchemas, ok := sm.schemas[database]
	if !ok {
		return nil
	}

	measurements := make([]string, 0, len(dbSchemas))
	for measurement := range dbSchemas {
		measurements = append(measurements, measurement)
	}
	return measurements
}

func postgresTypeToSQL(pgType string) string {
	switch pgType {
	case "double precision":
		return "DOUBLE PRECISION"
	case "bigint":
		return "BIGINT"
	case "boolean":
		return "BOOLEAN"
	case "text", "character varying":
		return "TEXT"
	default:
		return "TEXT"
	}
}

