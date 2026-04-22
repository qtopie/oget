package ogettest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"time"
)

const (
	// DefaultWebContent stores "Hello World!" text content.
	DefaultWebContent = "Hello World!"
)

// DummyContent implements io.ReadSeeker to simulate a large file without using memory.
// It generates deterministic data based on the offset.
type DummyContent struct {
	Size int64
	off  int64
	mu   sync.Mutex
}

func (d *DummyContent) Read(p []byte) (n int, err error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.off >= d.Size {
		return 0, io.EOF
	}

	remaining := d.Size - d.off
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}

	for i := range p {
		// Generate deterministic byte based on absolute position
		p[i] = byte((d.off + int64(i)) % 256)
	}

	d.off += int64(len(p))
	return len(p), nil
}

func (d *DummyContent) Seek(offset int64, whence int) (int64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	var newOff int64
	switch whence {
	case io.SeekStart:
		newOff = offset
	case io.SeekCurrent:
		newOff = d.off + offset
	case io.SeekEnd:
		newOff = d.Size + offset
	default:
		return 0, fmt.Errorf("invalid whence")
	}

	if newOff < 0 {
		return 0, fmt.Errorf("negative offset")
	}
	d.off = newOff
	return d.off, nil
}

// CalculateSHA256 returns the hash of the virtual content for verification.
func (d *DummyContent) CalculateSHA256() string {
	h := sha256.New()
	// We have to "read" it all to hash it, but it's fast since it's procedural.
	buf := make([]byte, 32*1024)
	curr := d.off
	d.off = 0
	for {
		n, err := d.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	d.off = curr
	return hex.EncodeToString(h.Sum(nil))
}

// EnhancedServer handles dynamic bandwidth and fault simulation.
type EnhancedServer struct {
	*httptest.Server
	Content     io.ReadSeeker
	BytesPerSec int64
	Latency     time.Duration
	ETag        string
}

func NewEnhancedServer(content io.ReadSeeker) *EnhancedServer {
	s := &EnhancedServer{
		Content: content,
		ETag:    "initial-etag",
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Simulate Latency
		if s.Latency > 0 {
			time.Sleep(s.Latency)
		}

		// 2. Bandwidth Limiting (Throttling)
		var writer io.Writer = w
		if s.BytesPerSec > 0 {
			writer = &throttledWriter{
				w:           w,
				bytesPerSec: s.BytesPerSec,
			}
		}

		w.Header().Set("ETag", s.ETag)
		w.Header().Set("Accept-Ranges", "bytes")

		// ServeContent handles Range requests automatically
		http.ServeContent(fakeResponseWriter{w, writer}, r, "testfile.bin", time.Now(), s.Content)
	})

	s.Server = httptest.NewServer(handler)
	return s
}

// throttledWriter limits the data transfer speed.
type throttledWriter struct {
	w           io.Writer
	bytesPerSec int64
}

func (t *throttledWriter) Write(p []byte) (n int, err error) {
	n, err = t.w.Write(p)
	if n > 0 && t.bytesPerSec > 0 {
		// Calculate time needed for this amount of data
		duration := time.Duration(n) * time.Second / time.Duration(t.bytesPerSec)
		time.Sleep(duration)
	}
	return n, err
}

// fakeResponseWriter allows us to wrap the underlying writer for ServeContent.
type fakeResponseWriter struct {
	http.ResponseWriter
	w io.Writer
}

func (f fakeResponseWriter) Write(p []byte) (int, error) {
	return f.w.Write(p)
}

// NewSimpleServer create a simple httptest.Server serving 'Hello World!' content
func NewSimpleServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, DefaultWebContent)
	}))
}

// NewLargeRangeServer creates a server with a virtual file of specified size.
func NewLargeRangeServer(sizeMB int) *EnhancedServer {
	content := &DummyContent{Size: int64(sizeMB) * 1024 * 1024}
	return NewEnhancedServer(content)
}

// NewSimpleRangeServer is kept for backward compatibility.
func NewSimpleRangeServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Now(), &DummyContent{Size: int64(len(DefaultWebContent))})
	}))
}
