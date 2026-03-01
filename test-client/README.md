# Timeflux Test Client

A comprehensive test tool using InfluxDB's official v1 client to validate the Timeflux facade.

## Usage

```bash
# Build
go build -o test-client

# Run (ensure Timeflux is running on localhost:8086)
./test-client
```

## What It Tests

### Write Operations
- Basic single field writes
- Multiple fields with different data types (float, int, string, bool)
- Tagged writes
- Batch writes (100 points)
- All supported data types

### Query Operations
- Simple SELECT *
- SELECT with WHERE clause
- Aggregations: MEAN, COUNT, SUM, MIN, MAX
- GROUP BY time() buckets
- GROUP BY tag
- Schema introspection: SHOW MEASUREMENTS, SHOW TAG KEYS, SHOW FIELD KEYS

## Output Format

The tool provides both human-readable and machine-readable (JSON) output:

1. Real-time test progress with ✓/✗ indicators
2. Summary statistics
3. Full JSON report with all test results and timings

## Example Output

```
=== Timeflux Facade Test Suite ===

✓ [12.3ms] BasicWrite (Write single point with one field)
✓ [8.1ms] MultiFieldWrite (Write point with multiple fields of different types)
...

=== Test Summary ===
Total:    16
Passed:   16 ✓
Failed:   0 ✗
Duration: 523.4ms

=== JSON Output ===
{
  "results": [
    {
      "name": "BasicWrite",
      "description": "Write single point with one field",
      "success": true,
      "duration": "12.3ms"
    },
    ...
  ],
  "summary": {
    "total": 16,
    "passed": 16,
    "failed": 0,
    "duration": "523.4ms"
  }
}
```
