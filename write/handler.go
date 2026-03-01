package write

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/metrics"
	"github.com/penguinpowernz/timeflux/schema"
)

// Handler handles InfluxDB write requests
type Handler struct {
	pool          *pgxpool.Pool
	schemaManager *schema.SchemaManager
}

// NewHandler creates a new write handler
func NewHandler(pool *pgxpool.Pool, schemaManager *schema.SchemaManager) *Handler {
	return &Handler{
		pool:          pool,
		schemaManager: schemaManager,
	}
}

// ServeHTTP handles HTTP requests to the write endpoint
// POST /write?db={database}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	m := metrics.Global()
	m.WriteRequests.Add(1)

	defer func() {
		m.WriteDuration.Record(time.Since(start))
	}()

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get database parameter
	database := r.URL.Query().Get("db")
	if database == "" {
		http.Error(w, "database parameter required", http.StatusBadRequest)
		return
	}

	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Parse line protocol
	points, err := ParseBatch(string(body))
	if err != nil {
		log.Printf("Error parsing line protocol: %v", err)
		http.Error(w, fmt.Sprintf("Failed to parse line protocol: %v", err), http.StatusBadRequest)
		return
	}

	if len(points) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Group points by measurement
	pointsByMeasurement := make(map[string][]*Point)
	for _, point := range points {
		pointsByMeasurement[point.Measurement] = append(pointsByMeasurement[point.Measurement], point)
	}

	// Write each measurement batch with timeout
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	for measurement, measurementPoints := range pointsByMeasurement {
		if err := h.writePoints(ctx, database, measurement, measurementPoints); err != nil {
			log.Printf("Error writing points to %s.%s: %v", database, measurement, err)
			m.WriteErrors.Add(1)
			http.Error(w, fmt.Sprintf("Failed to write data: %v", err), http.StatusInternalServerError)
			return
		}
		m.PointsWritten.Add(uint64(len(measurementPoints)))
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) writePoints(ctx context.Context, database, measurement string, points []*Point) error {
	if len(points) == 0 {
		return nil
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

	// Use COPY for bulk insert
	tableName := pgx.Identifier{database, measurement}.Sanitize()
	copyColumns := make([]string, len(columns))
	for i, col := range columns {
		copyColumns[i] = pgx.Identifier{col}.Sanitize()
	}

	// Start COPY
	copySource := pgx.CopyFromRows(makeRows(points, columns))
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

func makeRows(points []*Point, columns []string) [][]interface{} {
	rows := make([][]interface{}, len(points))

	for i, point := range points {
		row := make([]interface{}, len(columns))
		for j, col := range columns {
			switch col {
			case "time":
				row[j] = point.Timestamp
			default:
				// Check if it's a tag
				if val, ok := point.Tags[col]; ok {
					row[j] = val
				} else if val, ok := point.Fields[col]; ok {
					row[j] = val
				} else {
					row[j] = nil
				}
			}
		}
		rows[i] = row
	}

	return rows
}

// CopyFromRows adapter
type copyFromRowsAdapter struct {
	rows [][]interface{}
	idx  int
}

func (c *copyFromRowsAdapter) Next() bool {
	c.idx++
	return c.idx < len(c.rows)
}

func (c *copyFromRowsAdapter) Values() ([]interface{}, error) {
	if c.idx >= len(c.rows) {
		return nil, fmt.Errorf("no more rows")
	}
	return c.rows[c.idx], nil
}

func (c *copyFromRowsAdapter) Err() error {
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

func (h *Handler) insertPointsBatch(ctx context.Context, database, measurement string, points []*Point, columns []string) error {
	if len(points) == 0 {
		return nil
	}

	// Build INSERT statement
	var b strings.Builder
	b.WriteString("INSERT INTO ")
	b.WriteString(pgx.Identifier{database, measurement}.Sanitize())
	b.WriteString(" (")

	for i, col := range columns {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(pgx.Identifier{col}.Sanitize())
	}

	b.WriteString(") VALUES ")

	// Add value placeholders
	valueIdx := 1
	for i := range points {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString("(")
		for j := range columns {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString(fmt.Sprintf("$%d", valueIdx))
			valueIdx++
		}
		b.WriteString(")")
	}

	// Flatten values
	values := make([]interface{}, 0, len(points)*len(columns))
	for _, point := range points {
		for _, col := range columns {
			switch col {
			case "time":
				values = append(values, point.Timestamp)
			default:
				if val, ok := point.Tags[col]; ok {
					values = append(values, val)
				} else if val, ok := point.Fields[col]; ok {
					values = append(values, val)
				} else {
					values = append(values, nil)
				}
			}
		}
	}

	// Execute batch insert
	_, err := h.pool.Exec(ctx, b.String(), values...)
	return err
}
