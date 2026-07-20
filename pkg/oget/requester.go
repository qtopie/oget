package oget

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	SubmitTask      func(...*ChunkTask)
	storages        []StorageHandler // tracked for Sync/Close on cleanup
}

func NewRequester(resource string, config *Config) *Requester {
	if config == nil {
		config = DefaultConfig()
	}
	return &Requester{
		Resource: resource,
		Fetcher:  GetFetcher(resource, config),
		Prober:   GetProber(resource, config),
		Config:   config,
	}
}

func (r *Requester) getStateFileName(fileName string) string {
	dir := filepath.Dir(fileName)
	base := filepath.Base(fileName)
	return filepath.Join(dir, "."+base+".oget")
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
	isBitTorrent := strings.HasPrefix(strings.ToLower(r.Resource), "magnet:") || isTorrentResource(r.Resource)

	meta, err := r.Prober.Probe(ctx, r.Resource)
	if err != nil {
		return fmt.Errorf("failed to probe resource %s: %w", r.Resource, err)
	}

	length := meta.Size
	etag := meta.ETag
	lastModified := meta.LastModified

	fileName := parseFileName(r.Resource)
	if r.Config != nil && r.Config.OutputDir != "" && r.Config.OutputDir != "." {
		fileName = filepath.Join(r.Config.OutputDir, fileName)
	}
	stateFileName := r.getStateFileName(fileName)

	var state *DownloadState
	// Try to load existing state
	if _, err := os.Stat(stateFileName); err == nil {
		s, err := LoadState(stateFileName)
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
			if state == nil {
				s.Close()
			}
		} else {
			log.Printf("Failed to load existing state: %v", err)
		}
	}

	if state == nil {
		state, err = NewDownloadState(r.Resource, length, RangeSize, stateFileName)
		if err != nil {
			return fmt.Errorf("failed to create download state: %w", err)
		}
		state.ETag = etag
		state.LastModified = lastModified
		// Initial save of metadata
		if err := state.Save(); err != nil {
			log.Printf("Warning: failed to save initial state: %v", err)
		}
	}
	defer state.Close()

	log.Printf("Preparing tasks for %s (%s, size: %s, progress: %.2f%%)",
		r.Resource, fileName, humanizeSize(length), state.PercentComplete())

	var storage StorageHandler
	if !isBitTorrent {
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
		storage, err = r.createStorageHandler(file, length)
		if err != nil {
			log.Printf("Failed to create preferred storage handler, fallback to standard file: %v", err)
			storage = &FileStorageHandler{File: file}
		}
		r.storages = append(r.storages, storage)
	}

	// Define a common OnChunkComplete that saves state
	onChunkComplete := func(chunkID int, hash string) {
		state.MarkComplete(chunkID, hash)
		if r.OnChunkComplete != nil {
			r.OnChunkComplete(chunkID, hash)
		}
		// Metadata (URL, ETag etc) doesn't change per chunk, 
		// so we don't need to call state.Save() here.
		// The bitset is already updated via mmap in state.MarkComplete.
	}

	// Split tasks.
	if isBitTorrent {
		// Single task for BitTorrent (which handles its own internal concurrency/P2P)
		task := NewChunkTask()
		task.FileID = fileName
		task.ChunkID = 0
		task.Offset = 0
		task.Length = length
		task.URL = r.Resource
		task.StorageHandler = storage
		task.FetcherHandler = r.Fetcher
		task.OnProgress = r.OnProgress
		task.OnChunkComplete = onChunkComplete
		if r.SubmitTask != nil {
			r.SubmitTask(task)
		}
		return nil
	}

	if length <= 0 {
		// Single task for unknown length (no resume support for unknown length yet)
		task := NewChunkTask()
		task.FileID = fileName
		task.ChunkID = 0
		task.Offset = 0
		task.Length = -1 // -1 means until EOF
		task.URL = r.Resource
		task.StorageHandler = storage
		task.FetcherHandler = r.Fetcher
		task.OnProgress = r.OnProgress
		task.OnChunkComplete = onChunkComplete
		if r.SubmitTask != nil {
			r.SubmitTask(task)
		}
		return nil
	}

	// Use the global RangeSize (1MB) defined in fetcher.go.
	chunkCount := int(length / RangeSize)
	if length%RangeSize != 0 {
		chunkCount++
	}

	batchSize := r.Config.TaskBatchSize
	if batchSize <= 0 {
		batchSize = 100
	}
	var batch []*ChunkTask

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

		task := NewChunkTask()
		task.FileID = fileName
		task.ChunkID = i
		task.Offset = offset
		task.Length = chunkLength
		task.URL = r.Resource
		task.StorageHandler = storage
		task.FetcherHandler = r.Fetcher
		task.OnProgress = r.OnProgress
		task.OnChunkComplete = onChunkComplete
		
		batch = append(batch, task)
		if len(batch) >= batchSize {
			if r.SubmitTask != nil {
				r.SubmitTask(batch...)
			}
			batch = nil
		}
	}
	
	if len(batch) > 0 {
		if r.SubmitTask != nil {
			r.SubmitTask(batch...)
		}
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

func (p *HttpProber) httpClient() *http.Client {
	transport := &http.Transport{}
	if p.Config != nil && p.Config.ProxyURL != "" {
		if proxyURL, err := url.Parse(p.Config.ProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}
	return &http.Client{
		Timeout:   time.Second * time.Duration(p.Config.Timeout),
		Transport: transport,
	}
}

func (p *HttpProber) Probe(ctx context.Context, url string) (*ResourceMetadata, error) {
	client := p.httpClient()

	extractMeta := func(resp *http.Response) *ResourceMetadata {
		meta := &ResourceMetadata{
			ETag:         resp.Header.Get("ETag"),
			LastModified: resp.Header.Get("Last-Modified"),
		}
		if attr := resp.Header.Get("Content-Length"); attr != "" {
			if l, err := strconv.ParseInt(attr, 10, 64); err == nil {
				meta.Size = l
			}
		}
		return meta
	}

	// 1. Try HEAD with Range (most efficient for probing)
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err == nil {
		req.Header.Set("User-Agent", "oget/"+Version)
		req.Header.Set("Range", "bytes=0-0")
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusPartialContent {
				meta := extractMeta(resp)
				contentRange := resp.Header.Get("Content-Range")
				if contentRange != "" {
					var start, end, total int64
					fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
					meta.Size = total
					return meta, nil
				}
			}
		}
	}

	// 2. Try standard HEAD
	req, err = http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err == nil {
		req.Header.Set("User-Agent", "oget/"+Version)
		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return extractMeta(resp), nil
			}
		}
	}

	// 3. Fallback to GET with limit (if HEAD failed or returned 302/405/etc)
	// We use a limited GET to just see the headers and maybe the first byte
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "oget/"+Version)
	// We don't want the whole body yet, just the response headers
	resp, err := client.Do(req)
	if err != nil {
		// If even GET fails, we can't do much
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		return extractMeta(resp), nil
	}
	
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("resource not found (404): %s", url)
	}

	// If we got here, we still return what we have (even if Size 0) to allow direct download attempt
	return &ResourceMetadata{Size: 0}, nil
}

// Cleanup syncs data to disk and removes the state file associated with the resource.
func (r *Requester) Cleanup() {
	// Sync and close all storage handlers to ensure data is flushed (especially for mmap backend)
	for _, s := range r.storages {
		if err := s.Sync(); err != nil {
			log.Printf("Warning: failed to sync storage for %s: %v", r.Resource, err)
		}
		if err := s.Close(); err != nil {
			log.Printf("Warning: failed to close storage for %s: %v", r.Resource, err)
		}
	}
	r.storages = nil

	fileName := parseFileName(r.Resource)
	if r.Config != nil && r.Config.OutputDir != "" && r.Config.OutputDir != "." {
		fileName = filepath.Join(r.Config.OutputDir, fileName)
	}
	stateFileName := r.getStateFileName(fileName)
	dir := filepath.Dir(fileName)
	base := filepath.Base(fileName)
	bitsetFileName := filepath.Join(dir, "."+base+".oget.bits")

	_ = os.Remove(stateFileName)
	_ = os.Remove(bitsetFileName)
}
