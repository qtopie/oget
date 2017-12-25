package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
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

// Download downloads a file from remote site
func (f *Fetcher) Download() {
	req, err := http.NewRequest(http.MethodHead, f.URL, nil)
	if err != nil {
		log.Fatalf("Invalid url for downloading: %s, error: %v", f.URL, err)
	}
	req.Header.Set("Range", "bytes=0-")

	client := &http.Client{
		Timeout: time.Second * 5,
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()

	rangeSatisfiable := false
	switch resp.StatusCode {
	case http.StatusPartialContent:
		rangeSatisfiable = true
		log.Println("Partial Content is supported.")
	case http.StatusOK, http.StatusRequestedRangeNotSatisfiable:
		log.Println(f.URL, "does not support for range request.")
	default:
		log.Fatal("Got unexpected status code", resp.StatusCode)
		return
	}

	if rangeSatisfiable {
		length, _ := strconv.Atoi(resp.Header.Get("Content-Length"))

		if err != nil {
			log.Fatal(err)
			return
		}

		var pieces []RangeHeader
		rangeSize := splitSize(int64(length))
		amount := int(int64(length) / rangeSize)
		if int64(length)%rangeSize != 0 {
			amount = amount + 1
		}

		for i := 0; i < amount; i++ {
			if i == amount-1 {
				pieces = append(pieces, RangeHeader{
					StartPos: int64(i) * RangeSize,
					EndPos:   int64(length - 1),
				})
			} else {
				pieces = append(pieces, RangeHeader{
					StartPos: int64(i) * RangeSize,
					EndPos:   int64(i)*RangeSize + RangeSize - 1,
				})
			}
		}
		f.Pieces = pieces

		var wg sync.WaitGroup
		for i := 0; i < amount; i++ {
			wg.Add(1)
			log.Println("downloading", i)
			go func(pieceN int) {
				defer wg.Done()
				err := f.retrievePartial(pieceN)
				if err != nil {
					log.Fatal(err)
				}
			}(i)
		}

		wg.Wait()
		// checkfile completes
		if fi, _ := f.FileHandler.Stat(); int64(length) != fi.Size() {
			log.Fatalf("Downloaded file is not completed, remote: %v, local: %v", length, fi.Size())
		}
	} else {
		err := f.retrieveAll()
		if err != nil {
			log.Fatal(err)
		}
	}

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
