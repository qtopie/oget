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
	URL     string
	Fetcher Fetcher
}

func NewRequester(url string) *Requester {
	return &Requester{
		URL:     url,
		Fetcher: NewHttpFetcher(),
	}
}

// PrepareTasks probes the URL and splits it into ChunkTasks.
func (r *Requester) PrepareTasks(ctx context.Context) error {
	length, err := r.probe(ctx)
	if err != nil {
		return fmt.Errorf("failed to probe URL %s: %w", r.URL, err)
	}

	fileName := parseFileName(r.URL)
	log.Printf("Preparing tasks for %s (%s, size: %d bytes)", r.URL, fileName, length)

	file, err := os.OpenFile(fileName, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("failed to create/open file %s: %w", fileName, err)
	}

	// Ensure file has enough space if length is known.
	if length > 0 {
		if err := file.Truncate(length); err != nil {
			log.Printf("Warning: failed to truncate file %s: %v", fileName, err)
		}
	}

	// Split tasks.
	if length <= 0 {
		// Single task for unknown length (if supported by server).
		select {
		case TaskQueue <- &ChunkTask{
			FileID:         fileName,
			ChunkID:        0,
			Offset:         0,
			Length:         -1, // -1 means until EOF
			URL:            r.URL,
			FileHandler:    file,
			FetcherHandler: r.Fetcher,
		}:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	// Use the global RangeSize (1MB) defined in fetcher.go.
	chunkCount := int(length / RangeSize)
	if length%RangeSize != 0 {
		chunkCount++
	}

	for i := 0; i < chunkCount; i++ {
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

		select {
		case TaskQueue <- &ChunkTask{
			FileID:         fileName,
			ChunkID:        i,
			Offset:         offset,
			Length:         chunkLength,
			URL:            r.URL,
			FileHandler:    file,
			FetcherHandler: r.Fetcher,
		}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// probe makes an HTTP request with context support.
func (r *Requester) probe(ctx context.Context) (length int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, r.URL, nil)
	if err != nil {
		return 0, err
	}

	client := &http.Client{
		Timeout: time.Second * 5,
	}
	// Try range request to check if supported and get length.
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent {
		contentRange := resp.Header.Get("Content-Range")
		if contentRange != "" {
			var start, end, total int64
			fmt.Sscanf(contentRange, "bytes %d-%d/%d", &start, &end, &total)
			return total, nil
		}
	}

	// Fallback to HEAD without Range
	req, _ = http.NewRequestWithContext(ctx, http.MethodHead, r.URL, nil)
	resp, err = client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		attr := resp.Header.Get("Content-Length")
		if attr != "" {
			return strconv.ParseInt(attr, 10, 64)
		}
	}

	return 0, nil
}
