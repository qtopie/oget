package ogettest

import (
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"strings"
	"time"
)

const (
	// DefaultWebContent stores "Hello World!" text content.
	DefaultWebContent = "Hello World!"
)

// RangeBuffer is an alternative for bytes.Buffer but implements io.Seeker
// and some other interface for range testing.
type RangeBuffer struct {
	Data  []byte
	Index int64
}

func (b *RangeBuffer) String() string {
	return string(b.Data)
}

func (b *RangeBuffer) Len() int {
	return len(b.Data)
}

func (b *RangeBuffer) Grow(size int) {
	b.Data = append(b.Data, make([]byte, size)...)
}

func (b *RangeBuffer) Read(p []byte) (n int, err error) {
	n = copy(p, b.Data[b.Index:])
	if n != len(p) {
		return n, errors.New("Copy failed")
	}
	return n, nil
}

func (b *RangeBuffer) Seek(offset int64, whence int) (ret int64, err error) {
	switch whence {
	case io.SeekStart:
		if offset >= int64(len(b.Data)) || offset < 0 {
			err = errors.New("Invalid offset")
		} else {
			b.Index = offset
			return b.Index, nil
		}
	case io.SeekEnd:
		return int64(len(b.Data)), nil
	default:
		err = errors.New("Unsupported seek method")
	}

	return 0, err
}

func (b *RangeBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if int(off)+len(p) > b.Len() {
		return 0, errors.New("index out of range")
	}

	n = copy(p, b.Data[off:])
	if n != len(p) {
		return n, errors.New("Copy failed")
	}
	return n, nil
}

func (b *RangeBuffer) WriteAt(p []byte, off int64) (n int, err error) {
	if int(off)+len(p) > b.Len() {
		size := int(off) + len(p) - b.Len()
		b.Data = append(b.Data, make([]byte, size)...)
	}

	n = copy(b.Data[off:], p)
	if n != len(p) {
		return n, errors.New("Copy failed")
	}

	return n, nil
}

// NewSimpleServer create a simple httptest.Server serving 'Hello World!' content
func NewSimpleServer() *httptest.Server {
	return NewServer(strings.NewReader(DefaultWebContent))
}

// NewServer creates a simple httptest.Server with an io.Reader for writing content.
func NewServer(contentReader io.Reader) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(w, contentReader)
		if err != nil {
			log.Fatal(err)
		}
	}))
}

// NewSimpleRangeServer creates a simple httptest.Server serving 'Hello World!' content.
func NewSimpleRangeServer() *httptest.Server {
	return NewRangeServer(strings.NewReader(DefaultWebContent))
}

// NewRangeServer creates a simple httptest.Server with an io.Reader for writing content.
func NewRangeServer(content io.ReadSeeker) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// log request
		reqData, _ := httputil.DumpRequest(r, false)
		log.Println(string(reqData))

		http.ServeContent(w, r, "", time.Now(), content)
	}))
}
