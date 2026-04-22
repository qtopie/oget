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

func (d *DummyContent) CalculateSHA256() string {
	h := sha256.New()
	buf := make([]byte, 32*1024)
	// Create a temporary clone for hashing to not affect state
	clone := &DummyContent{Size: d.Size}
	for {
		n, err := clone.Read(buf)
		if n > 0 {
			h.Write(buf[:n])
		}
		if err != nil {
			break
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

type EnhancedServer struct {
	*httptest.Server
	Content     *DummyContent
	BytesPerSec int64
	Latency     time.Duration
	ETag        string
}

func NewEnhancedServer(content *DummyContent) *EnhancedServer {
	s := &EnhancedServer{
		Content: content,
		ETag:    "initial-etag",
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if s.Latency > 0 {
			time.Sleep(s.Latency)
		}

		var writer io.Writer = w
		if s.BytesPerSec > 0 {
			writer = &throttledWriter{
				w:           w,
				bytesPerSec: s.BytesPerSec,
			}
		}

		w.Header().Set("ETag", s.ETag)
		w.Header().Set("Accept-Ranges", "bytes")

		// Important: Create a fresh reader for each request to avoid seek conflicts
		reader := &DummyContent{Size: s.Content.Size}
		http.ServeContent(fakeResponseWriter{w, writer}, r, "testfile.bin", time.Now(), reader)
	})

	s.Server = httptest.NewServer(handler)
	return s
}

type throttledWriter struct {
	w           io.Writer
	bytesPerSec int64
}

func (t *throttledWriter) Write(p []byte) (n int, err error) {
	n, err = t.w.Write(p)
	if n > 0 && t.bytesPerSec > 0 {
		duration := time.Duration(n) * time.Second / time.Duration(t.bytesPerSec)
		time.Sleep(duration)
	}
	return n, err
}

type fakeResponseWriter struct {
	http.ResponseWriter
	w io.Writer
}

func (f fakeResponseWriter) Write(p []byte) (int, error) {
	return f.w.Write(p)
}

func NewSimpleServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, DefaultWebContent)
	}))
}

func NewLargeRangeServer(sizeMB int) *EnhancedServer {
	content := &DummyContent{Size: int64(sizeMB) * 1024 * 1024}
	return NewEnhancedServer(content)
}

func NewSimpleRangeServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, "", time.Now(), &DummyContent{Size: int64(len(DefaultWebContent))})
	}))
}
