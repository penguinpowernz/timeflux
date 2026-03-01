package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
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

// ServeHTTP handles HTTP requests to the query endpoint
// GET or POST /query?db={database}&q={query}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get database parameter
	database := r.URL.Query().Get("db")
	if database == "" {
		http.Error(w, "database parameter required", http.StatusBadRequest)
		return
	}

	// Get query parameter
	query := r.URL.Query().Get("q")
	if query == "" {
		// Try to get from POST body
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Failed to parse form", http.StatusBadRequest)
				return
			}
			query = r.FormValue("q")
		}
	}

	if query == "" {
		http.Error(w, "query parameter required", http.StatusBadRequest)
		return
	}

	// Translate InfluxQL to SQL
	translator := NewTranslator(database)
	sql, err := translator.Translate(query)
	if err != nil {
		log.Printf("Error translating query: %v", err)
		h.sendErrorResponse(w, fmt.Sprintf("Failed to translate query: %v", err))
		return
	}

	log.Printf("Translated query: %s", sql)

	// Execute SQL query
	ctx := r.Context()
	results, err := h.executeQuery(ctx, sql)
	if err != nil {
		log.Printf("Error executing query: %v", err)
		h.sendErrorResponse(w, fmt.Sprintf("Failed to execute query: %v", err))
		return
	}

	// Send response in InfluxDB format
	h.sendSuccessResponse(w, results)
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

func (h *Handler) sendSuccessResponse(w http.ResponseWriter, result *QueryResult) {
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func (h *Handler) sendErrorResponse(w http.ResponseWriter, errMsg string) {
	response := InfluxDBResponse{
		Results: []InfluxDBResult{
			{
				StatementID: 0,
				Error:       errMsg,
			},
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK) // InfluxDB returns 200 even for query errors

	if err := json.NewEncoder(w).Encode(response); err != nil {
		log.Printf("Error encoding error response: %v", err)
	}
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
