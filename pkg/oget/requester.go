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

// ResourceMetadata contains basic information about a remote resource.
type ResourceMetadata struct {
	Size         int64
	ETag         string
	LastModified string
}

// Prober defines the interface for resource discovery.
type Prober interface {
	Probe(ctx context.Context, resource string) (*ResourceMetadata, error)
}

// Requester manages probing and task creation for a single resource.
type Requester struct {
	Resource        string
	Fetcher         Fetcher
	Prober          Prober
	Config          *Config
	OnProgress      func(int)
	OnChunkComplete func(int, string)
	SubmitTask      func(*ChunkTask)
}

func NewRequester(resource string, config *Config) *Requester {
	if config == nil {
		config = DefaultConfig()
	}
	return &Requester{
		Resource: resource,
		Fetcher:  NewHttpFetcher(config),
		Prober:   NewHttpProber(config),
		Config:   config,
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

// PrepareTasks probes the resource and splits it into ChunkTasks.
func (r *Requester) PrepareTasks(ctx context.Context) error {
	meta, err := r.Prober.Probe(ctx, r.Resource)
	if err != nil {
		return fmt.Errorf("failed to probe resource %s: %w", r.Resource, err)
	}

	length := meta.Size
	etag := meta.ETag
	lastModified := meta.LastModified

	fileName := parseFileName(r.Resource)
	stateFileName := r.getStateFileName(fileName)

	var state *DownloadState
	// Try to load existing state
	if _, err := os.Stat(stateFileName); err == nil {
		f, err := os.OpenFile(stateFileName, os.O_RDWR, 0666)
		if err == nil {
			store := &JSONStateStore{Store: f}
			s, err := store.Load()
			if err == nil {
				// Verify if server file has changed and target file exists
				if s.FileSize == length && !s.IsServerChanged(etag, lastModified) {
					if _, err := os.Stat(fileName); err == nil {
						log.Printf("Found existing state and file for %s, resuming download...", fileName)
						state = s
					} else {
						log.Printf("Target file %s missing, restarting download", fileName)
					}
				} else {
					log.Printf("Server file changed or size mismatch, restarting download for %s", fileName)
				}
			}
			f.Close()
		}
	}

	if state == nil {
		state = NewDownloadState(r.Resource, length, RangeSize)
		state.ETag = etag
		state.LastModified = lastModified
	}

	log.Printf("Preparing tasks for %s (%s, size: %s, progress: %.2f%%)",
		r.Resource, fileName, humanizeSize(length), state.PercentComplete())

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
			URL:             r.Resource,
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
			URL:             r.Resource,
			StorageHandler:  storage,
			FetcherHandler:  r.Fetcher,
			OnProgress:      r.OnProgress,
			OnChunkComplete: onChunkComplete,
		})
	}
	return nil
}

// HttpProber implements Prober for HTTP protocol.
type HttpProber struct {
	Config *Config
}

func NewHttpProber(config *Config) *HttpProber {
	return &HttpProber{Config: config}
}

func (p *HttpProber) Probe(ctx context.Context, url string) (*ResourceMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Timeout: time.Second * time.Duration(p.Config.Timeout),
	}
	// Try range request to check if supported and get length.
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	etag := resp.Header.Get("ETag")
	lastModified := resp.Header.Get("Last-Modified")

	if resp.StatusCode == http.StatusPartialContent {
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			var start, end, total int64
			fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
			return &ResourceMetadata{Size: total, ETag: etag, LastModified: lastModified}, nil
		}
	}

	// Fallback to HEAD without Range
	req, _ = http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	resp, err = client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	etag = resp.Header.Get("ETag")
	lastModified = resp.Header.Get("Last-Modified")

	if resp.StatusCode == http.StatusOK {
		attr := resp.Header.Get("Content-Length")
		if attr != "" {
			l, err := strconv.ParseInt(attr, 10, 64)
			return &ResourceMetadata{Size: l, ETag: etag, LastModified: lastModified}, err
		}
	}

	return &ResourceMetadata{Size: 0, ETag: etag, LastModified: lastModified}, nil
}

// Cleanup removes the state file associated with the resource.
func (r *Requester) Cleanup() {
	fileName := parseFileName(r.Resource)
	stateFileName := r.getStateFileName(fileName)
	if err := os.Remove(stateFileName); err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Warning: failed to remove state file %s: %v", stateFileName, err)
		}
	}
}
