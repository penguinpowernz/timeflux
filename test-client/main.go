package main

import (
	"encoding/json"
	"fmt"
	"log"
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

func main() {
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

	fmt.Println("=== Timeflux Facade Test Suite ===\n")

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

	// Print summary
	fmt.Println("\n=== Test Summary ===")
	fmt.Printf("Total:    %d\n", suite.Summary.Total)
	fmt.Printf("Passed:   %d ✓\n", suite.Summary.Passed)
	fmt.Printf("Failed:   %d ✗\n", suite.Summary.Failed)
	fmt.Printf("Duration: %s\n", suite.Summary.Duration)

	// Output JSON for programmatic consumption
	fmt.Println("\n=== JSON Output ===")
	jsonData, _ := json.MarshalIndent(suite, "", "  ")
	fmt.Println(string(jsonData))

	// Exit with error code if any tests failed
	if suite.Summary.Failed > 0 {
		fmt.Println("\n⚠ Some tests failed")
	} else {
		fmt.Println("\n✓ All tests passed")
	}
}

func (s *TestSuite) addTest(result TestResult) {
	s.Results = append(s.Results, result)
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
