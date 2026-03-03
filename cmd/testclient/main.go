package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"strings"
	"time"

	client "github.com/influxdata/influxdb1-client/v2"
)

const (
	serverURL = "http://localhost:8086"
	database  = "testdb"
)

type TestResult struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Success     bool        `json:"success"`
	Error       string      `json:"error,omitempty"`
	Data        interface{} `json:"data,omitempty"`
	Duration    string      `json:"duration"`
}

type TestSuite struct {
	Results []TestResult `json:"results"`
	Summary Summary      `json:"summary"`
}

type Summary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Duration string `json:"duration"`
}

var (
	markdownMode bool
	jsonMode     bool
)

func main() {
	flag.BoolVar(&markdownMode, "m", false, "Output results as a markdown table")
	flag.BoolVar(&jsonMode, "j", false, "Output results as JSON")
	flag.Parse()

	suite := &TestSuite{Results: []TestResult{}}
	startTime := time.Now()

	// Create client
	c, err := client.NewHTTPClient(client.HTTPConfig{
		Addr: serverURL,
	})
	if err != nil {
		log.Fatalf("Failed to create InfluxDB client: %v", err)
	}
	defer c.Close()

	if !markdownMode && !jsonMode {
		fmt.Println("=== Timeflux Facade Test Suite ===\n")
	}

	// Test 1: Write basic point with single field
	suite.addTest(testBasicWrite(c))

	// Test 2: Write point with multiple fields (different types)
	suite.addTest(testMultiFieldWrite(c))

	// Test 3: Write points with tags
	suite.addTest(testTaggedWrite(c))

	// Test 4: Write batch of points
	suite.addTest(testBatchWrite(c))

	// Test 5: Write with all data types
	suite.addTest(testAllDataTypes(c))

	// Small delay to ensure data is written
	time.Sleep(500 * time.Millisecond)

	// Test 6: Simple SELECT query
	suite.addTest(testSimpleSelect(c))

	// Test 7: SELECT with WHERE clause
	suite.addTest(testSelectWithWhere(c))

	// Test 8: SELECT with aggregation (MEAN)
	suite.addTest(testSelectMean(c))

	// Test 9: SELECT with GROUP BY time()
	suite.addTest(testGroupByTime(c))

	// Test 10: SELECT with GROUP BY tags
	suite.addTest(testGroupByTag(c))

	// Test 11: SELECT COUNT
	suite.addTest(testCount(c))

	// Test 12: SELECT SUM
	suite.addTest(testSum(c))

	// Test 13: SELECT MIN/MAX
	suite.addTest(testMinMax(c))

	// Test 14: SHOW MEASUREMENTS
	suite.addTest(testShowMeasurements(c))

	// Test 15: SHOW TAG KEYS
	suite.addTest(testShowTagKeys(c))

	// Test 16: SHOW FIELD KEYS
	suite.addTest(testShowFieldKeys(c))

	// Test 17: CREATE DATABASE
	suite.addTest(testCreateDatabase(c))

	// Test 18: SHOW DATABASES
	suite.addTest(testShowDatabases(c))

	// Test 19: SHOW SERIES
	suite.addTest(testShowSeries(c))

	// Test 20: DROP SERIES
	suite.addTest(testDropSeries(c))

	// Test 21: DROP MEASUREMENT
	suite.addTest(testDropMeasurement(c))

	// Test 22: FIRST and LAST functions
	suite.addTest(testFirstLast(c))

	// Test 23: PERCENTILE function
	suite.addTest(testPercentile(c))

	// Test 24: Multiple aggregations in one query
	suite.addTest(testMultipleAggregations(c))

	// Test 25: Arithmetic operations in SELECT
	suite.addTest(testArithmeticOperations(c))

	// Test 26: Complex WHERE with AND/OR
	suite.addTest(testComplexWhere(c))

	// Test 27: SHOW TAG VALUES
	suite.addTest(testShowTagValues(c))

	// Test 28: SELECT with LIMIT
	suite.addTest(testLimit(c))

	// Test 29: SELECT with OFFSET
	suite.addTest(testOffset(c))

	// Test 30: SELECT with ORDER BY
	suite.addTest(testOrderBy(c))

	// Test 31: SELECT with time range
	suite.addTest(testTimeRange(c))

	// Test 32: GROUP BY time with multiple intervals
	suite.addTest(testGroupByTimeIntervals(c))

	// Test 33: GROUP BY multiple tags
	suite.addTest(testGroupByMultipleTags(c))

	// Test 34: NOW() function in WHERE
	suite.addTest(testNowFunction(c))

	// Test 35: Boolean field queries
	suite.addTest(testBooleanFields(c))

	// Test 36: String field queries
	suite.addTest(testStringFields(c))

	// Test 37: Negative numbers and zero
	suite.addTest(testNegativeNumbers(c))

	// Test 38: DROP SERIES
	suite.addTest(testDropSeries(c))

	// Test 39: DROP MEASUREMENT
	suite.addTest(testDropMeasurement(c))

	// Test 40: DROP DATABASE
	suite.addTest(testDropDatabase(c))

	// Phase 1 function tests - write numeric data first
	suite.addTest(testWriteNumericData(c))
	time.Sleep(500 * time.Millisecond)

	// Test 41: STDDEV function
	suite.addTest(testStddev(c))

	// Test 42: MEDIAN function
	suite.addTest(testMedian(c))

	// Test 43: SPREAD function
	suite.addTest(testSpread(c))

	// Test 44: ABS function
	suite.addTest(testAbs(c))

	// Test 45: CEIL function
	suite.addTest(testCeil(c))

	// Test 46: FLOOR function
	suite.addTest(testFloor(c))

	// Test 47: ROUND function
	suite.addTest(testRound(c))

	// Test 48: SQRT function
	suite.addTest(testSqrt(c))

	// Test 49: POW function
	suite.addTest(testPow(c))

	// Test 50: EXP function
	suite.addTest(testExp(c))

	// Test 51: LN function
	suite.addTest(testLn(c))

	// Test 52: LOG2 function
	suite.addTest(testLog2(c))

	// Test 53: LOG10 function
	suite.addTest(testLog10(c))

	// Test 54: LOG with base
	suite.addTest(testLogBase(c))

	// Test 55: SIN function
	suite.addTest(testSin(c))

	// Test 56: COS function
	suite.addTest(testCos(c))

	// Test 57: TAN function
	suite.addTest(testTan(c))

	// Test 58: ASIN function
	suite.addTest(testAsin(c))

	// Test 59: ACOS function
	suite.addTest(testAcos(c))

	// Test 60: ATAN function
	suite.addTest(testAtan(c))

	// Test 61: ATAN2 function
	suite.addTest(testAtan2(c))

	// Calculate summary
	suite.Summary.Total = len(suite.Results)
	suite.Summary.Duration = time.Since(startTime).String()
	for _, r := range suite.Results {
		if r.Success {
			suite.Summary.Passed++
		} else {
			suite.Summary.Failed++
		}
	}

	switch {
	case jsonMode:
		jsonData, _ := json.MarshalIndent(suite, "", "  ")
		fmt.Println(string(jsonData))
	case markdownMode:
		printMarkdownTable(suite)
	default:
		fmt.Println("\n=== Test Summary ===")
		fmt.Printf("Total:    %d\n", suite.Summary.Total)
		fmt.Printf("Passed:   %d ✓\n", suite.Summary.Passed)
		fmt.Printf("Failed:   %d ✗\n", suite.Summary.Failed)
		fmt.Printf("Duration: %s\n", suite.Summary.Duration)
		if suite.Summary.Failed > 0 {
			fmt.Println("\n⚠ Some tests failed")
		} else {
			fmt.Println("\n✓ All tests passed")
		}
	}
}

func printMarkdownTable(suite *TestSuite) {
	fmt.Println("# Timeflux Facade Test Results\n")
	fmt.Printf("| Status | Test | Description | Duration |\n")
	fmt.Printf("|--------|------|-------------|----------|\n")
	for _, r := range suite.Results {
		icon := "✅"
		if !r.Success {
			icon = "❌"
		}
		desc := r.Description
		if !r.Success && r.Error != "" {
			desc = r.Description + "<br>**Error:** " + r.Error
		}
		// Escape pipe characters in description for markdown table
		desc = strings.ReplaceAll(desc, "|", "\\|")
		fmt.Printf("| %s | %s | %s | %s |\n", icon, r.Name, desc, r.Duration)
	}
	fmt.Printf("\n**Summary:** %d/%d passed in %s\n", suite.Summary.Passed, suite.Summary.Total, suite.Summary.Duration)
}

func (s *TestSuite) addTest(result TestResult) {
	s.Results = append(s.Results, result)
	if markdownMode || jsonMode {
		return
	}
	status := "✓"
	if !result.Success {
		status = "✗"
	}
	fmt.Printf("%s [%s] %s (%s)\n", status, result.Duration, result.Name, result.Description)
	if !result.Success && result.Error != "" {
		fmt.Printf("  Error: %s\n", result.Error)
	}
}

func testBasicWrite(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "BasicWrite",
		Description: "Write single point with one field",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	tags := map[string]string{}
	fields := map[string]interface{}{
		"value": 42.5,
	}
	pt, _ := client.NewPoint("basic_metric", tags, fields, time.Now())
	bp.AddPoint(pt)

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	return result
}

func testMultiFieldWrite(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "MultiFieldWrite",
		Description: "Write point with multiple fields of different types",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	fields := map[string]interface{}{
		"cpu_usage":    75.5,
		"memory_bytes": int64(1024000),
		"is_healthy":   true,
		"status":       "running",
	}
	pt, _ := client.NewPoint("system_stats", map[string]string{}, fields, time.Now())
	bp.AddPoint(pt)

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	return result
}

func testTaggedWrite(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "TaggedWrite",
		Description: "Write points with tags",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	tags := map[string]string{
		"host":   "server1",
		"region": "us-west",
	}
	fields := map[string]interface{}{
		"temperature": 72.3,
	}
	pt, _ := client.NewPoint("environment", tags, fields, time.Now())
	bp.AddPoint(pt)

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	return result
}

func testBatchWrite(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "BatchWrite",
		Description: "Write batch of 100 points",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	baseTime := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 100; i++ {
		tags := map[string]string{
			"sensor_id": fmt.Sprintf("sensor_%d", i%5),
		}
		fields := map[string]interface{}{
			"value": float64(50 + i),
		}
		pt, _ := client.NewPoint("sensor_data", tags, fields, baseTime.Add(time.Duration(i)*time.Minute))
		bp.AddPoint(pt)
	}

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]int{"points_written": 100}
	return result
}

func testAllDataTypes(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "AllDataTypes",
		Description: "Write point with all supported data types",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	fields := map[string]interface{}{
		"float_field":   3.14159,
		"int_field":     int64(42),
		"string_field":  "hello world",
		"bool_field":    false,
		"negative_int":  int64(-999),
		"large_float":   1234567.89,
	}
	pt, _ := client.NewPoint("datatypes", map[string]string{"test": "types"}, fields, time.Now())
	bp.AddPoint(pt)

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	return result
}

func testSimpleSelect(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "SimpleSelect",
		Description: "SELECT * FROM measurement",
	}

	q := client.NewQuery("SELECT * FROM basic_metric", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testSelectWithWhere(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "SelectWithWhere",
		Description: "SELECT with WHERE clause filtering tags",
	}

	q := client.NewQuery("SELECT * FROM environment WHERE host='server1'", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	return result
}

func testSelectMean(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "SelectMean",
		Description: "SELECT MEAN() aggregation",
	}

	q := client.NewQuery("SELECT MEAN(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 1 {
			result.Data = map[string]interface{}{
				"mean": response.Results[0].Series[0].Values[0][1],
			}
		}
	}
	return result
}

func testGroupByTime(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "GroupByTime",
		Description: "SELECT with GROUP BY time(5m)",
	}

	q := client.NewQuery("SELECT MEAN(value) FROM sensor_data WHERE time > now() - 2h GROUP BY time(5m)", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"buckets": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testGroupByTag(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "GroupByTag",
		Description: "SELECT with GROUP BY tag",
	}

	q := client.NewQuery("SELECT MEAN(value) FROM sensor_data GROUP BY sensor_id", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 {
		result.Data = map[string]interface{}{
			"groups": len(response.Results[0].Series),
		}
	}
	return result
}

func testCount(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "Count",
		Description: "SELECT COUNT(*)",
	}

	q := client.NewQuery("SELECT COUNT(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 1 {
			result.Data = map[string]interface{}{
				"count": response.Results[0].Series[0].Values[0][1],
			}
		}
	}
	return result
}

func testSum(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "Sum",
		Description: "SELECT SUM()",
	}

	q := client.NewQuery("SELECT SUM(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 1 {
			result.Data = map[string]interface{}{
				"sum": response.Results[0].Series[0].Values[0][1],
			}
		}
	}
	return result
}

func testMinMax(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "MinMax",
		Description: "SELECT MIN() and MAX()",
	}

	q := client.NewQuery("SELECT MIN(value), MAX(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 2 {
			result.Data = map[string]interface{}{
				"min": response.Results[0].Series[0].Values[0][1],
				"max": response.Results[0].Series[0].Values[0][2],
			}
		}
	}
	return result
}

func testShowMeasurements(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowMeasurements",
		Description: "SHOW MEASUREMENTS",
	}

	q := client.NewQuery("SHOW MEASUREMENTS", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		measurements := []string{}
		for _, val := range response.Results[0].Series[0].Values {
			if len(val) > 0 {
				measurements = append(measurements, val[0].(string))
			}
		}
		result.Data = map[string]interface{}{
			"measurements": measurements,
			"count":        len(measurements),
		}
	}
	return result
}

func testShowTagKeys(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowTagKeys",
		Description: "SHOW TAG KEYS",
	}

	q := client.NewQuery("SHOW TAG KEYS FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		tagKeys := []string{}
		for _, val := range response.Results[0].Series[0].Values {
			if len(val) > 0 {
				tagKeys = append(tagKeys, val[0].(string))
			}
		}
		result.Data = map[string]interface{}{
			"tag_keys": tagKeys,
		}
	}
	return result
}

func testShowFieldKeys(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowFieldKeys",
		Description: "SHOW FIELD KEYS",
	}

	q := client.NewQuery("SHOW FIELD KEYS FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		fieldKeys := []string{}
		for _, val := range response.Results[0].Series[0].Values {
			if len(val) > 0 {
				fieldKeys = append(fieldKeys, val[0].(string))
			}
		}
		result.Data = map[string]interface{}{
			"field_keys": fieldKeys,
		}
	}
	return result
}

func testCreateDatabase(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "CreateDatabase",
		Description: "CREATE DATABASE",
	}

	q := client.NewQuery("CREATE DATABASE test_created_db", "", "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{
		"database": "test_created_db",
	}
	return result
}

func testShowDatabases(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowDatabases",
		Description: "SHOW DATABASES",
	}

	q := client.NewQuery("SHOW DATABASES", "", "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		databases := []string{}
		for _, val := range response.Results[0].Series[0].Values {
			if len(val) > 0 {
				databases = append(databases, val[0].(string))
			}
		}
		result.Data = map[string]interface{}{
			"databases": databases,
			"count":     len(databases),
		}
	}
	return result
}

func testShowSeries(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowSeries",
		Description: "SHOW SERIES",
	}

	q := client.NewQuery("SHOW SERIES", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"series_count": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testDropSeries(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "DropSeries",
		Description: "DROP SERIES with WHERE clause",
	}

	// First write some test data with specific tags
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  database,
		Precision: "ns",
	})

	tags := map[string]string{"test_tag": "drop_me"}
	fields := map[string]interface{}{"value": 123.45}
	pt, _ := client.NewPoint("drop_test", tags, fields, time.Now())
	bp.AddPoint(pt)
	c.Write(bp)

	time.Sleep(100 * time.Millisecond)

	// Now drop the series
	q := client.NewQuery("DROP SERIES FROM drop_test WHERE test_tag='drop_me'", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{
		"status": "series dropped",
	}
	return result
}

func testDropMeasurement(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "DropMeasurement",
		Description: "DROP MEASUREMENT",
	}

	// First create a test measurement
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database:  database,
		Precision: "ns",
	})

	tags := map[string]string{"host": "test"}
	fields := map[string]interface{}{"value": 99.99}
	pt, _ := client.NewPoint("temp_measurement", tags, fields, time.Now())
	bp.AddPoint(pt)
	c.Write(bp)

	time.Sleep(100 * time.Millisecond)

	// Now drop the measurement
	q := client.NewQuery("DROP MEASUREMENT temp_measurement", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{
		"status": "measurement dropped",
	}
	return result
}

func testFirstLast(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "FirstLast",
		Description: "SELECT FIRST() and LAST() functions",
	}

	q := client.NewQuery("SELECT FIRST(value), LAST(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 2 {
			result.Data = map[string]interface{}{
				"first": response.Results[0].Series[0].Values[0][1],
				"last":  response.Results[0].Series[0].Values[0][2],
			}
		}
	}
	return result
}

func testPercentile(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "Percentile",
		Description: "SELECT PERCENTILE() function",
	}

	q := client.NewQuery("SELECT PERCENTILE(value, 95) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		if len(response.Results[0].Series[0].Values[0]) > 1 {
			result.Data = map[string]interface{}{
				"percentile_95": response.Results[0].Series[0].Values[0][1],
			}
		}
	}
	return result
}

func testMultipleAggregations(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "MultipleAggregations",
		Description: "SELECT multiple aggregations in one query",
	}

	q := client.NewQuery("SELECT COUNT(value), MEAN(value), SUM(value), MIN(value), MAX(value) FROM sensor_data", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 && len(response.Results[0].Series[0].Values) > 0 {
		vals := response.Results[0].Series[0].Values[0]
		if len(vals) > 5 {
			result.Data = map[string]interface{}{
				"count": vals[1],
				"mean":  vals[2],
				"sum":   vals[3],
				"min":   vals[4],
				"max":   vals[5],
			}
		}
	}
	return result
}

func testArithmeticOperations(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ArithmeticOperations",
		Description: "SELECT with arithmetic operations (+, -, *, /)",
	}

	q := client.NewQuery("SELECT value * 2 AS doubled, value + 10 AS plus_ten, value / 2 AS halved FROM sensor_data LIMIT 5", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testComplexWhere(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ComplexWhere",
		Description: "SELECT with complex WHERE (AND, OR, comparison operators)",
	}

	q := client.NewQuery("SELECT * FROM sensor_data WHERE (value > 60 AND value < 80) OR sensor_id='sensor_0'", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testShowTagValues(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "ShowTagValues",
		Description: "SHOW TAG VALUES with KEY",
	}

	q := client.NewQuery("SHOW TAG VALUES FROM sensor_data WITH KEY = sensor_id", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		values := []string{}
		for _, val := range response.Results[0].Series[0].Values {
			if len(val) > 0 {
				values = append(values, fmt.Sprintf("%v", val[0]))
			}
		}
		result.Data = map[string]interface{}{
			"tag_values": values,
			"count":      len(values),
		}
	}
	return result
}

func testLimit(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "Limit",
		Description: "SELECT with LIMIT clause",
	}

	q := client.NewQuery("SELECT * FROM sensor_data LIMIT 10", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		rowCount := len(response.Results[0].Series[0].Values)
		result.Data = map[string]interface{}{
			"rows":      rowCount,
			"limit_set": 10,
		}
		// Verify LIMIT is working
		if rowCount > 10 {
			result.Error = fmt.Sprintf("LIMIT not working: got %d rows, expected <= 10", rowCount)
			result.Success = false
		}
	}
	return result
}

func testOffset(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "Offset",
		Description: "SELECT with OFFSET clause",
	}

	q := client.NewQuery("SELECT * FROM sensor_data LIMIT 5 OFFSET 10", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows":   len(response.Results[0].Series[0].Values),
			"offset": 10,
		}
	}
	return result
}

func testOrderBy(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "OrderBy",
		Description: "SELECT with ORDER BY clause",
	}

	q := client.NewQuery("SELECT * FROM sensor_data ORDER BY time DESC LIMIT 10", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows":     len(response.Results[0].Series[0].Values),
			"order_by": "time DESC",
		}
	}
	return result
}

func testTimeRange(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "TimeRange",
		Description: "SELECT with time range in WHERE",
	}

	q := client.NewQuery("SELECT * FROM sensor_data WHERE time > now() - 3h AND time < now()", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testGroupByTimeIntervals(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "GroupByTimeIntervals",
		Description: "Test different GROUP BY time() intervals (1m, 5m, 1h)",
	}

	// Test with 1 minute interval
	q := client.NewQuery("SELECT MEAN(value) FROM sensor_data WHERE time > now() - 2h GROUP BY time(1m)", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"interval_1m_buckets": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testGroupByMultipleTags(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "GroupByMultipleTags",
		Description: "SELECT with GROUP BY multiple tags",
	}

	// First write some data with multiple tags
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	for i := 0; i < 20; i++ {
		tags := map[string]string{
			"region": []string{"us-west", "us-east"}[i%2],
			"zone":   []string{"zone-a", "zone-b", "zone-c"}[i%3],
		}
		fields := map[string]interface{}{
			"latency": float64(10 + i),
		}
		pt, _ := client.NewPoint("network_stats", tags, fields, time.Now().Add(-time.Duration(i)*time.Minute))
		bp.AddPoint(pt)
	}
	c.Write(bp)

	time.Sleep(200 * time.Millisecond)

	q := client.NewQuery("SELECT MEAN(latency) FROM network_stats GROUP BY region, zone", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 {
		result.Data = map[string]interface{}{
			"groups": len(response.Results[0].Series),
		}
	}
	return result
}

func testNowFunction(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "NowFunction",
		Description: "Use NOW() function in WHERE clause",
	}

	q := client.NewQuery("SELECT * FROM sensor_data WHERE time < now() LIMIT 10", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testBooleanFields(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "BooleanFields",
		Description: "Query boolean fields with WHERE",
	}

	q := client.NewQuery("SELECT * FROM system_stats WHERE is_healthy=true", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testStringFields(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "StringFields",
		Description: "Query string fields with WHERE",
	}

	q := client.NewQuery("SELECT * FROM system_stats WHERE status='running'", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

func testNegativeNumbers(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "NegativeNumbers",
		Description: "Query with negative numbers and zero",
	}

	// Write some test data with negatives
	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	fields := map[string]interface{}{
		"temperature": -5.5,
		"pressure":    0.0,
		"altitude":    int64(-100),
	}
	pt, _ := client.NewPoint("weather", map[string]string{"station": "north"}, fields, time.Now())
	bp.AddPoint(pt)
	c.Write(bp)

	time.Sleep(200 * time.Millisecond)

	q := client.NewQuery("SELECT * FROM weather WHERE temperature < 0", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{
			"rows": len(response.Results[0].Series[0].Values),
		}
	}
	return result
}

// --- Phase 1 Function Tests ---

// testWriteNumericData writes a known dataset for math function tests
func testWriteNumericData(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "WriteNumericData",
		Description: "Write known numeric dataset for Phase 1 function tests",
	}

	bp, _ := client.NewBatchPoints(client.BatchPointsConfig{
		Database: database,
	})

	// Write values: 1, 4, 9, 16, 25 (perfect squares), and also -3.7, 3.7 (for abs/ceil/floor tests)
	// and a value in [0,1] for asin/acos
	baseTime := time.Now().Add(-10 * time.Minute)
	testValues := []float64{1.0, 4.0, 9.0, 16.0, 25.0}
	for i, v := range testValues {
		fields := map[string]interface{}{
			"value":    v,
			"neg":      -v,
			"fraction": v / 100.0, // 0.01, 0.04, ... 0.25 (in [-1,1] for asin/acos)
		}
		pt, _ := client.NewPoint("math_test", map[string]string{"run": "phase1"}, fields, baseTime.Add(time.Duration(i)*time.Minute))
		bp.AddPoint(pt)
	}

	err := c.Write(bp)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"points_written": len(testValues)}
	return result
}

func queryOneValue(c client.Client, q string) (interface{}, error) {
	query := client.NewQuery(q, database, "")
	response, err := c.Query(query)
	if err != nil {
		return nil, err
	}
	if response.Error() != nil {
		return nil, response.Error()
	}
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 &&
		len(response.Results[0].Series[0].Values) > 0 &&
		len(response.Results[0].Series[0].Values[0]) > 0 {
		row := response.Results[0].Series[0].Values[0]
		cols := response.Results[0].Series[0].Columns
		// If first column is "time", return second; otherwise return first
		if len(cols) > 0 && cols[0] == "time" && len(row) > 1 {
			return row[1], nil
		}
		return row[0], nil
	}
	return nil, fmt.Errorf("no data returned")
}

func testStddev(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Stddev", Description: "SELECT STDDEV() aggregation"}

	val, err := queryOneValue(c, "SELECT stddev(value) FROM math_test WHERE run='phase1'")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"stddev": val}
	return result
}

func testMedian(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Median", Description: "SELECT MEDIAN() aggregation"}

	val, err := queryOneValue(c, "SELECT median(value) FROM math_test WHERE run='phase1'")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"median": val}
	return result
}

func testSpread(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Spread", Description: "SELECT SPREAD() (MAX-MIN) aggregation"}

	val, err := queryOneValue(c, "SELECT spread(value) FROM math_test WHERE run='phase1'")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"spread": val}
	return result
}

func testAbs(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Abs", Description: "SELECT ABS() on negative values"}

	q := client.NewQuery("SELECT abs(neg) FROM math_test WHERE run='phase1' LIMIT 3", database, "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	if len(response.Results) > 0 && len(response.Results[0].Series) > 0 {
		result.Data = map[string]interface{}{"rows": len(response.Results[0].Series[0].Values)}
	}
	return result
}

func testCeil(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Ceil", Description: "SELECT CEIL() ceiling function"}

	val, err := queryOneValue(c, "SELECT ceil(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"ceil": val}
	return result
}

func testFloor(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Floor", Description: "SELECT FLOOR() floor function"}

	val, err := queryOneValue(c, "SELECT floor(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"floor": val}
	return result
}

func testRound(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Round", Description: "SELECT ROUND() rounding function"}

	val, err := queryOneValue(c, "SELECT round(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"round": val}
	return result
}

func testSqrt(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Sqrt", Description: "SELECT SQRT() square root function"}

	val, err := queryOneValue(c, "SELECT sqrt(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"sqrt": val}
	return result
}

func testPow(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Pow", Description: "SELECT POW(field, exponent) power function"}

	val, err := queryOneValue(c, "SELECT pow(value, 2) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"pow2": val}
	return result
}

func testExp(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Exp", Description: "SELECT EXP() exponential function"}

	val, err := queryOneValue(c, "SELECT exp(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"exp": val}
	return result
}

func testLn(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Ln", Description: "SELECT LN() natural logarithm"}

	val, err := queryOneValue(c, "SELECT ln(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"ln": val}
	return result
}

func testLog2(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Log2", Description: "SELECT LOG2() base-2 logarithm"}

	val, err := queryOneValue(c, "SELECT log2(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"log2": val}
	return result
}

func testLog10(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Log10", Description: "SELECT LOG10() base-10 logarithm"}

	val, err := queryOneValue(c, "SELECT log10(value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"log10": val}
	return result
}

func testLogBase(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "LogBase", Description: "SELECT LOG(field, base) custom base logarithm"}

	val, err := queryOneValue(c, "SELECT log(value, 4) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"log_base4": val}
	return result
}

func testSin(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Sin", Description: "SELECT SIN() sine function (radians)"}

	val, err := queryOneValue(c, "SELECT sin(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"sin": val}
	return result
}

func testCos(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Cos", Description: "SELECT COS() cosine function (radians)"}

	val, err := queryOneValue(c, "SELECT cos(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"cos": val}
	return result
}

func testTan(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Tan", Description: "SELECT TAN() tangent function (radians)"}

	val, err := queryOneValue(c, "SELECT tan(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"tan": val}
	return result
}

func testAsin(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Asin", Description: "SELECT ASIN() arcsine (input in [-1,1])"}

	// fraction values are 0.01..0.25, safe for asin
	val, err := queryOneValue(c, "SELECT asin(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"asin": val}
	return result
}

func testAcos(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Acos", Description: "SELECT ACOS() arccosine (input in [-1,1])"}

	val, err := queryOneValue(c, "SELECT acos(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"acos": val}
	return result
}

func testAtan(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Atan", Description: "SELECT ATAN() arctangent"}

	val, err := queryOneValue(c, "SELECT atan(fraction) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"atan": val}
	return result
}

func testAtan2(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{Name: "Atan2", Description: "SELECT ATAN2(y, x) two-argument arctangent"}

	val, err := queryOneValue(c, "SELECT atan2(fraction, value) FROM math_test WHERE run='phase1' LIMIT 1")
	result.Duration = time.Since(start).String()
	if err != nil {
		result.Error = err.Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{"atan2": val}
	return result
}

func testDropDatabase(c client.Client) TestResult {
	start := time.Now()
	result := TestResult{
		Name:        "DropDatabase",
		Description: "DROP DATABASE",
	}

	// First create a test database
	qCreate := client.NewQuery("CREATE DATABASE test_drop_db", "", "")
	c.Query(qCreate)

	time.Sleep(100 * time.Millisecond)

	// Now drop it
	q := client.NewQuery("DROP DATABASE test_drop_db", "", "")
	response, err := c.Query(q)
	result.Duration = time.Since(start).String()

	if err != nil {
		result.Error = err.Error()
		return result
	}

	if response.Error() != nil {
		result.Error = response.Error().Error()
		return result
	}

	result.Success = true
	result.Data = map[string]interface{}{
		"status": "database dropped",
	}
	return result
}
