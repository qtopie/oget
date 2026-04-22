package oget

import (
	"encoding/json"
	"io"
	"sync"
	"time"
)

// DownloadState represents the serializable state of a download task.
type DownloadState struct {
	URL          string            `json:"url"`
	FileSize     int64             `json:"file_size"`
	ETag         string            `json:"etag"`
	LastModified string            `json:"last_modified"`
	ChunkSize    int64             `json:"chunk_size"`
	Completed    []byte            `json:"completed"`
	Hashes       map[int]string    `json:"hashes"`
	UpdatedAt    time.Time         `json:"updated_at"`

	mu           sync.RWMutex      `json:"-"`
}

func NewDownloadState(url string, fileSize, chunkSize int64) *DownloadState {
	numChunks := (fileSize + chunkSize - 1) / chunkSize
	numBytes := (numChunks + 7) / 8
	return &DownloadState{
		URL:       url,
		FileSize:  fileSize,
		ChunkSize: chunkSize,
		Completed: make([]byte, numBytes),
		Hashes:    make(map[int]string),
		UpdatedAt: time.Now(),
	}
}

func (s *DownloadState) MarkComplete(chunkID int, hash string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byteIdx := chunkID / 8
	bitIdx := uint(chunkID % 8)
	if byteIdx < len(s.Completed) {
		s.Completed[byteIdx] |= (1 << bitIdx)
		s.Hashes[chunkID] = hash
		s.UpdatedAt = time.Now()
	}
}

func (s *DownloadState) IsComplete(chunkID int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	byteIdx := chunkID / 8
	bitIdx := uint(chunkID % 8)
	if byteIdx < len(s.Completed) {
		return (s.Completed[byteIdx] & (1 << bitIdx)) != 0
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

	if s.FileSize <= 0 {
		return 0
	}
	
	count := 0
	for _, b := range s.Completed {
		for i := 0; i < 8; i++ {
			if (b & (1 << uint(i))) != 0 {
				count++
			}
		}
	}
	
	numChunks := (s.FileSize + s.ChunkSize - 1) / s.ChunkSize
	return float64(count) / float64(numChunks) * 100
}

func (s *DownloadState) Save(store StateStore) error {
	s.mu.RLock()
	data, err := json.Marshal(s)
	s.mu.RUnlock()

	if err != nil {
		return err
	}

	_, err = store.Seek(0, io.SeekStart)
	if err != nil {
		return err
	}
	_, err = store.Write(data)
	if err != nil {
		return err
	}
	return store.Sync()
}

type StateStore interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Sync() error
}

type JSONStateStore struct {
	Store io.ReadWriteSeeker
}

func (s *JSONStateStore) Read(p []byte) (n int, err error) {
	return s.Store.Read(p)
}

func (s *JSONStateStore) Write(p []byte) (n int, err error) {
	return s.Store.Write(p)
}

func (s *JSONStateStore) Seek(offset int64, whence int) (int64, error) {
	return s.Store.Seek(offset, whence)
}

func (s *JSONStateStore) Load() (*DownloadState, error) {
	if _, err := s.Store.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	var state DownloadState
	if err := json.NewDecoder(s.Store).Decode(&state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *JSONStateStore) Close() error {
	if closer, ok := s.Store.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

func (s *JSONStateStore) Sync() error {
	if syncer, ok := s.Store.(interface{ Sync() error }); ok {
		return syncer.Sync()
	}
	return nil
}
