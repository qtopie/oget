package oget

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
)

// RangeSize sets the default range size to 1MB
const (
	RangeSize    int64 = 1024 * 1024
	ThreadAmount int   = 32
)

// Fetcher is the interface for different download protocols.
type Fetcher interface {
	Fetch(ctx context.Context, task *ChunkTask) error
}

// ChunkTask represents a small piece of a file to be downloaded.
type ChunkTask struct {
	FileID         string
	ChunkID        int
	Offset         int64
	Length         int64
	URL            string
	FileHandler    *os.File
	FetcherHandler Fetcher
}

// HttpFetcher implements Fetcher for HTTP protocol.
type HttpFetcher struct {
	Client *http.Client
}

// NewHttpFetcher creates a new HttpFetcher with default configuration.
func NewHttpFetcher() *HttpFetcher {
	return &HttpFetcher{
		Client: &http.Client{},
	}
}

// Fetch executes a single ChunkTask with context support.
func (f *HttpFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.URL, nil)
	if err != nil {
		return err
	}

	rangeHeader := fmt.Sprintf("bytes=%d-%d", task.Offset, task.Offset+task.Length-1)
	req.Header.Set("Range", rangeHeader)

	resp, err := f.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	buf := make([]byte, 32*1024)
	var written int64
	for {
		// Check context before each read/write cycle
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		nr, er := resp.Body.Read(buf)
		if nr > 0 {
			nw, ew := task.FileHandler.WriteAt(buf[0:nr], task.Offset+written)
			if nw > 0 {
				written += int64(nw)
			}
			if ew != nil {
				return ew
			}
			if nr != nw {
				return io.ErrShortWrite
			}
		}
		if er != nil {
			if er != io.EOF {
				return er
			}
			break
		}
	}

	return nil
}
