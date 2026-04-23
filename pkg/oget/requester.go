package oget

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

// Requester manages probing and task creation for a single URL.
type Requester struct {
	URL             string
	Fetcher         Fetcher
	Config          *Config
	OnProgress      func(int)
	OnChunkComplete func(int, string)
	SubmitTask      func(*ChunkTask)
}

func NewRequester(url string, config *Config) *Requester {
	if config == nil {
		config = DefaultConfig()
	}
	return &Requester{
		URL:     url,
		Fetcher: NewHttpFetcher(config),
		Config:  config,
	}
}

func (r *Requester) getStateFileName(fileName string) string {
	return "." + fileName + ".oget"
}

func (r *Requester) createStorageHandler(file *os.File, length int64) (StorageHandler, error) {
	switch r.Config.StorageType {
	case "uring":
		log.Printf("Using standard file storage (io_uring is disabled due to environment compatibility)")
		return &FileStorageHandler{File: file}, nil
	case "mmap":
		if length <= 0 {
			log.Printf("Warning: cannot use mmap for unknown length, falling back to standard file")
			return &FileStorageHandler{File: file}, nil
		}
		log.Printf("Using mmap storage backend")
		return NewMmapStorageHandler(file, length)
	default:
		return &FileStorageHandler{File: file}, nil
	}
}

// PrepareTasks probes the URL and splits it into ChunkTasks.
func (r *Requester) PrepareTasks(ctx context.Context) error {
	length, etag, lastModified, err := r.probe(ctx)
	if err != nil {
		return fmt.Errorf("failed to probe URL %s: %w", r.URL, err)
	}

	fileName := parseFileName(r.URL)
	stateFileName := r.getStateFileName(fileName)

	var state *DownloadState
	// Try to load existing state
	if _, err := os.Stat(stateFileName); err == nil {
		f, err := os.OpenFile(stateFileName, os.O_RDWR, 0666)
		if err == nil {
			store := &JSONStateStore{Store: f}
			s, err := store.Load()
			if err == nil {
				// Verify if server file has changed
				if s.FileSize == length && !s.IsServerChanged(etag, lastModified) {
					log.Printf("Found existing state for %s, resuming download...", fileName)
					state = s
				} else {
					log.Printf("Server file changed or size mismatch, restarting download for %s", fileName)
				}
			}
			f.Close()
		}
	}

	if state == nil {
		state = NewDownloadState(r.URL, length, RangeSize)
		state.ETag = etag
		state.LastModified = lastModified
	}

	log.Printf("Preparing tasks for %s (%s, size: %s, progress: %.2f%%)",
		r.URL, fileName, humanizeSize(length), state.PercentComplete())

	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("failed to create/open file %s: %w", fileName, err)
	}

	// Ensure file has enough space if length is known.
	if length > 0 {
		// Priority 1: Use fallocate for physical pre-allocation (best performance)
		if err := fallocate(int(file.Fd()), 0, 0, length); err != nil {
			log.Printf("Warning: fallocate failed for %s, falling back to truncate: %v", fileName, err)
			// Priority 2: Fallback to Truncate (Sparse file)
			if err := file.Truncate(length); err != nil {
				log.Printf("Error: failed to truncate file %s: %v", fileName, err)
			}
		}
	}

	// Choose the appropriate storage handler (standard, uring, mmap)
	storage, err := r.createStorageHandler(file, length)
	if err != nil {
		log.Printf("Failed to create preferred storage handler, fallback to standard file: %v", err)
		storage = &FileStorageHandler{File: file}
	}

	// Define a common OnChunkComplete that saves state
	onChunkComplete := func(chunkID int, hash string) {
		state.MarkComplete(chunkID, hash)
		if r.OnChunkComplete != nil {
			r.OnChunkComplete(chunkID, hash)
		}
		// Save state to disk
		sf, err := os.OpenFile(stateFileName, os.O_CREATE|os.O_RDWR, 0666)
		if err == nil {
			store := &JSONStateStore{Store: sf}
			if err := state.Save(store); err != nil {
				log.Printf("Warning: failed to save download state: %v", err)
			}
			sf.Close()
		}
	}

	// Helper to submit task
	submit := func(task *ChunkTask) {
		if r.SubmitTask != nil {
			r.SubmitTask(task)
		}
	}

	// Split tasks.
	if length <= 0 {
		// Single task for unknown length (no resume support for unknown length yet)
		submit(&ChunkTask{
			FileID:          fileName,
			ChunkID:         0,
			Offset:          0,
			Length:          -1, // -1 means until EOF
			URL:             r.URL,
			StorageHandler:  storage,
			FetcherHandler:  r.Fetcher,
			OnProgress:      r.OnProgress,
			OnChunkComplete: onChunkComplete,
		})
		return nil
	}

	// Use the global RangeSize (1MB) defined in fetcher.go.
	chunkCount := int(length / RangeSize)
	if length%RangeSize != 0 {
		chunkCount++
	}

	for i := 0; i < chunkCount; i++ {
		// Skip completed chunks
		if state.IsComplete(i) {
			continue
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		offset := int64(i) * RangeSize
		chunkLength := RangeSize
		if i == chunkCount-1 {
			chunkLength = length - offset
		}

		submit(&ChunkTask{
			FileID:          fileName,
			ChunkID:         i,
			Offset:          offset,
			Length:          chunkLength,
			URL:             r.URL,
			StorageHandler:  storage,
			FetcherHandler:  r.Fetcher,
			OnProgress:      r.OnProgress,
			OnChunkComplete: onChunkComplete,
		})
	}
	return nil
}

// probe makes an HTTP request with context support.
func (r *Requester) probe(ctx context.Context) (length int64, etag string, lastModified string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, r.URL, nil)
	if err != nil {
		return 0, "", "", err
	}

	client := &http.Client{
		Timeout: time.Second * 5,
	}
	// Try range request to check if supported and get length.
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	etag = resp.Header.Get("ETag")
	lastModified = resp.Header.Get("Last-Modified")

	if resp.StatusCode == http.StatusPartialContent {
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			var start, end, total int64
			fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
			return total, etag, lastModified, nil
		}
	}

	// Fallback to HEAD without Range
	req, _ = http.NewRequestWithContext(ctx, http.MethodHead, r.URL, nil)
	resp, err = client.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	etag = resp.Header.Get("ETag")
	lastModified = resp.Header.Get("Last-Modified")

	if resp.StatusCode == http.StatusOK {
		attr := resp.Header.Get("Content-Length")
		if attr != "" {
			l, err := strconv.ParseInt(attr, 10, 64)
			return l, etag, lastModified, err
		}
	}

	return 0, etag, lastModified, nil
}
