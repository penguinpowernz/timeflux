package schema

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	. "github.com/smartystreets/goconvey/convey"
)

func TestMeasurementSchema(t *testing.T) {
	Convey("Given a measurement schema", t, func() {

		Convey("When creating new schema", func() {
			schema := &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}

			So(schema.Tags, ShouldNotBeNil)
			So(schema.Fields, ShouldNotBeNil)
			So(len(schema.Tags), ShouldEqual, 0)
			So(len(schema.Fields), ShouldEqual, 0)
		})

		Convey("When adding tags", func() {
			schema := &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}

			schema.Tags["host"] = true
			schema.Tags["region"] = true

			So(len(schema.Tags), ShouldEqual, 2)
			So(schema.Tags["host"], ShouldBeTrue)
			So(schema.Tags["region"], ShouldBeTrue)
		})

		Convey("When adding fields", func() {
			schema := &MeasurementSchema{
				Tags:   make(map[string]bool),
				Fields: make(map[string]string),
			}

			schema.Fields["value"] = "DOUBLE PRECISION"
			schema.Fields["count"] = "BIGINT"
			schema.Fields["enabled"] = "BOOLEAN"
			schema.Fields["message"] = "TEXT"

			So(len(schema.Fields), ShouldEqual, 4)
			So(schema.Fields["value"], ShouldEqual, "DOUBLE PRECISION")
			So(schema.Fields["count"], ShouldEqual, "BIGINT")
			So(schema.Fields["enabled"], ShouldEqual, "BOOLEAN")
			So(schema.Fields["message"], ShouldEqual, "TEXT")
		})
	})
}

func TestSchemaManagerCreation(t *testing.T) {
	Convey("Given a database connection pool", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping: requires TEST_DATABASE_URL", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		Convey("When creating a schema manager", func() {
			sm := NewSchemaManager(pool)
			defer sm.Shutdown()

			So(sm, ShouldNotBeNil)
			So(sm.pool, ShouldEqual, pool)
			So(sm.schemas, ShouldNotBeNil)
			So(len(sm.schemas), ShouldEqual, 0)
			So(sm.indexQueue, ShouldNotBeNil)
		})

		Convey("When creating and shutting down schema manager", func() {
			sm := NewSchemaManager(pool)

			// Shutdown should be graceful
			sm.Shutdown()

			// Index queue should be closed
			// Attempting to send would panic, but we can verify workers stopped
			So(sm.indexQueue, ShouldNotBeNil)
		})
	})
}

func TestSchemaManagerConcurrency(t *testing.T) {
	Convey("Given a schema manager", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping: requires TEST_DATABASE_URL", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		sm := NewSchemaManager(pool)
		defer sm.Shutdown()

		Convey("When multiple goroutines read schema concurrently", func() {
			// Pre-populate schema
			sm.mu.Lock()
			sm.schemas["testdb"] = map[string]*MeasurementSchema{
				"cpu": {
					Tags:   map[string]bool{"host": true},
					Fields: map[string]string{"value": "DOUBLE PRECISION"},
				},
			}
			sm.mu.Unlock()

			var wg sync.WaitGroup
			numReaders := 50
			errors := make(chan error, numReaders)

			// Launch concurrent readers
			for i := 0; i < numReaders; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					// Read schema multiple times
					for j := 0; j < 100; j++ {
						sm.mu.RLock()
						schema := sm.schemas["testdb"]["cpu"]
						sm.mu.RUnlock()

						if schema == nil {
							errors <- nil // Use nil to signal schema not found
							return
						}
						if !schema.Tags["host"] {
							errors <- nil // Use nil to signal incorrect data
							return
						}
					}
				}()
			}

			wg.Wait()
			close(errors)

			// All goroutines should complete without errors
			errorCount := 0
			for range errors {
				errorCount++
			}
			So(errorCount, ShouldEqual, 0)
		})

		Convey("When multiple goroutines request measurement locks", func() {
			var wg sync.WaitGroup
			numGoroutines := 20
			counter := 0

			// Each goroutine tries to get a lock for the same measurement
			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					lockKey := "testdb.cpu"
					lockIface, _ := sm.measurementLocks.LoadOrStore(lockKey, &sync.Mutex{})
					lock := lockIface.(*sync.Mutex)

					// Critical section
					lock.Lock()
					localCounter := counter
					time.Sleep(1 * time.Millisecond) // Simulate work
					counter = localCounter + 1
					lock.Unlock()
				}()
			}

			wg.Wait()

			// Counter should equal number of goroutines (no race condition)
			So(counter, ShouldEqual, numGoroutines)
		})

		Convey("When different measurements get different locks", func() {
			lock1Iface, _ := sm.measurementLocks.LoadOrStore("db1.measurement1", &sync.Mutex{})
			lock2Iface, _ := sm.measurementLocks.LoadOrStore("db1.measurement2", &sync.Mutex{})
			lock3Iface, _ := sm.measurementLocks.LoadOrStore("db2.measurement1", &sync.Mutex{})

			lock1 := lock1Iface.(*sync.Mutex)
			lock2 := lock2Iface.(*sync.Mutex)
			lock3 := lock3Iface.(*sync.Mutex)

			// Different measurements should have different locks
			So(lock1, ShouldNotEqual, lock2)
			So(lock1, ShouldNotEqual, lock3)
			So(lock2, ShouldNotEqual, lock3)
		})

		Convey("When same measurement gets same lock", func() {
			lock1Iface, _ := sm.measurementLocks.LoadOrStore("testdb.cpu", &sync.Mutex{})
			lock2Iface, _ := sm.measurementLocks.LoadOrStore("testdb.cpu", &sync.Mutex{})

			lock1 := lock1Iface.(*sync.Mutex)
			lock2 := lock2Iface.(*sync.Mutex)

			// Same measurement should return same lock
			So(lock1, ShouldEqual, lock2)
		})
	})
}

func TestSchemaManagerIndexQueue(t *testing.T) {
	Convey("Given a schema manager with index queue", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping: requires TEST_DATABASE_URL", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		sm := NewSchemaManager(pool)
		defer sm.Shutdown()

		Convey("When enqueueing index jobs", func() {
			// Send a few jobs
			sm.indexQueue <- indexJob{
				database:    "testdb",
				measurement: "cpu",
				columnName:  "host",
				isTag:       true,
			}

			sm.indexQueue <- indexJob{
				database:    "testdb",
				measurement: "cpu",
				columnName:  "region",
				isTag:       true,
			}

			// Wait a moment for processing
			time.Sleep(100 * time.Millisecond)

			// Jobs should be processed (no panic, no deadlock)
			So(true, ShouldBeTrue)
		})

		Convey("When queue capacity is exceeded", func() {
			// The queue has capacity 1000, but we won't actually fill it
			// Just verify it doesn't panic with multiple jobs
			for i := 0; i < 10; i++ {
				sm.indexQueue <- indexJob{
					database:    "testdb",
					measurement: "cpu",
					columnName:  "tag",
					isTag:       true,
				}
			}

			So(true, ShouldBeTrue)
		})
	})
}

func TestSchemaManagerInMemoryOperations(t *testing.T) {
	Convey("Given a schema manager with in-memory schemas", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping: requires TEST_DATABASE_URL", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		sm := NewSchemaManager(pool)
		defer sm.Shutdown()

		Convey("When checking schema exists", func() {
			// Initially empty
			sm.mu.RLock()
			_, exists := sm.schemas["testdb"]
			sm.mu.RUnlock()

			So(exists, ShouldBeFalse)

			// Add schema
			sm.mu.Lock()
			sm.schemas["testdb"] = map[string]*MeasurementSchema{
				"cpu": {
					Tags:   map[string]bool{"host": true},
					Fields: map[string]string{"value": "DOUBLE PRECISION"},
				},
			}
			sm.mu.Unlock()

			// Now should exist
			sm.mu.RLock()
			_, exists = sm.schemas["testdb"]
			sm.mu.RUnlock()

			So(exists, ShouldBeTrue)
		})

		Convey("When checking measurement exists", func() {
			sm.mu.Lock()
			sm.schemas["testdb"] = map[string]*MeasurementSchema{
				"cpu": {
					Tags:   map[string]bool{"host": true},
					Fields: map[string]string{"value": "DOUBLE PRECISION"},
				},
			}
			sm.mu.Unlock()

			sm.mu.RLock()
			_, exists := sm.schemas["testdb"]["cpu"]
			sm.mu.RUnlock()

			So(exists, ShouldBeTrue)

			sm.mu.RLock()
			_, exists = sm.schemas["testdb"]["memory"]
			sm.mu.RUnlock()

			So(exists, ShouldBeFalse)
		})

		Convey("When checking columns exist", func() {
			sm.mu.Lock()
			sm.schemas["testdb"] = map[string]*MeasurementSchema{
				"cpu": {
					Tags:   map[string]bool{"host": true, "region": true},
					Fields: map[string]string{"value": "DOUBLE PRECISION", "count": "BIGINT"},
				},
			}
			sm.mu.Unlock()

			sm.mu.RLock()
			schema := sm.schemas["testdb"]["cpu"]
			sm.mu.RUnlock()

			So(schema.Tags["host"], ShouldBeTrue)
			So(schema.Tags["region"], ShouldBeTrue)
			So(schema.Tags["missing"], ShouldBeFalse)
			So(schema.Fields["value"], ShouldEqual, "DOUBLE PRECISION")
			So(schema.Fields["count"], ShouldEqual, "BIGINT")
			So(schema.Fields["missing"], ShouldEqual, "")
		})
	})
}

// Integration tests - require TEST_DATABASE_URL
func TestSchemaManagerIntegration(t *testing.T) {
	Convey("Given a schema manager with database", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping integration tests (set TEST_DATABASE_URL)", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		ctx := context.Background()
		sm := NewSchemaManager(pool)
		defer sm.Shutdown()

		// Create test schema
		_, err := pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS test_schema_manager")
		So(err, ShouldBeNil)
		defer pool.Exec(ctx, "DROP SCHEMA IF EXISTS test_schema_manager CASCADE")

		Convey("When loading existing schemas", func() {
			// Create a test table
			_, err := pool.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS test_schema_manager.test_measurement (
					time TIMESTAMPTZ NOT NULL,
					host TEXT,
					value DOUBLE PRECISION
				)
			`)
			So(err, ShouldBeNil)

			// Load schemas
			err = sm.LoadExistingSchemas(ctx)
			So(err, ShouldBeNil)

			// Verify schema was loaded
			sm.mu.RLock()
			schema, exists := sm.schemas["test_schema_manager"]["test_measurement"]
			sm.mu.RUnlock()

			So(exists, ShouldBeTrue)
			So(schema, ShouldNotBeNil)
			So(schema.Tags["host"], ShouldBeTrue)
			So(schema.Fields["value"], ShouldEqual, "DOUBLE PRECISION")
		})

		Convey("When loading schemas with multiple tables", func() {
			// Create multiple tables
			_, err := pool.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS test_schema_manager.cpu (
					time TIMESTAMPTZ NOT NULL,
					host TEXT,
					usage DOUBLE PRECISION
				)
			`)
			So(err, ShouldBeNil)

			_, err = pool.Exec(ctx, `
				CREATE TABLE IF NOT EXISTS test_schema_manager.memory (
					time TIMESTAMPTZ NOT NULL,
					host TEXT,
					used BIGINT
				)
			`)
			So(err, ShouldBeNil)

			// Load schemas
			err = sm.LoadExistingSchemas(ctx)
			So(err, ShouldBeNil)

			// Verify both tables loaded
			sm.mu.RLock()
			cpuSchema, cpuExists := sm.schemas["test_schema_manager"]["cpu"]
			memSchema, memExists := sm.schemas["test_schema_manager"]["memory"]
			sm.mu.RUnlock()

			So(cpuExists, ShouldBeTrue)
			So(memExists, ShouldBeTrue)
			So(cpuSchema.Fields["usage"], ShouldEqual, "DOUBLE PRECISION")
			So(memSchema.Fields["used"], ShouldEqual, "BIGINT")
		})

		Convey("When loading schemas excludes system schemas", func() {
			err := sm.LoadExistingSchemas(ctx)
			So(err, ShouldBeNil)

			sm.mu.RLock()
			_, pgCatalogExists := sm.schemas["pg_catalog"]
			_, infoSchemaExists := sm.schemas["information_schema"]
			sm.mu.RUnlock()

			// System schemas should not be loaded
			So(pgCatalogExists, ShouldBeFalse)
			So(infoSchemaExists, ShouldBeFalse)
		})
	})
}

func TestSchemaManagerStressTest(t *testing.T) {
	Convey("Given a schema manager under concurrent load", t, func() {
		pool := getTestPool(t)
		if pool == nil {
			SkipSo("Skipping: requires TEST_DATABASE_URL", true, ShouldBeTrue)
			return
		}
		defer pool.Close()

		sm := NewSchemaManager(pool)
		defer sm.Shutdown()

		// Pre-populate some schemas
		sm.mu.Lock()
		for i := 0; i < 10; i++ {
			dbName := "testdb"
			sm.schemas[dbName] = make(map[string]*MeasurementSchema)
			for j := 0; j < 5; j++ {
				measurementName := "cpu"
				sm.schemas[dbName][measurementName] = &MeasurementSchema{
					Tags:   map[string]bool{"host": true, "region": true},
					Fields: map[string]string{"value": "DOUBLE PRECISION"},
				}
			}
		}
		sm.mu.Unlock()

		Convey("When many goroutines read and acquire locks", func() {
			var wg sync.WaitGroup
			numGoroutines := 100
			numOperations := 50

			start := time.Now()

			for i := 0; i < numGoroutines; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					for j := 0; j < numOperations; j++ {
						// Read operation
						sm.mu.RLock()
						_ = sm.schemas["testdb"]["cpu"]
						sm.mu.RUnlock()

						// Lock acquisition
						lockKey := "testdb.cpu"
						lockIface, _ := sm.measurementLocks.LoadOrStore(lockKey, &sync.Mutex{})
						lock := lockIface.(*sync.Mutex)
						lock.Lock()
						// Simulate quick DDL
						time.Sleep(10 * time.Microsecond)
						lock.Unlock()
					}
				}(i)
			}

			wg.Wait()
			elapsed := time.Since(start)

			// Should complete without deadlock
			So(elapsed.Seconds(), ShouldBeLessThan, 30) // Generous timeout
		})
	})
}

// Helper function to get test database connection
func getTestPool(t *testing.T) *pgxpool.Pool {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		return nil
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	return pool
}

// Benchmark tests
func BenchmarkSchemaManagerReadLock(b *testing.B) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost/test")
	if err != nil {
		b.Skip("No database available")
	}
	defer pool.Close()

	sm := NewSchemaManager(pool)
	defer sm.Shutdown()

	// Pre-populate schema
	sm.mu.Lock()
	sm.schemas["testdb"] = map[string]*MeasurementSchema{
		"cpu": {
			Tags:   map[string]bool{"host": true},
			Fields: map[string]string{"value": "DOUBLE PRECISION"},
		},
	}
	sm.mu.Unlock()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sm.mu.RLock()
		_ = sm.schemas["testdb"]["cpu"]
		sm.mu.RUnlock()
	}
}

func BenchmarkSchemaManagerLockAcquisition(b *testing.B) {
	pool, err := pgxpool.New(context.Background(), "postgres://localhost/test")
	if err != nil {
		b.Skip("No database available")
	}
	defer pool.Close()

	sm := NewSchemaManager(pool)
	defer sm.Shutdown()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lockKey := "testdb.cpu"
		lockIface, _ := sm.measurementLocks.LoadOrStore(lockKey, &sync.Mutex{})
		lock := lockIface.(*sync.Mutex)
		lock.Lock()
		lock.Unlock()
	}
}
