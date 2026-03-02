package write

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/penguinpowernz/timeflux/metrics"
	"github.com/penguinpowernz/timeflux/schema"
	"github.com/tidwall/wal"
)

// WALBuffer provides write-ahead logging with background processing
type WALBuffer struct {
	wal           *wal.Log
	pool          *pgxpool.Pool
	schemaManager *schema.SchemaManager
	workers       []*WALWorker
	workerWg      sync.WaitGroup
	stopCh        chan struct{}
	fsyncTicker   *time.Ticker
	mu            sync.Mutex
	nextIndex     uint64 // shared read position for workers (protected by indexMu)
	indexMu       sync.Mutex
}

// WALConfig holds configuration for the WAL buffer
type WALConfig struct {
	Path              string
	NumWorkers        int
	FsyncIntervalMs   int
	SegmentSizeMB     int
	SegmentCacheSize  int
	NoSync            bool // disable fsync for testing/development
}

// WALWorker processes entries from the WAL
type WALWorker struct {
	id            int
	walBuffer     *WALBuffer
	writeHandler  *Handler
}

// NewWALBuffer creates a new WAL buffer with background workers
func NewWALBuffer(cfg WALConfig, pool *pgxpool.Pool, schemaManager *schema.SchemaManager) (*WALBuffer, error) {
	// Open WAL
	walLog, err := wal.Open(cfg.Path, &wal.Options{
		SegmentSize:      cfg.SegmentSizeMB * 1024 * 1024,
		SegmentCacheSize: cfg.SegmentCacheSize,
		NoCopy:           true,
		NoSync:           cfg.NoSync,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	// Get the first index to initialize nextIndex
	firstIndex, err := walLog.FirstIndex()
	if err != nil {
		walLog.Close()
		return nil, fmt.Errorf("failed to get first index: %w", err)
	}

	wb := &WALBuffer{
		wal:           walLog,
		pool:          pool,
		schemaManager: schemaManager,
		stopCh:        make(chan struct{}),
		fsyncTicker:   time.NewTicker(time.Duration(cfg.FsyncIntervalMs) * time.Millisecond),
		nextIndex:     firstIndex,
	}

	// Create write handler for workers to use (auto-create disabled in WAL workers, will be handled by main write path)
	writeHandler := NewHandler(pool, schemaManager, false)

	// Start worker pool
	wb.workers = make([]*WALWorker, cfg.NumWorkers)
	for i := 0; i < cfg.NumWorkers; i++ {
		wb.workers[i] = &WALWorker{
			id:           i,
			walBuffer:    wb,
			writeHandler: writeHandler,
		}
		wb.workerWg.Add(1)
		go wb.workers[i].run()
	}

	// Start fsync goroutine
	if !cfg.NoSync {
		go wb.fsyncLoop()
	}

	log.Printf("WAL buffer initialized: path=%s workers=%d fsync_interval=%dms",
		cfg.Path, cfg.NumWorkers, cfg.FsyncIntervalMs)

	return wb, nil
}

// Append adds a batch of line protocol to the WAL
func (wb *WALBuffer) Append(database string, lineProtocol []byte) error {
	m := metrics.Global()
	start := time.Now()

	// Create WAL entry with checksum
	entry := NewWALEntry(database, lineProtocol)
	data := entry.Marshal()

	// Write to WAL
	lastIndex, err := wb.wal.LastIndex()
	if err != nil {
		m.WALWriteErrors.Add(1)
		return fmt.Errorf("failed to get last index: %w", err)
	}

	err = wb.wal.Write(lastIndex+1, data)
	if err != nil {
		m.WALWriteErrors.Add(1)
		return fmt.Errorf("WAL write failed: %w", err)
	}

	m.WALWrites.Add(1)
	m.WALBytes.Add(uint64(len(data)))
	m.WALWriteDuration.Record(time.Since(start))

	return nil
}

// fsyncLoop periodically fsyncs the WAL to disk
func (wb *WALBuffer) fsyncLoop() {
	for {
		select {
		case <-wb.fsyncTicker.C:
			if err := wb.wal.Sync(); err != nil {
				log.Printf("WAL fsync failed: %v", err)
			}
		case <-wb.stopCh:
			return
		}
	}
}

// run processes WAL entries continuously
func (w *WALWorker) run() {
	defer w.walBuffer.workerWg.Done()

	log.Printf("WAL worker %d started", w.id)

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-w.walBuffer.stopCh:
			log.Printf("WAL worker %d stopping", w.id)
			return
		case <-ticker.C:
			// Get next entry to process atomically
			w.walBuffer.indexMu.Lock()
			currentIndex := w.walBuffer.nextIndex

			// Check if there are new entries
			lastIndex, err := w.walBuffer.wal.LastIndex()
			if err != nil {
				w.walBuffer.indexMu.Unlock()
				log.Printf("WAL worker %d: failed to get last index: %v", w.id, err)
				continue
			}

			// If no new entries, unlock and continue
			if currentIndex > lastIndex {
				w.walBuffer.indexMu.Unlock()
				continue
			}

			// Claim this index by incrementing nextIndex
			w.walBuffer.nextIndex++
			w.walBuffer.indexMu.Unlock()

			// Process the entry (outside of lock)
			if err := w.processEntry(currentIndex); err != nil {
				log.Printf("WAL worker %d: failed to process entry %d: %v", w.id, currentIndex, err)
				// Skip corrupted entry and continue
				metrics.Global().WALReplayFailures.Add(1)
			} else {
				metrics.Global().WALReplaySuccess.Add(1)
			}
		}
	}
}

// processEntry reads, validates, and processes a single WAL entry
func (w *WALWorker) processEntry(index uint64) error {
	m := metrics.Global()

	// Read entry from WAL
	data, err := w.walBuffer.wal.Read(index)
	if err != nil {
		return fmt.Errorf("failed to read WAL entry: %w", err)
	}

	// Unmarshal and validate checksum
	walEntry, err := UnmarshalWALEntry(data)
	if err != nil {
		m.WALCorruptions.Add(1)
		return fmt.Errorf("WAL corruption detected at index %d: %w", index, err)
	}

	// Decompress
	lineProtocol, err := walEntry.Decompress()
	if err != nil {
		return fmt.Errorf("decompression failed: %w", err)
	}

	// Parse the entry to extract database and points
	entry, err := ParseWALData(lineProtocol)
	if err != nil {
		return fmt.Errorf("failed to parse WAL data: %w", err)
	}

	// Group by measurement
	pointsByMeasurement := make(map[string][]*Point)
	for _, point := range entry.Points {
		pointsByMeasurement[point.Measurement] = append(pointsByMeasurement[point.Measurement], point)
	}

	// Write to TimescaleDB using existing handler logic
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for measurement, measurementPoints := range pointsByMeasurement {
		// Check if context is cancelled before processing
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		default:
		}

		if err := w.writeHandler.writePoints(ctx, entry.Database, measurement, measurementPoints); err != nil {
			return fmt.Errorf("failed to write points for %s.%s: %w", entry.Database, measurement, err)
		}
		m.PointsWritten.Add(uint64(len(measurementPoints)))
	}

	return nil
}

// RecoverFromWAL replays all entries in the WAL on startup
func (wb *WALBuffer) RecoverFromWAL() error {
	log.Printf("Starting WAL recovery...")

	firstIndex, err := wb.wal.FirstIndex()
	if err != nil {
		return fmt.Errorf("failed to get first index: %w", err)
	}

	lastIndex, err := wb.wal.LastIndex()
	if err != nil {
		return fmt.Errorf("failed to get last index: %w", err)
	}

	if lastIndex < firstIndex {
		log.Printf("WAL is empty, no recovery needed")
		return nil
	}

	validEntries := 0
	corruptedEntries := 0

	for index := firstIndex; index <= lastIndex; index++ {
		data, err := wb.wal.Read(index)
		if err != nil {
			log.Printf("Failed to read WAL entry %d: %v", index, err)
			continue
		}

		entry, err := UnmarshalWALEntry(data)
		if err != nil {
			log.Printf("Skipping corrupted entry at index %d: %v", index, err)
			corruptedEntries++
			continue
		}

		// Just validate, workers will process
		_, err = entry.Decompress()
		if err != nil {
			log.Printf("Entry %d decompression failed: %v", index, err)
			corruptedEntries++
			continue
		}

		validEntries++
	}

	log.Printf("WAL recovery scan complete: %d valid entries, %d corrupted entries, will be processed by workers",
		validEntries, corruptedEntries)

	if corruptedEntries > 0 {
		log.Printf("WARNING: WAL had %d corrupted entries that will be skipped", corruptedEntries)
	}

	return nil
}

// Shutdown stops the WAL buffer and waits for workers to finish
func (wb *WALBuffer) Shutdown() error {
	log.Printf("Shutting down WAL buffer...")

	// Stop workers
	close(wb.stopCh)
	wb.workerWg.Wait()

	// Stop fsync ticker
	wb.fsyncTicker.Stop()

	// Final fsync
	if err := wb.wal.Sync(); err != nil {
		log.Printf("Final WAL fsync failed: %v", err)
	}

	// Close WAL
	if err := wb.wal.Close(); err != nil {
		return fmt.Errorf("failed to close WAL: %w", err)
	}

	log.Printf("WAL buffer shutdown complete")
	return nil
}

// TruncateBefore removes WAL entries before the given index
func (wb *WALBuffer) TruncateBefore(index uint64) error {
	return wb.wal.TruncateFront(index)
}

// Stats returns current WAL statistics
func (wb *WALBuffer) Stats() map[string]interface{} {
	firstIndex, _ := wb.wal.FirstIndex()
	lastIndex, _ := wb.wal.LastIndex()

	return map[string]interface{}{
		"first_index":   firstIndex,
		"last_index":    lastIndex,
		"pending_count": lastIndex - firstIndex + 1,
		"num_workers":   len(wb.workers),
	}
}
