package query

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/auth"
	"github.com/penguinpowernz/timeflux/metrics"
)

// Handler handles InfluxDB query requests
type Handler struct {
	pool *pgxpool.Pool
}

// NewHandler creates a new query handler
func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{
		pool: pool,
	}
}

// Handle processes query requests using Gin context
// GET or POST /query?db={database}&q={query}
func (h *Handler) Handle(c *gin.Context) {
	start := time.Now()
	m := metrics.Global()
	m.QueryRequests.Add(1)

	defer func() {
		m.QueryDuration.Record(time.Since(start))
	}()

	// Get query parameter
	query := c.Query("q")
	if query == "" {
		// Try to get from POST form
		if c.Request.Method == http.MethodPost {
			query = c.PostForm("q")
		}
	}

	if query == "" {
		log.Printf("%s /query: Missing query parameter", c.Request.Method)
		h.sendErrorResponse(c, "query parameter required")
		return
	}

	// Get database parameter (optional for some queries)
	database := c.Query("db")

	log.Printf("%s /query: db=%s, query=%s", c.Request.Method, database, query)

	// Prevent access to auth tables
	if auth.IsAuthTableQuery(query) {
		log.Printf("Attempt to query auth tables blocked: %s", query)
		m.QueryErrors.Add(1)
		h.sendErrorResponse(c, "Access to authentication tables is forbidden")
		return
	}

	// Check if database is required for this query
	if database == "" && !isSystemQuery(query) {
		log.Printf("%s /query: Missing database parameter for non-system query", c.Request.Method)
		h.sendErrorResponse(c, "database parameter required")
		return
	}

	// Translate InfluxQL to SQL
	translator := NewTranslator(database)
	sql, err := translator.Translate(query)
	if err != nil {
		log.Printf("Error translating query: %v", err)
		m.QueryErrors.Add(1)
		h.sendErrorResponse(c, fmt.Sprintf("Failed to translate query: %v", err))
		return
	}

	log.Printf("Translated query: %s", sql)

	// Execute SQL query
	ctx := c.Request.Context()
	results, err := h.executeQuery(ctx, sql)
	if err != nil {
		log.Printf("Error executing query: %v", err)
		m.QueryErrors.Add(1)
		h.sendErrorResponse(c, fmt.Sprintf("Failed to execute query: %v", err))
		return
	}

	// Send response in InfluxDB format
	h.sendSuccessResponse(c, results)
}

// isSystemQuery checks if a query can run without a database parameter
func isSystemQuery(query string) bool {
	// Normalize query to uppercase for comparison
	upperQuery := strings.ToUpper(strings.TrimSpace(query))

	// Queries that don't require a database parameter
	systemQueries := []string{
		"SHOW DATABASES",
		"CREATE DATABASE",
		"DROP DATABASE",
		"SHOW SERIES",
		"DROP SERIES",
		"DROP MEASUREMENT",
	}

	for _, sysQuery := range systemQueries {
		if strings.HasPrefix(upperQuery, sysQuery) {
			return true
		}
	}

	return false
}

func (h *Handler) executeQuery(ctx context.Context, sql string) (*QueryResult, error) {
	rows, err := h.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	defer rows.Close()

	// Get column descriptions
	fieldDescriptions := rows.FieldDescriptions()
	columns := make([]string, len(fieldDescriptions))
	for i, fd := range fieldDescriptions {
		columns[i] = string(fd.Name)
	}

	// Read all rows
	var values [][]interface{}
	for rows.Next() {
		rowValues, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("failed to read row: %w", err)
		}
		values = append(values, rowValues)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("row iteration error: %w", err)
	}

	result := &QueryResult{
		Columns: columns,
		Values:  values,
	}

	return result, nil
}

func (h *Handler) sendSuccessResponse(c *gin.Context, result *QueryResult) {
	response := InfluxDBResponse{
		Results: []InfluxDBResult{
			{
				StatementID: 0,
				Series: []InfluxDBSeries{
					{
						Columns: result.Columns,
						Values:  result.Values,
					},
				},
			},
		},
	}

	c.JSON(http.StatusOK, response)
}

func (h *Handler) sendErrorResponse(c *gin.Context, errMsg string) {
	response := InfluxDBResponse{
		Results: []InfluxDBResult{
			{
				StatementID: 0,
				Error:       errMsg,
			},
		},
	}

	// InfluxDB returns 200 even for query errors
	c.JSON(http.StatusOK, response)
}

// QueryResult represents the result of a SQL query
type QueryResult struct {
	Columns []string
	Values  [][]interface{}
}

// InfluxDBResponse represents the InfluxDB JSON response format
type InfluxDBResponse struct {
	Results []InfluxDBResult `json:"results"`
}

// InfluxDBResult represents a single query result
type InfluxDBResult struct {
	StatementID int              `json:"statement_id"`
	Series      []InfluxDBSeries `json:"series,omitempty"`
	Error       string           `json:"error,omitempty"`
}

// InfluxDBSeries represents a series in the result
type InfluxDBSeries struct {
	Name    string          `json:"name,omitempty"`
	Columns []string        `json:"columns"`
	Values  [][]interface{} `json:"values,omitempty"`
}
