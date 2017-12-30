package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
)

// RangeSize is the default range size to 1MB
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
	URL      string
	FileName string
	Pieces   []RangeHeader
	// TO-DO Use interface, maybe io.WriterCloser
	FileHandler *os.File
}

// retrieveAll downloads the file completely.
func (f *Fetcher) retrieveAll() (err error) {

	resp, err := http.Get(f.URL)
	if err != nil {
		return
	}

	_, err = io.Copy(f.FileHandler, resp.Body)

	return
}

// retrievePartial downloads part of the file.
func (f *Fetcher) retrievePartial(pieceN int) (err error) {
	if pieceN < 0 || pieceN >= len(f.Pieces) {
		return errors.New("Unspported index")
	}
	s := f.Pieces[pieceN]

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

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	//just write to file
	n, err := f.FileHandler.WriteAt(data, int64(s.StartPos))
	if err == nil && n != len(data) {
		err = errors.New("Downloading is not complete")
	}
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
