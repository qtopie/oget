package oget

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"syscall"
	"time"

	"github.com/quic-go/quic-go/http3"
	"golang.org/x/net/http2"
	"golang.org/x/sys/unix"
)

// RangeSize sets the default range size to 1MB
const (
	RangeSize          int64 = 1024 * 1024
	DefaultConcurrency int   = 32
)

// Fetcher is the interface for different download protocols.
type Fetcher interface {
	Fetch(ctx context.Context, task *ChunkTask) error
}

// StorageHandler defines the interface for file operations, allowing for different storage backends.
type StorageHandler interface {
	io.ReaderAt
	io.WriterAt
	io.Seeker
	io.Closer
	// ReadAtFrom reads data from r and writes it to the storage at the given offset.
	ReadAtFrom(r io.Reader, off int64, count int64) (n int64, err error)
	// SpliceFrom splices data from a raw file descriptor (e.g., a socket) directly.
	SpliceFrom(fd uintptr, off int64, count int64) (n int64, err error)
}

// FileStorageHandler wraps *os.File to implement StorageHandler.
type FileStorageHandler struct {
	*os.File
}

// ReadAtFrom implements ReadAtFrom by reading into a buffer and calling WriteAt.
func (f *FileStorageHandler) ReadAtFrom(r io.Reader, off int64, count int64) (int64, error) {
	buf := make([]byte, 32*1024)
	var total int64
	for total < count {
		toRead := int64(len(buf))
		if count-total < toRead {
			toRead = count - total
		}
		nr, er := r.Read(buf[:toRead])
		if nr > 0 {
			nw, ew := f.WriteAt(buf[:nr], off+total)
			if nw > 0 {
				total += int64(nw)
			}
			if ew != nil {
				return total, ew
			}
		}
		if er != nil {
			if er == io.EOF {
				return total, nil
			}
			return total, er
		}
	}
	return total, nil
}

// SpliceFrom implements true zero-copy using Linux splice system call.
func (f *FileStorageHandler) SpliceFrom(fd uintptr, off int64, count int64) (int64, error) {
	p1, p2, err := os.Pipe()
	if err != nil {
		return 0, err
	}
	defer p1.Close()
	defer p2.Close()

	var total int64
	for total < count {
		n1, err := unix.Splice(int(fd), nil, int(p2.Fd()), nil, int(count-total), unix.SPLICE_F_MOVE|unix.SPLICE_F_MORE)
		if err != nil {
			return total, err
		}
		if n1 == 0 {
			break
		}

		n2, err := unix.Splice(int(p1.Fd()), nil, int(f.Fd()), &off, int(n1), unix.SPLICE_F_MOVE)
		if err != nil {
			return total, err
		}
		total += int64(n2)
	}
	return total, nil
}

// ChunkTask represents a small piece of a file to be downloaded.
type ChunkTask struct {
	FileID          string
	ChunkID         int
	Offset          int64
	Length          int64
	URL             string
	StorageHandler  StorageHandler
	FetcherHandler  Fetcher
	OnProgress      func(bytesRead int)
	OnChunkComplete func(chunkID int, hash string)
}

// HttpFetcher implements Fetcher for HTTP protocol.
type HttpFetcher struct {
	Client *http.Client
}

// NewHttpFetcher creates a new HttpFetcher with BBR, Keepalive, H2, H3 and Proxy support.
func NewHttpFetcher(config *Config) *HttpFetcher {
	if config == nil {
		config = DefaultConfig()
	}

	// Suppress QUIC receive buffer warning unless verbose is enabled.
	if !config.Verbose {
		os.Setenv("QUIC_GO_DISABLE_RECEIVE_BUFFER_WARNING", "true")
	}

	// Proxy Detection
	proxyURLStr := config.ProxyURL
	// Fallback to environment if config is empty
	if proxyURLStr == "" {
		if u, err := http.ProxyFromEnvironment(&http.Request{URL: &url.URL{Scheme: "http"}}); err == nil && u != nil {
			proxyURLStr = u.String()
		}
	}

	var proxyFunc func(*http.Request) (*url.URL, error)
	hasProxy := false
	if proxyURLStr != "" {
		if pURL, err := url.Parse(proxyURLStr); err == nil {
			proxyFunc = http.ProxyURL(pURL)
			hasProxy = true
			if config.Verbose {
				log.Printf("Using proxy: %s", proxyURLStr)
			}
		}
	} else {
		proxyFunc = http.ProxyFromEnvironment
	}

	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control: func(network, address string, c syscall.RawConn) error {
			return c.Control(func(fd uintptr) {
				_ = unix.SetsockoptString(int(fd), unix.IPPROTO_TCP, unix.TCP_CONGESTION, "bbr")
			})
		},
	}

	t1 := &http.Transport{
		Proxy:                 proxyFunc,
		DialContext:           dialer.DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"h2", "http/1.1"},
		},
	}
	_ = http2.ConfigureTransport(t1)

	// In latest quic-go/http3, RoundTripper is replaced by Transport or similar structures.
	// We'll use http3.Transport and get its RoundTripper.
	var h3Transport http.RoundTripper
	if !hasProxy {
		// Only enable HTTP/3 if NO proxy is used, because most proxies don't support UDP.
		h3Transport = &http3.Transport{
			TLSClientConfig: &tls.Config{
				NextProtos: []string{"h3"},
			},
		}
	}

	client := &http.Client{
		Transport: &hybridRoundTripper{
			h12: t1,
			h3:  h3Transport,
		},
	}

	return &HttpFetcher{
		Client: client,
	}
}

type hybridRoundTripper struct {
	h12 http.RoundTripper
	h3  http.RoundTripper
}

func (h *hybridRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" || h.h3 == nil {
		return h.h12.RoundTrip(req)
	}

	res, err := h.h3.RoundTrip(req)
	if err == nil {
		return res, nil
	}

	return h.h12.RoundTrip(req)
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

	hash := sha256.New()
	var written int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		remaining := task.Length - written
		if task.Length == -1 {
			remaining = 32 * 1024
		}
		if remaining <= 0 && task.Length != -1 {
			break
		}

		n, err := task.StorageHandler.ReadAtFrom(io.TeeReader(resp.Body, hash), task.Offset+written, remaining)
		if n > 0 {
			written += n
			if task.OnProgress != nil {
				task.OnProgress(int(n))
			}
		}

		if err != nil {
			if err != io.EOF && err != io.ErrUnexpectedEOF {
				return err
			}
			break
		}
	}

	if task.OnChunkComplete != nil {
		task.OnChunkComplete(task.ChunkID, hex.EncodeToString(hash.Sum(nil)))
	}

	return nil
}
