package oget

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/fxamacker/cbor/v2"
)

/*
Standardized Binary State Format (CBOR):
The file is a CBOR Sequence:
1. Tag 55799 (Self-describe CBOR)
2. Map containing:
   - "ver": Version (int)
   - "meta": Metadata Map (URL, FileSize, etc.)
   - "bits": Byte String (The completion bitset)

To maintain mmap performance, the "bits" data is padded to start at a 16KB boundary.
*/

const (
	stateVersion    = 1
	stateHeaderSize = 16384 // We reserve 16KB for CBOR header + padding to ensure page alignment for mmap
)

// DownloadState represents the metadata of a download task.
type DownloadState struct {
	URL          string            `cbor:"url"`
	FileSize     int64             `cbor:"file_size"`
	ETag         string            `cbor:"etag"`
	LastModified string            `cbor:"last_modified"`
	ChunkSize    int64             `cbor:"chunk_size"`
	UpdatedAt    time.Time         `cbor:"updated_at"`
	
	// Internal state
	bitset       *mmapBitset       `cbor:"-"`
	mu           sync.RWMutex      `cbor:"-"`
	filePath     string            `cbor:"-"`
}

type mmapBitset struct {
	file *os.File
	data []byte
}

// NewDownloadState creates a new state file using CBOR.
func NewDownloadState(url string, fileSize, chunkSize int64, statePath string) (*DownloadState, error) {
	s := &DownloadState{
		URL:       url,
		FileSize:  fileSize,
		ChunkSize: chunkSize,
		UpdatedAt: time.Now(),
		filePath:  statePath,
	}
	
	numChunks := (fileSize + chunkSize - 1) / chunkSize
	numBytes := (numChunks + 7) / 8
	
	totalSize := int64(stateHeaderSize) + numBytes
	
	f, err := os.OpenFile(statePath, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		return nil, err
	}
	
	if err := f.Truncate(totalSize); err != nil {
		f.Close()
		return nil, err
	}
	
	// Map the bitset part (starting from 4096)
	data, err := mmapFileOffset(f, int(numBytes), stateHeaderSize)
	if err != nil {
		f.Close()
		return nil, err
	}
	
	s.bitset = &mmapBitset{
		file: f,
		data: data,
	}
	
	return s, nil
}

// LoadState loads the metadata from a CBOR state file.
func LoadState(statePath string) (*DownloadState, error) {
	f, err := os.OpenFile(statePath, os.O_RDWR, 0666)
	if err != nil {
		return nil, err
	}
	
	// Read the header part (first 4KB)
	headerData := make([]byte, stateHeaderSize)
	if _, err := f.ReadAt(headerData, 0); err != nil {
		f.Close()
		return nil, err
	}
	
	// Decode CBOR metadata
	// We expect a map at the beginning of the file (after potential tags)
	var state DownloadState
	dec := cbor.NewDecoder(io.NewSectionReader(f, 0, stateHeaderSize))
	if err := dec.Decode(&state); err != nil {
		f.Close()
		return nil, fmt.Errorf("failed to decode cbor state: %w", err)
	}
	state.filePath = statePath
	
	// Initialize mmap bitset
	numChunks := (state.FileSize + state.ChunkSize - 1) / state.ChunkSize
	numBytes := (numChunks + 7) / 8
	
	data, err := mmapFileOffset(f, int(numBytes), stateHeaderSize)
	if err != nil {
		f.Close()
		return nil, err
	}
	
	state.bitset = &mmapBitset{
		file: f,
		data: data,
	}
	
	return &state, nil
}

func (s *DownloadState) MarkComplete(chunkID int, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byteIdx := chunkID / 8
	bitIdx := uint(chunkID % 8)
	
	if s.bitset != nil && byteIdx < len(s.bitset.data) {
		s.bitset.data[byteIdx] |= (1 << bitIdx)
		s.UpdatedAt = time.Now()
	}
}

func (s *DownloadState) IsComplete(chunkID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byteIdx := chunkID / 8
	bitIdx := uint(chunkID % 8)
	if s.bitset != nil && byteIdx < len(s.bitset.data) {
		return (s.bitset.data[byteIdx] & (1 << bitIdx)) != 0
	}
	return false
}

func (s *DownloadState) IsServerChanged(newETag, newLastModified string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if newETag != "" && s.ETag != "" && newETag != s.ETag {
		return true
	}
	if newLastModified != "" && s.LastModified != "" && newLastModified != s.LastModified {
		return true
	}
	return false
}

func (s *DownloadState) PercentComplete() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.FileSize <= 0 || s.bitset == nil {
		return 0
	}
	
	count := 0
	for _, b := range s.bitset.data {
		for i := 0; i < 8; i++ {
			if (b & (1 << uint(i))) != 0 {
				count++
			}
		}
	}
	
	numChunks := (s.FileSize + s.ChunkSize - 1) / s.ChunkSize
	return float64(count) / float64(numChunks) * 100
}

// Save writes the metadata part to the file using CBOR.
func (s *DownloadState) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a buffer for the CBOR header
	data, err := cbor.Marshal(s)
	if err != nil {
		return err
	}

	if len(data) > stateHeaderSize {
		return fmt.Errorf("metadata too large for reserved header space")
	}

	// Write at the beginning of the file
	if _, err := s.bitset.file.WriteAt(data, 0); err != nil {
		return err
	}
	
	// We don't pad with zeros explicitly as the file is already truncated 
	// and we know the bitset starts at stateHeaderSize.
	
	return s.bitset.file.Sync()
}

func (s *DownloadState) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.bitset != nil {
		err := munmapFile(s.bitset.data)
		s.bitset.file.Close()
		s.bitset = nil
		return err
	}
	return nil
}

type StateStore interface {
	io.Closer
	Sync() error
}
