package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"testing"
	"time"
)

// ContentBuffer implements some io interfaces for testing purpose.
type ContentBuffer struct {
	Data  []byte
	Index int64
}

func (b *ContentBuffer) String() string {
	return string(b.Data)
}

func (b *ContentBuffer) Len() int {
	return len(b.Data)
}

func (b *ContentBuffer) Grow(size int) {
	b.Data = append(b.Data, make([]byte, size)...)
}

func (b *ContentBuffer) Read(p []byte) (n int, err error) {
	n = copy(p, b.Data[b.Index:])
	if n != len(p) {
		return n, errors.New("Copy failed")
	}
	return n, nil
}

func (b *ContentBuffer) Seek(offset int64, whence int) (ret int64, err error) {
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

func (b *ContentBuffer) ReadAt(p []byte, off int64) (n int, err error) {
	if int(off)+len(p) > b.Len() {
		return 0, errors.New("index out of range")
	}

	n = copy(p, b.Data[off:])
	if n != len(p) {
		return n, errors.New("Copy failed")
	}
	return n, nil
}

func (b *ContentBuffer) WriteAt(p []byte, off int64) (n int, err error) {
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

func TestFetcher_retrieveAll(t *testing.T) {
	webContent := "Hello World!"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, webContent)
	}))
	defer server.Close()

	type fields struct {
		URL    string
		Pieces []RangeHeader
	}
	tests := []struct {
		name    string
		fields  fields
		wantN   int64
		wantW   string
		wantErr bool
	}{
		{"fetchAll_mock", fields{URL: server.URL}, 12, webContent, false},
		{"fetchAll_failed", fields{URL: "invalid-url"}, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{
				URL:    tt.fields.URL,
				Pieces: tt.fields.Pieces,
			}
			w := &bytes.Buffer{}
			gotN, err := f.retrieveAll(w)
			if (err != nil) != tt.wantErr {
				t.Errorf("Fetcher.retrieveAll() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotN != tt.wantN {
				t.Errorf("Fetcher.retrieveAll() = %v, want %v", gotN, tt.wantN)
			}
			if gotW := w.String(); gotW != tt.wantW {
				t.Errorf("Fetcher.retrieveAll() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}

func TestFetcher_retrievePartial(t *testing.T) {
	webContent := "HelloWorld!"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqData, _ := httputil.DumpRequest(r, false)
		t.Log(string(reqData))

		content := &ContentBuffer{
			Data:  []byte(webContent),
			Index: 0,
		}
		http.ServeContent(w, r, "sample.txt", time.Now(), content)
	}))
	defer server.Close()

	type fields struct {
		URL    string
		Pieces []RangeHeader
	}
	type args struct {
		pieceN int
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		wantN   int
		wantW   string
		wantErr bool
	}{
		{"fetchPartial_mock", fields{server.URL, []RangeHeader{{5, 9}}}, args{0}, 5, "World", false},
		{"fetchPartial_mock", fields{"invalid-url", []RangeHeader{{5, 9}}}, args{0}, 0, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{
				URL:    tt.fields.URL,
				Pieces: tt.fields.Pieces,
			}
			w := &ContentBuffer{}
			gotN, err := f.retrievePartial(tt.args.pieceN, w)
			if (err != nil) != tt.wantErr {
				t.Errorf("Fetcher.retrievePartial() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotN != tt.wantN {
				t.Errorf("Fetcher.retrievePartial() = %v, want %v", gotN, tt.wantN)
			}

			rangeHeader := tt.fields.Pieces[tt.args.pieceN]
			gotData := make([]byte, tt.wantN)
			_, err = w.ReadAt(gotData, rangeHeader.StartPos)
			if err != nil {
				t.Log(err)
			}
			if gotW := string(gotData); gotW != tt.wantW {
				t.Errorf("Fetcher.retrievePartial() = %v, want %v", gotW, tt.wantW)
			}
		})
	}
}
