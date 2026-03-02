package write

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/metrics"
	"github.com/penguinpowernz/timeflux/schema"
)

// Handler handles InfluxDB write requests
type Handler struct {
	pool                *pgxpool.Pool
	schemaManager       *schema.SchemaManager
	walBuffer           *WALBuffer // optional, nil if WAL disabled
	autoCreateDatabases bool
}

// NewHandler creates a new write handler
func NewHandler(pool *pgxpool.Pool, schemaManager *schema.SchemaManager, autoCreateDatabases bool) *Handler {
	return &Handler{
		pool:                pool,
		schemaManager:       schemaManager,
		walBuffer:           nil, // set by SetWALBuffer if enabled
		autoCreateDatabases: autoCreateDatabases,
	}
}

// SetWALBuffer enables WAL buffering for this handler
func (h *Handler) SetWALBuffer(walBuffer *WALBuffer) {
	h.walBuffer = walBuffer
}

// Handle processes write requests using Gin context
// POST /write?db={database}
func (h *Handler) Handle(c *gin.Context) {
	start := time.Now()
	m := metrics.Global()
	m.WriteRequests.Add(1)

	defer func() {
		m.WriteDuration.Record(time.Since(start))
	}()

	// Get database parameter
	database := c.Query("db")
	if database == "" {
		c.String(http.StatusBadRequest, "database parameter required")
		return
	}

	// Validate database name
	if err := validateIdentifier(database); err != nil {
		log.Printf("Invalid database name '%s': %v", database, err)
		c.String(http.StatusBadRequest, "invalid database name")
		return
	}

	// Auto-create database if enabled
	if h.autoCreateDatabases {
		if err := h.ensureDatabaseExists(c.Request.Context(), database); err != nil {
			log.Printf("Error ensuring database exists: %v", err)
			c.String(http.StatusInternalServerError, "Failed to create database: %v", err)
			return
		}
	}

	// Read request body
	body, err := c.GetRawData()
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		c.String(http.StatusBadRequest, "Failed to read request body")
		return
	}

	// Parse line protocol
	points, err := ParseBatch(string(body))
	if err != nil {
		log.Printf("Error parsing line protocol: %v", err)
		c.String(http.StatusBadRequest, "Failed to parse line protocol: %v", err)
		return
	}

	if len(points) == 0 {
		c.Status(http.StatusNoContent)
		return
	}

	// If WAL is enabled, append to WAL and return immediately
	if h.walBuffer != nil {
		if err := h.walBuffer.Append(database, body); err != nil {
			log.Printf("Error appending to WAL: %v", err)
			m.WriteErrors.Add(1)
			c.String(http.StatusInternalServerError, "Failed to write to WAL: %v", err)
			return
		}
		// Return success immediately (data will be processed by background workers)
		c.Status(http.StatusNoContent)
		return
	}

	// Synchronous write path (WAL disabled)
	// Group points by measurement
	pointsByMeasurement := make(map[string][]*Point)
	for _, point := range points {
		pointsByMeasurement[point.Measurement] = append(pointsByMeasurement[point.Measurement], point)
	}

	// Write each measurement batch with timeout
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	// Parallelize writes across measurements
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex

	for measurement, measurementPoints := range pointsByMeasurement {
		wg.Add(1)
		go func(meas string, pts []*Point) {
			defer wg.Done()
			if err := h.writePoints(ctx, database, meas, pts); err != nil {
				log.Printf("Error writing points to %s.%s: %v", database, meas, err)
				errMu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				errMu.Unlock()
			} else {
				m.PointsWritten.Add(uint64(len(pts)))
			}
		}(measurement, measurementPoints)
	}

	wg.Wait()

	// Check for errors
	if firstErr != nil {
		m.WriteErrors.Add(1)
		c.String(http.StatusInternalServerError, "Failed to write data: %v", firstErr)
		return
	}

	c.Status(http.StatusNoContent)
}

func (h *Handler) writePoints(ctx context.Context, database, measurement string, points []*Point) error {
	if len(points) == 0 {
		return nil
	}

	// Validate measurement name
	if err := validateIdentifier(measurement); err != nil {
		return fmt.Errorf("invalid measurement name '%s': %w", measurement, err)
	}

	// Collect all unique tags and fields from this batch
	allTags := make(map[string]bool)
	allFields := make(map[string]string)

	for _, point := range points {
		for tag := range point.Tags {
			allTags[tag] = true
		}
		for field, value := range point.Fields {
			fieldType := string(GetFieldType(value))
			if existing, ok := allFields[field]; ok {
				// If we see conflicting types, prefer the more general one
				// Type hierarchy: TEXT > DOUBLE PRECISION > BIGINT > BOOLEAN
				if existing != fieldType {
					allFields[field] = resolveFieldType(existing, fieldType)
				}
			} else {
				allFields[field] = fieldType
			}
		}
	}

	// Ensure schema exists
	if err := h.schemaManager.EnsureSchema(ctx, database, measurement, allTags, allFields); err != nil {
		return fmt.Errorf("failed to ensure schema: %w", err)
	}

	// Build column list (time + tags + fields in sorted order for consistency)
	columns := []string{"time"}
	tagColumns := make([]string, 0, len(allTags))
	for tag := range allTags {
		tagColumns = append(tagColumns, tag)
	}
	fieldColumns := make([]string, 0, len(allFields))
	for field := range allFields {
		fieldColumns = append(fieldColumns, field)
	}

	columns = append(columns, tagColumns...)
	columns = append(columns, fieldColumns...)

	// Use COPY for bulk insert with streaming source
	tableName := pgx.Identifier{database, measurement}.Sanitize()

	// Create streaming copy source (avoids materializing all rows)
	copySource := &pointsCopySource{
		points:  points,
		columns: columns,
		idx:     -1,
	}

	copyCount, err := h.pool.CopyFrom(
		ctx,
		pgx.Identifier{database, measurement},
		columns,
		copySource,
	)
	if err != nil {
		return fmt.Errorf("COPY failed for %s: %w", tableName, err)
	}

	log.Printf("Wrote %d rows to %s.%s", copyCount, database, measurement)
	return nil
}

// pointsCopySource implements pgx.CopyFromSource to stream rows without materializing
type pointsCopySource struct {
	points  []*Point
	columns []string
	idx     int
	rowBuf  []interface{} // reuse buffer
}

func (p *pointsCopySource) Next() bool {
	p.idx++
	return p.idx < len(p.points)
}

func (p *pointsCopySource) Values() ([]interface{}, error) {
	if p.idx >= len(p.points) {
		return nil, fmt.Errorf("no more rows")
	}

	point := p.points[p.idx]

	// Reuse buffer if possible
	if p.rowBuf == nil {
		p.rowBuf = make([]interface{}, len(p.columns))
	}

	for j, col := range p.columns {
		switch col {
		case "time":
			p.rowBuf[j] = point.Timestamp
		default:
			// Check if it's a tag
			if val, ok := point.Tags[col]; ok {
				p.rowBuf[j] = val
			} else if val, ok := point.Fields[col]; ok {
				p.rowBuf[j] = val
			} else {
				p.rowBuf[j] = nil
			}
		}
	}

	return p.rowBuf, nil
}

func (p *pointsCopySource) Err() error {
	return nil
}

// Batch insert using parameterized query (fallback if COPY doesn't work)
// resolveFieldType determines the most general type between two field types
// Type hierarchy: TEXT > DOUBLE PRECISION > BIGINT > BOOLEAN
func resolveFieldType(type1, type2 string) string {
	typeRank := map[string]int{
		string(FieldTypeBool):   1,
		string(FieldTypeInt):    2,
		string(FieldTypeFloat):  3,
		string(FieldTypeString): 4,
	}

	rank1 := typeRank[type1]
	rank2 := typeRank[type2]

	if rank1 > rank2 {
		return type1
	}
	return type2
}

// ensureDatabaseExists creates the PostgreSQL schema if it doesn't exist
func (h *Handler) ensureDatabaseExists(ctx context.Context, database string) error {
	// Check if schema already exists
	var exists bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM information_schema.schemata WHERE schema_name = $1
		)
	`, database).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check schema existence: %w", err)
	}

	if exists {
		return nil
	}

	// Create schema
	_, err = h.pool.Exec(ctx, fmt.Sprintf("CREATE SCHEMA %s", pgx.Identifier{database}.Sanitize()))
	if err != nil {
		// Check for PostgreSQL error code 42P06 (duplicate schema)
		if !strings.Contains(err.Error(), "already exists") && !strings.Contains(err.Error(), "42P06") {
			return fmt.Errorf("failed to create schema: %w", err)
		}
		// Schema already exists, continue
	}

	log.Printf("Auto-created database (schema): %s", database)
	return nil
}

// validateIdentifier validates database, measurement, and column names to prevent SQL injection
// Allows only alphanumeric characters, underscores, and must start with a letter or underscore
func validateIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("identifier cannot be empty")
	}

	// Check length (PostgreSQL limit is 63 bytes)
	if len(name) > 63 {
		return fmt.Errorf("identifier too long (max 63 characters)")
	}

	// Must start with letter or underscore
	firstChar := name[0]
	if !((firstChar >= 'a' && firstChar <= 'z') ||
		(firstChar >= 'A' && firstChar <= 'Z') ||
		firstChar == '_') {
		return fmt.Errorf("identifier must start with letter or underscore")
	}

	// Rest can be alphanumeric or underscore
	for i := 1; i < len(name); i++ {
		c := name[i]
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '_') {
			return fmt.Errorf("identifier contains invalid character: %c", c)
		}
	}

	return nil
}

