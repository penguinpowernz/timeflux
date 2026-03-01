package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics holds application metrics
type Metrics struct {
	// Write metrics
	WriteRequests     atomic.Uint64
	WriteErrors       atomic.Uint64
	PointsWritten     atomic.Uint64
	WriteDuration     *DurationStats

	// Query metrics
	QueryRequests     atomic.Uint64
	QueryErrors       atomic.Uint64
	QueryDuration     *DurationStats

	// Schema metrics
	SchemaEvolutions  atomic.Uint64
	SchemaCacheHits   atomic.Uint64
	SchemaCacheMisses atomic.Uint64

	// WAL metrics
	WALWrites         atomic.Uint64
	WALBytes          atomic.Uint64
	WALWriteErrors    atomic.Uint64
	WALCorruptions    atomic.Uint64
	WALReplaySuccess  atomic.Uint64
	WALReplayFailures atomic.Uint64
	WALWriteDuration  *DurationStats

	// Connection pool metrics
	PoolAcquireCount  atomic.Uint64
	PoolAcquireDuration *DurationStats
}

// DurationStats tracks duration statistics
type DurationStats struct {
	mu       sync.RWMutex
	total    time.Duration
	count    uint64
	min      time.Duration
	max      time.Duration
}

var globalMetrics = &Metrics{
	WriteDuration:       &DurationStats{min: time.Hour},
	QueryDuration:       &DurationStats{min: time.Hour},
	WALWriteDuration:    &DurationStats{min: time.Hour},
	PoolAcquireDuration: &DurationStats{min: time.Hour},
}

// Global returns the global metrics instance
func Global() *Metrics {
	return globalMetrics
}

// RecordDuration records a duration measurement
func (d *DurationStats) Record(duration time.Duration) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.total += duration
	d.count++

	if duration < d.min {
		d.min = duration
	}
	if duration > d.max {
		d.max = duration
	}
}

// Stats returns current statistics
func (d *DurationStats) Stats() (avg, min, max time.Duration, count uint64) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.count == 0 {
		return 0, 0, 0, 0
	}

	avg = d.total / time.Duration(d.count)
	return avg, d.min, d.max, d.count
}

// Snapshot returns a copy of current metrics
func (m *Metrics) Snapshot() map[string]interface{} {
	writeAvg, writeMin, writeMax, writeCount := m.WriteDuration.Stats()
	queryAvg, queryMin, queryMax, queryCount := m.QueryDuration.Stats()
	walAvg, walMin, walMax, walCount := m.WALWriteDuration.Stats()
	poolAvg, poolMin, poolMax, poolCount := m.PoolAcquireDuration.Stats()

	return map[string]interface{}{
		"writes": map[string]interface{}{
			"requests":         m.WriteRequests.Load(),
			"errors":           m.WriteErrors.Load(),
			"points_written":   m.PointsWritten.Load(),
			"duration_avg_ms":  writeAvg.Milliseconds(),
			"duration_min_ms":  writeMin.Milliseconds(),
			"duration_max_ms":  writeMax.Milliseconds(),
			"duration_count":   writeCount,
		},
		"queries": map[string]interface{}{
			"requests":        m.QueryRequests.Load(),
			"errors":          m.QueryErrors.Load(),
			"duration_avg_ms": queryAvg.Milliseconds(),
			"duration_min_ms": queryMin.Milliseconds(),
			"duration_max_ms": queryMax.Milliseconds(),
			"duration_count":  queryCount,
		},
		"schema": map[string]interface{}{
			"evolutions":   m.SchemaEvolutions.Load(),
			"cache_hits":   m.SchemaCacheHits.Load(),
			"cache_misses": m.SchemaCacheMisses.Load(),
		},
		"wal": map[string]interface{}{
			"writes":          m.WALWrites.Load(),
			"bytes":           m.WALBytes.Load(),
			"write_errors":    m.WALWriteErrors.Load(),
			"corruptions":     m.WALCorruptions.Load(),
			"replay_success":  m.WALReplaySuccess.Load(),
			"replay_failures": m.WALReplayFailures.Load(),
			"duration_avg_us": walAvg.Microseconds(),
			"duration_min_us": walMin.Microseconds(),
			"duration_max_us": walMax.Microseconds(),
			"duration_count":  walCount,
		},
		"pool": map[string]interface{}{
			"acquire_count":   m.PoolAcquireCount.Load(),
			"acquire_avg_ms":  poolAvg.Milliseconds(),
			"acquire_min_ms":  poolMin.Milliseconds(),
			"acquire_max_ms":  poolMax.Milliseconds(),
			"acquire_count_total": poolCount,
		},
	}
}
