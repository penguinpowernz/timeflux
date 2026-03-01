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
	errCh := make(chan error, len(pointsByMeasurement))

	for measurement, measurementPoints := range pointsByMeasurement {
		wg.Add(1)
		go func(meas string, pts []*Point) {
			defer wg.Done()
			if err := h.writePoints(ctx, database, meas, pts); err != nil {
				log.Printf("Error writing points to %s.%s: %v", database, meas, err)
				select {
				case errCh <- err:
				default:
					// Error channel full, log it
					log.Printf("Additional error (channel full): %v", err)
				}
			} else {
				m.PointsWritten.Add(uint64(len(pts)))
			}
		}(measurement, measurementPoints)
	}

	wg.Wait()
	close(errCh)

	// Check for errors - read first error if any
	select {
	case err := <-errCh:
		if err != nil {
			m.WriteErrors.Add(1)
			c.String(http.StatusInternalServerError, "Failed to write data: %v", err)
			return
		}
	default:
		// No errors
	}

	c.Status(http.StatusNoContent)
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
