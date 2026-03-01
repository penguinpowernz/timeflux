package schema

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

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
	measurementLocks map[string]*sync.Mutex                   // key: "database.measurement" -> *sync.Mutex for DDL
	locksMu          sync.Mutex                               // protects measurementLocks map
	pool             *pgxpool.Pool
}

// NewSchemaManager creates a new SchemaManager
func NewSchemaManager(pool *pgxpool.Pool) *SchemaManager {
	return &SchemaManager{
		schemas:          make(map[string]map[string]*MeasurementSchema),
		measurementLocks: make(map[string]*sync.Mutex),
		pool:             pool,
	}
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

	// Build schema list for WHERE clause
	schemaList := make([]string, len(databases))
	for i, db := range databases {
		schemaList[i] = "'" + strings.ReplaceAll(db, "'", "''") + "'"
	}

	// Query all columns for all schemas at once
	query := fmt.Sprintf(`
		SELECT
			table_schema,
			table_name,
			column_name,
			data_type
		FROM information_schema.columns
		WHERE table_schema IN (%s)
			AND table_name != '_timeflux_metadata'
		ORDER BY table_schema, table_name, ordinal_position
	`, strings.Join(schemaList, ", "))

	rows, err := sm.pool.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to query columns: %w", err)
	}
	defer rows.Close()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for rows.Next() {
		var schemaName, tableName, columnName, dataType string
		if err := rows.Scan(&schemaName, &tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if columnName == "time" {
			continue // Skip the time column
		}

		if sm.schemas[schemaName] == nil {
			sm.schemas[schemaName] = make(map[string]*MeasurementSchema)
		}
		if sm.schemas[schemaName][tableName] == nil {
			sm.schemas[schemaName][tableName] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}

		// Heuristic: TEXT columns are likely tags
		sqlType := postgresTypeToSQL(dataType)
		if sqlType == "TEXT" {
			sm.schemas[schemaName][tableName].Tags[columnName] = true
		} else {
			sm.schemas[schemaName][tableName].Fields[columnName] = sqlType
		}
	}

	// Load metadata for all databases
	for _, database := range databases {
		sm.loadMetadata(ctx, database)
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

	sm.mu.Lock()
	defer sm.mu.Unlock()

	if sm.schemas[database] == nil {
		sm.schemas[database] = make(map[string]*MeasurementSchema)
	}

	for rows.Next() {
		var tableName, columnName, dataType string
		if err := rows.Scan(&tableName, &columnName, &dataType); err != nil {
			return fmt.Errorf("failed to scan column info: %w", err)
		}

		if columnName == "time" {
			continue // Skip the time column
		}

		if sm.schemas[database][tableName] == nil {
			sm.schemas[database][tableName] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}

		// Determine if it's a tag or field by checking metadata table
		// For now, we'll use a heuristic: TEXT columns without specific numeric types are likely tags
		sqlType := postgresTypeToSQL(dataType)
		if sqlType == "TEXT" {
			// Could be tag or field - check metadata table if it exists
			sm.schemas[database][tableName].Tags[columnName] = true
		} else {
			sm.schemas[database][tableName].Fields[columnName] = sqlType
		}
	}

	// Load actual tag/field distinctions from metadata table if it exists
	sm.loadMetadata(ctx, database)

	return nil
}

func (sm *SchemaManager) loadMetadata(ctx context.Context, database string) {
	rows, err := sm.pool.Query(ctx,
		fmt.Sprintf(`
			SELECT measurement, column_name, column_type, is_tag
			FROM %s._timeflux_metadata
		`, pgx.Identifier{database}.Sanitize()))
	if err != nil {
		// Metadata table doesn't exist yet
		return
	}
	defer rows.Close()

	for rows.Next() {
		var measurement, columnName, columnType string
		var isTag bool
		if err := rows.Scan(&measurement, &columnName, &columnType, &isTag); err != nil {
			continue
		}

		if sm.schemas[database][measurement] == nil {
			sm.schemas[database][measurement] = &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}
		}

		if isTag {
			sm.schemas[database][measurement].Tags[columnName] = true
			delete(sm.schemas[database][measurement].Fields, columnName)
		} else {
			sm.schemas[database][measurement].Fields[columnName] = columnType
			delete(sm.schemas[database][measurement].Tags, columnName)
		}
	}
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

	// Slow path: acquire measurement-specific lock for DDL
	lockKey := database + "." + measurement

	sm.locksMu.Lock()
	lock, exists := sm.measurementLocks[lockKey]
	if !exists {
		lock = &sync.Mutex{}
		sm.measurementLocks[lockKey] = lock
	}
	sm.locksMu.Unlock()

	lock.Lock()
	defer lock.Unlock()

	// Double-check pattern: verify another goroutine didn't already do the work
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

	// Add missing tags
	for tag := range tags {
		if err := sm.ensureTagColumn(ctx, database, measurement, tag); err != nil {
			return err
		}
	}

	// Add missing fields
	for field, fieldType := range fields {
		if err := sm.ensureFieldColumn(ctx, database, measurement, field, fieldType); err != nil {
			return err
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
		// Ignore error if already a hypertable
		if !strings.Contains(err.Error(), "already a hypertable") {
			log.Printf("Warning: failed to create hypertable for %s.%s: %v", database, measurement, err)
		}
	}

	return nil
}

func (sm *SchemaManager) ensureTagColumn(ctx context.Context, database, measurement, tag string) error {
	// Add column
	_, err := sm.pool.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s TEXT",
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{tag}.Sanitize(),
	))
	if err != nil {
		return fmt.Errorf("failed to add tag column: %w", err)
	}

	// Create index
	indexName := fmt.Sprintf("%s_%s_idx", measurement, tag)
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(
		"CREATE INDEX IF NOT EXISTS %s ON %s (%s, time DESC)",
		pgx.Identifier{indexName}.Sanitize(),
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{tag}.Sanitize(),
	))
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	// Update metadata
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(`
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
	// Add column
	_, err := sm.pool.Exec(ctx, fmt.Sprintf(
		"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s %s",
		pgx.Identifier{database, measurement}.Sanitize(),
		pgx.Identifier{field}.Sanitize(),
		sqlType,
	))
	if err != nil {
		return fmt.Errorf("failed to add field column: %w", err)
	}

	// Update metadata
	_, err = sm.pool.Exec(ctx, fmt.Sprintf(`
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

