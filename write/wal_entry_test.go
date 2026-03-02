package write

import (
	"bytes"
	"encoding/binary"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestNewWALEntry(t *testing.T) {
	Convey("Given database and line protocol data", t, func() {

		Convey("When creating a WAL entry", func() {
			database := "testdb"
			lineProtocol := []byte("cpu,host=server1 value=85.3 1620000000000000000")

			entry := NewWALEntry(database, lineProtocol)

			So(entry, ShouldNotBeNil)
			So(entry.Checksum, ShouldNotEqual, 0)
			So(entry.Length, ShouldBeGreaterThan, 0)
			So(len(entry.Data), ShouldEqual, int(entry.Length))
		})

		Convey("When creating entries with different data", func() {
			entry1 := NewWALEntry("db1", []byte("cpu value=1"))
			entry2 := NewWALEntry("db2", []byte("cpu value=2"))

			// Different data should produce different checksums
			So(entry1.Checksum, ShouldNotEqual, entry2.Checksum)
		})

		Convey("When creating entry with empty line protocol", func() {
			entry := NewWALEntry("testdb", []byte(""))

			So(entry, ShouldNotBeNil)
			So(entry.Checksum, ShouldNotEqual, 0)
		})

		Convey("When creating entry with large data", func() {
			// Generate large line protocol batch
			var largeData bytes.Buffer
			for i := 0; i < 1000; i++ {
				largeData.WriteString("cpu,host=server1 value=85.3 1620000000000000000\n")
			}

			entry := NewWALEntry("testdb", largeData.Bytes())

			So(entry, ShouldNotBeNil)
			So(entry.Length, ShouldBeGreaterThan, 0)
			// Compressed size should be less than original (snappy compression)
			So(int(entry.Length), ShouldBeLessThan, largeData.Len())
		})
	})
}

func TestWALEntryMarshalUnmarshal(t *testing.T) {
	Convey("Given a WAL entry", t, func() {
		database := "testdb"
		lineProtocol := []byte("cpu,host=server1 value=85.3 1620000000000000000")
		entry := NewWALEntry(database, lineProtocol)

		Convey("When marshaling and unmarshaling", func() {
			marshaled := entry.Marshal()

			unmarshaled, err := UnmarshalWALEntry(marshaled)

			So(err, ShouldBeNil)
			So(unmarshaled.Checksum, ShouldEqual, entry.Checksum)
			So(unmarshaled.Length, ShouldEqual, entry.Length)
			So(unmarshaled.Data, ShouldResemble, entry.Data)
		})

		Convey("When marshaling produces correct format", func() {
			marshaled := entry.Marshal()

			// Should be at least 8 bytes (header) + data
			So(len(marshaled), ShouldEqual, 8+int(entry.Length))

			// First 4 bytes should be checksum
			checksum := binary.LittleEndian.Uint32(marshaled[0:4])
			So(checksum, ShouldEqual, entry.Checksum)

			// Next 4 bytes should be length
			length := binary.LittleEndian.Uint32(marshaled[4:8])
			So(length, ShouldEqual, entry.Length)
		})

		Convey("When round-tripping multiple entries", func() {
			entries := []*WALEntry{
				NewWALEntry("db1", []byte("cpu value=1")),
				NewWALEntry("db2", []byte("mem used=1024i")),
				NewWALEntry("db3", []byte("disk,host=server1 free=50000i")),
			}

			for _, original := range entries {
				marshaled := original.Marshal()
				restored, err := UnmarshalWALEntry(marshaled)

				So(err, ShouldBeNil)
				So(restored.Checksum, ShouldEqual, original.Checksum)
				So(restored.Length, ShouldEqual, original.Length)
				So(restored.Data, ShouldResemble, original.Data)
			}
		})
	})
}

func TestUnmarshalWALEntry_Errors(t *testing.T) {
	Convey("Given invalid WAL entry data", t, func() {

		Convey("When buffer is too short", func() {
			buf := make([]byte, 7) // Less than 8 bytes

			_, err := UnmarshalWALEntry(buf)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "too short")
		})

		Convey("When buffer is empty", func() {
			buf := []byte{}

			_, err := UnmarshalWALEntry(buf)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "too short")
		})

		Convey("When length header doesn't match data size", func() {
			// Create valid entry then modify length header
			entry := NewWALEntry("testdb", []byte("cpu value=42"))
			marshaled := entry.Marshal()

			// Change the length to mismatch actual data
			binary.LittleEndian.PutUint32(marshaled[4:8], entry.Length+10)

			_, err := UnmarshalWALEntry(marshaled)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "length mismatch")
		})

		Convey("When checksum is corrupted", func() {
			entry := NewWALEntry("testdb", []byte("cpu value=42"))
			marshaled := entry.Marshal()

			// Corrupt the checksum
			marshaled[0] ^= 0xFF

			_, err := UnmarshalWALEntry(marshaled)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "checksum mismatch")
		})

		Convey("When data is corrupted", func() {
			entry := NewWALEntry("testdb", []byte("cpu value=42"))
			marshaled := entry.Marshal()

			// Corrupt a byte in the data section
			marshaled[10] ^= 0xFF

			_, err := UnmarshalWALEntry(marshaled)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "checksum mismatch")
		})

		Convey("When multiple bytes are corrupted", func() {
			entry := NewWALEntry("testdb", []byte("cpu value=42"))
			marshaled := entry.Marshal()

			// Corrupt multiple bytes
			for i := 8; i < min(len(marshaled), 12); i++ {
				marshaled[i] ^= 0xFF
			}

			_, err := UnmarshalWALEntry(marshaled)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "checksum mismatch")
		})
	})
}

func TestWALEntryDecompress(t *testing.T) {
	Convey("Given a WAL entry", t, func() {
		database := "testdb"
		lineProtocol := []byte("cpu,host=server1 value=85.3 1620000000000000000")
		entry := NewWALEntry(database, lineProtocol)

		Convey("When decompressing", func() {
			decompressed, err := entry.Decompress()

			So(err, ShouldBeNil)
			So(decompressed, ShouldNotBeEmpty)

			// Should be valid JSON
			So(string(decompressed), ShouldContainSubstring, `"db":"testdb"`)
			So(string(decompressed), ShouldContainSubstring, `"lp":"cpu,host=server1 value=85.3`)
		})

		Convey("When decompressing corrupted data", func() {
			// Corrupt the compressed data
			entry.Data[0] ^= 0xFF

			_, err := entry.Decompress()

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "snappy decompression failed")
		})

		Convey("When decompressing empty data", func() {
			entry.Data = []byte{}
			entry.Length = 0

			_, err := entry.Decompress()

			So(err, ShouldNotBeNil)
		})
	})
}

func TestParseWALData(t *testing.T) {
	Convey("Given decompressed WAL data", t, func() {

		Convey("When parsing valid WAL data", func() {
			entry := NewWALEntry("testdb", []byte("cpu,host=server1 value=85.3 1620000000000000000"))
			decompressed, _ := entry.Decompress()

			walData, err := ParseWALData(decompressed)

			So(err, ShouldBeNil)
			So(walData.Database, ShouldEqual, "testdb")
			So(walData.LineProtocol, ShouldContainSubstring, "cpu,host=server1")
			So(len(walData.Points), ShouldEqual, 1)
			So(walData.Points[0].Measurement, ShouldEqual, "cpu")
			So(walData.Points[0].Tags["host"], ShouldEqual, "server1")
		})

		Convey("When parsing batch of points", func() {
			lineProtocol := `cpu value=1
mem value=2
disk value=3`
			entry := NewWALEntry("testdb", []byte(lineProtocol))
			decompressed, _ := entry.Decompress()

			walData, err := ParseWALData(decompressed)

			So(err, ShouldBeNil)
			So(len(walData.Points), ShouldEqual, 3)
			So(walData.Points[0].Measurement, ShouldEqual, "cpu")
			So(walData.Points[1].Measurement, ShouldEqual, "mem")
			So(walData.Points[2].Measurement, ShouldEqual, "disk")
		})

		Convey("When parsing invalid JSON", func() {
			invalidJSON := []byte(`{invalid json}`)

			_, err := ParseWALData(invalidJSON)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed to unmarshal")
		})

		Convey("When parsing invalid line protocol", func() {
			entry := NewWALEntry("testdb", []byte("invalid line protocol without fields"))
			decompressed, _ := entry.Decompress()

			_, err := ParseWALData(decompressed)

			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "failed to parse line protocol")
		})

		Convey("When parsing empty line protocol", func() {
			entry := NewWALEntry("testdb", []byte(""))
			decompressed, _ := entry.Decompress()

			walData, err := ParseWALData(decompressed)

			So(err, ShouldBeNil)
			So(len(walData.Points), ShouldEqual, 0)
		})
	})
}

func TestWALEntrySize(t *testing.T) {
	Convey("Given WAL entries", t, func() {

		Convey("When getting size of entry", func() {
			entry := NewWALEntry("testdb", []byte("cpu value=42"))
			size := entry.Size()

			So(size, ShouldEqual, 8+int(entry.Length))
			So(size, ShouldEqual, len(entry.Marshal()))
		})

		Convey("When comparing sizes of different entries", func() {
			smallEntry := NewWALEntry("db", []byte("cpu value=1"))
			largeEntry := NewWALEntry("database", []byte("cpu,host=server1,region=us-west,dc=dc1 value=85.3,count=42i,enabled=true 1620000000000000000"))

			So(largeEntry.Size(), ShouldBeGreaterThan, smallEntry.Size())
		})
	})
}

func TestWALEntry_EndToEnd(t *testing.T) {
	Convey("Given an end-to-end WAL workflow", t, func() {

		Convey("When creating, marshaling, unmarshaling, and parsing", func() {
			// Step 1: Create entry
			database := "production"
			lineProtocol := []byte(`cpu,host=web01,region=us-east usage=75.5,count=100i
mem,host=web01 used=8192i,available=16384i`)

			entry := NewWALEntry(database, lineProtocol)

			// Step 2: Marshal to bytes (what would be written to WAL)
			marshaled := entry.Marshal()
			So(len(marshaled), ShouldBeGreaterThan, 8)

			// Step 3: Unmarshal from bytes (what would be read from WAL)
			restored, err := UnmarshalWALEntry(marshaled)
			So(err, ShouldBeNil)

			// Step 4: Decompress
			decompressed, err := restored.Decompress()
			So(err, ShouldBeNil)

			// Step 5: Parse WAL data and line protocol
			walData, err := ParseWALData(decompressed)
			So(err, ShouldBeNil)
			So(walData.Database, ShouldEqual, "production")
			So(len(walData.Points), ShouldEqual, 2)

			// Verify first point
			So(walData.Points[0].Measurement, ShouldEqual, "cpu")
			So(walData.Points[0].Tags["host"], ShouldEqual, "web01")
			So(walData.Points[0].Tags["region"], ShouldEqual, "us-east")
			So(walData.Points[0].Fields["usage"], ShouldEqual, 75.5)
			So(walData.Points[0].Fields["count"], ShouldEqual, int64(100))

			// Verify second point
			So(walData.Points[1].Measurement, ShouldEqual, "mem")
			So(walData.Points[1].Tags["host"], ShouldEqual, "web01")
			So(walData.Points[1].Fields["used"], ShouldEqual, int64(8192))
			So(walData.Points[1].Fields["available"], ShouldEqual, int64(16384))
		})
	})
}

func TestWALEntry_ChecksumDetectsAllCorruption(t *testing.T) {
	Convey("Given a WAL entry", t, func() {
		entry := NewWALEntry("testdb", []byte("cpu value=42"))
		marshaled := entry.Marshal()

		Convey("When corrupting each byte individually", func() {
			for i := 8; i < len(marshaled); i++ { // Skip header, test data only
				corrupted := make([]byte, len(marshaled))
				copy(corrupted, marshaled)
				corrupted[i] ^= 0x01 // Flip one bit

				_, err := UnmarshalWALEntry(corrupted)
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "checksum mismatch")
			}
		})
	})
}

// Benchmark tests for performance measurement
func BenchmarkNewWALEntry(b *testing.B) {
	lineProtocol := []byte("cpu,host=server1,region=us-west value=85.3,count=42i 1620000000000000000")
	for i := 0; i < b.N; i++ {
		NewWALEntry("testdb", lineProtocol)
	}
}

func BenchmarkWALEntryMarshal(b *testing.B) {
	entry := NewWALEntry("testdb", []byte("cpu,host=server1 value=85.3"))
	for i := 0; i < b.N; i++ {
		entry.Marshal()
	}
}

func BenchmarkWALEntryUnmarshal(b *testing.B) {
	entry := NewWALEntry("testdb", []byte("cpu,host=server1 value=85.3"))
	marshaled := entry.Marshal()
	for i := 0; i < b.N; i++ {
		UnmarshalWALEntry(marshaled)
	}
}

func BenchmarkWALEntryDecompress(b *testing.B) {
	entry := NewWALEntry("testdb", []byte("cpu,host=server1 value=85.3"))
	for i := 0; i < b.N; i++ {
		entry.Decompress()
	}
}

func BenchmarkWALEntryEndToEnd(b *testing.B) {
	lineProtocol := []byte("cpu,host=server1,region=us-west value=85.3,count=42i 1620000000000000000")
	for i := 0; i < b.N; i++ {
		entry := NewWALEntry("testdb", lineProtocol)
		marshaled := entry.Marshal()
		restored, _ := UnmarshalWALEntry(marshaled)
		decompressed, _ := restored.Decompress()
		ParseWALData(decompressed)
	}
}

// Helper function for older Go versions
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
