package write

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"

	"github.com/golang/snappy"
)

// WALData represents the uncompressed data stored in WAL
type WALData struct {
	Database     string   `json:"db"`
	LineProtocol string   `json:"lp"`
	Points       []*Point `json:"-"` // not serialized, populated on read
}

// WALEntry represents a single entry in the write-ahead log
// Format: [4 bytes CRC32][4 bytes length][N bytes compressed data]
type WALEntry struct {
	Checksum uint32 // CRC32 of length+data
	Length   uint32 // size of compressed data
	Data     []byte // snappy-compressed JSON (WALData)
}

// NewWALEntry creates a new WAL entry from database and line protocol data
func NewWALEntry(database string, lineProtocol []byte) *WALEntry {
	// Create WAL data structure
	walData := &WALData{
		Database:     database,
		LineProtocol: string(lineProtocol),
	}

	// Marshal to JSON
	jsonData, _ := json.Marshal(walData)

	// Compress the JSON using snappy
	compressed := snappy.Encode(nil, jsonData)

	// Calculate checksum of length+data
	length := uint32(len(compressed))
	checksumData := make([]byte, 4+len(compressed))
	binary.LittleEndian.PutUint32(checksumData[0:4], length)
	copy(checksumData[4:], compressed)

	return &WALEntry{
		Checksum: crc32.ChecksumIEEE(checksumData),
		Length:   length,
		Data:     compressed,
	}
}

// Marshal serializes the WAL entry to bytes
func (e *WALEntry) Marshal() []byte {
	buf := make([]byte, 8+len(e.Data))
	binary.LittleEndian.PutUint32(buf[0:4], e.Checksum)
	binary.LittleEndian.PutUint32(buf[4:8], e.Length)
	copy(buf[8:], e.Data)
	return buf
}

// UnmarshalWALEntry deserializes and validates a WAL entry
func UnmarshalWALEntry(buf []byte) (*WALEntry, error) {
	if len(buf) < 8 {
		return nil, fmt.Errorf("entry too short: %d bytes (minimum 8)", len(buf))
	}

	entry := &WALEntry{
		Checksum: binary.LittleEndian.Uint32(buf[0:4]),
		Length:   binary.LittleEndian.Uint32(buf[4:8]),
		Data:     buf[8:],
	}

	// Verify length matches
	if len(entry.Data) != int(entry.Length) {
		return nil, fmt.Errorf("length mismatch: header says %d bytes, got %d bytes",
			entry.Length, len(entry.Data))
	}

	// Verify checksum
	checksumData := make([]byte, 4+len(entry.Data))
	binary.LittleEndian.PutUint32(checksumData[0:4], entry.Length)
	copy(checksumData[4:], entry.Data)
	computed := crc32.ChecksumIEEE(checksumData)

	if computed != entry.Checksum {
		return nil, fmt.Errorf("checksum mismatch: expected 0x%08x, got 0x%08x (data corrupted)",
			entry.Checksum, computed)
	}

	return entry, nil
}

// Decompress decompresses the entry data
func (e *WALEntry) Decompress() ([]byte, error) {
	decompressed, err := snappy.Decode(nil, e.Data)
	if err != nil {
		return nil, fmt.Errorf("snappy decompression failed: %w", err)
	}
	return decompressed, nil
}

// Size returns the total size of the marshaled entry in bytes
func (e *WALEntry) Size() int {
	return 8 + len(e.Data)
}

// ParseWALData parses the decompressed WAL data and returns database + points
func ParseWALData(decompressed []byte) (*WALData, error) {
	var walData WALData
	if err := json.Unmarshal(decompressed, &walData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal WAL data: %w", err)
	}

	// Parse the line protocol
	points, err := ParseBatch(walData.LineProtocol)
	if err != nil {
		return nil, fmt.Errorf("failed to parse line protocol: %w", err)
	}

	walData.Points = points
	return &walData, nil
}
