package write

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Point represents a single InfluxDB data point
type Point struct {
	Measurement string
	Tags        map[string]string
	Fields      map[string]interface{}
	Timestamp   time.Time
}

// FieldType represents the SQL type for a field
type FieldType string

const (
	FieldTypeFloat   FieldType = "DOUBLE PRECISION"
	FieldTypeInt     FieldType = "BIGINT"
	FieldTypeString  FieldType = "TEXT"
	FieldTypeBool    FieldType = "BOOLEAN"
)

// ParseLineProtocol parses InfluxDB line protocol into Point structs
// Format: measurement[,tag=value...] field=value[,field=value...] [timestamp]
func ParseLineProtocol(line string) (*Point, error) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return nil, nil // Empty line or comment
	}

	// Split by spaces (but respect escaping and quotes)
	parts := splitLineProtocol(line)
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid line protocol: expected at least 2 parts, got %d", len(parts))
	}

	point := &Point{
		Tags:   make(map[string]string),
		Fields: make(map[string]interface{}),
	}

	// Parse measurement and tags
	measurementPart := parts[0]
	if err := parseMeasurementAndTags(measurementPart, point); err != nil {
		return nil, err
	}

	// Parse fields
	fieldsPart := parts[1]
	if err := parseFields(fieldsPart, point); err != nil {
		return nil, err
	}

	// Parse timestamp (optional)
	if len(parts) >= 3 {
		ts, err := parseTimestamp(parts[2])
		if err != nil {
			return nil, err
		}
		point.Timestamp = ts
	} else {
		point.Timestamp = time.Now()
	}

	if len(point.Fields) == 0 {
		return nil, fmt.Errorf("no fields in line protocol")
	}

	return point, nil
}

// ParseBatch parses multiple lines of line protocol
func ParseBatch(data string) ([]*Point, error) {
	lines := strings.Split(data, "\n")
	points := make([]*Point, 0, len(lines))

	for i, line := range lines {
		point, err := ParseLineProtocol(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if point != nil {
			points = append(points, point)
		}
	}

	return points, nil
}

func splitLineProtocol(line string) []string {
	var parts []string
	var current strings.Builder
	inQuotes := false
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == '"' {
			inQuotes = !inQuotes
			current.WriteByte(ch)
			continue
		}

		if ch == ' ' && !inQuotes {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func parseMeasurementAndTags(part string, point *Point) error {
	// Split by comma (but respect escaping)
	segments := splitByComma(part)
	if len(segments) == 0 {
		return fmt.Errorf("empty measurement")
	}

	point.Measurement = unescapeKey(segments[0])

	// Parse tags
	for i := 1; i < len(segments); i++ {
		kv := strings.SplitN(segments[i], "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid tag format: %s", segments[i])
		}
		key := unescapeKey(kv[0])
		value := unescapeValue(kv[1])
		point.Tags[key] = value
	}

	return nil
}

func parseFields(part string, point *Point) error {
	// Split by comma (but respect escaping and quotes)
	segments := splitFieldSegments(part)

	for _, segment := range segments {
		kv := strings.SplitN(segment, "=", 2)
		if len(kv) != 2 {
			return fmt.Errorf("invalid field format: %s", segment)
		}
		key := unescapeKey(kv[0])
		value, err := parseFieldValue(kv[1])
		if err != nil {
			return fmt.Errorf("field %s: %w", key, err)
		}
		point.Fields[key] = value
	}

	return nil
}

func parseFieldValue(s string) (interface{}, error) {
	s = strings.TrimSpace(s)

	// String (quoted)
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		unquoted := s[1 : len(s)-1]
		unquoted = strings.ReplaceAll(unquoted, `\"`, `"`)
		unquoted = strings.ReplaceAll(unquoted, `\\`, `\`)
		return unquoted, nil
	}

	// Boolean
	if s == "t" || s == "T" || s == "true" || s == "True" || s == "TRUE" {
		return true, nil
	}
	if s == "f" || s == "F" || s == "false" || s == "False" || s == "FALSE" {
		return false, nil
	}

	// Integer (ends with 'i')
	if strings.HasSuffix(s, "i") {
		intStr := s[:len(s)-1]
		val, err := strconv.ParseInt(intStr, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer: %s", s)
		}
		return val, nil
	}

	// Float (default for bare numbers)
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number: %s", s)
	}
	return val, nil
}

func parseTimestamp(s string) (time.Time, error) {
	// InfluxDB timestamps are in nanoseconds since epoch
	ns, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp: %s", s)
	}
	return time.Unix(0, ns), nil
}

func splitByComma(s string) []string {
	var parts []string
	var current strings.Builder
	escaped := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			escaped = true
			continue
		}

		if ch == ',' {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func splitFieldSegments(s string) []string {
	var parts []string
	var current strings.Builder
	escaped := false
	inQuotes := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if escaped {
			current.WriteByte(ch)
			escaped = false
			continue
		}

		if ch == '\\' {
			current.WriteByte(ch)
			escaped = true
			continue
		}

		if ch == '"' {
			inQuotes = !inQuotes
			current.WriteByte(ch)
			continue
		}

		if ch == ',' && !inQuotes {
			if current.Len() > 0 {
				parts = append(parts, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func unescapeKey(s string) string {
	s = strings.ReplaceAll(s, `\ `, ` `)
	s = strings.ReplaceAll(s, `\,`, `,`)
	s = strings.ReplaceAll(s, `\=`, `=`)
	return s
}

func unescapeValue(s string) string {
	s = strings.ReplaceAll(s, `\ `, ` `)
	s = strings.ReplaceAll(s, `\,`, `,`)
	s = strings.ReplaceAll(s, `\=`, `=`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	return s
}

// GetFieldType returns the SQL type for a field value
func GetFieldType(value interface{}) FieldType {
	switch value.(type) {
	case int64:
		return FieldTypeInt
	case float64:
		return FieldTypeFloat
	case string:
		return FieldTypeString
	case bool:
		return FieldTypeBool
	default:
		return FieldTypeString
	}
}
