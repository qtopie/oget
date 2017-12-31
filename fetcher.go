package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
)

// RangeSize sets the default range size to 1MB
const (
	RangeSize    int64 = 1024 * 1024
	ThreadAmount int   = 32
)

// RangeHeader defines the part of file to download.
type RangeHeader struct {
	StartPos int64
	EndPos   int64
}

func (h *RangeHeader) String() string {
	return fmt.Sprintf("bytes=%d-%d", h.StartPos, h.EndPos)
}

// Fetcher downloads file from URL.
type Fetcher struct {
	URL    string
	Pieces []RangeHeader
}

// retrieveAll downloads the file completely.
func (f *Fetcher) retrieveAll(w io.Writer) (int64, error) {
	resp, err := http.Get(f.URL)
	if err != nil {
		return 0, err
	}

	n, err := io.Copy(w, resp.Body)
	return n, err
}

// retrievePartial downloads part of the file.
func (f *Fetcher) retrievePartial(pieceN int, w io.WriterAt) (n int, err error) {
	if pieceN < 0 || pieceN >= len(f.Pieces) {
		return 0, errors.New("Unspported index")
	}
	s := f.Pieces[pieceN]

	// make HTTP Range request to get file from server
	req, err := http.NewRequest(http.MethodGet, f.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Range", s.String())

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	// read data from response and write it
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	n, err = w.WriteAt(data, s.StartPos)
	return
}

func splitSize(length int64) (size int64) {
	// less than 1KB
	if length <= 1024 {
		return 1024
	}

	size = length / int64(ThreadAmount)
	if length%32 != 0 {
		size = size + 1
	}

	return
}
