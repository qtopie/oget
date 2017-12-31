package main

import (
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type StateCode int

const (
	READY    = 1
	RUNNING  = 2
	STOPPED  = 3 // Can be resumed
	FINISHED = 4
	FAILED   = 5
)

type Work struct {
	FetcherHandler Fetcher // multiple
	State          StateCode
	FileName       string // TODO remove?
	Length         int64
	FileHandler    *os.File
}

func (w *Work) parse(cmd Command) {
	length, err := probe(cmd.URL)
	if err != nil {
		log.Fatal(err)
		return
	}

	w.FileName = parseFileName(cmd.URL)
	log.Println("Downloading file", cmd.URL, w.FileName)

	file, err := os.Create(w.FileName)
	if err != nil {
		log.Fatal(err)
	}
	// defer file.Close()
	w.FileHandler = file

	w.FetcherHandler = Fetcher{
		URL: cmd.URL,
	}

	if length > 0 {
		// break-point downloading
		w.Length = length
		var pieces []RangeHeader
		rangeSize := splitSize(int64(w.Length))
		amount := int(int64(w.Length) / rangeSize)
		if int64(w.Length)%rangeSize != 0 {
			amount = amount + 1
		}

		for i := 0; i < amount; i++ {
			if i == amount-1 {
				pieces = append(pieces, RangeHeader{
					StartPos: int64(i) * RangeSize,
					EndPos:   int64(w.Length - 1),
				})
			} else {
				pieces = append(pieces, RangeHeader{
					StartPos: int64(i) * RangeSize,
					EndPos:   int64(i)*RangeSize + RangeSize - 1,
				})
			}
		}
		w.FetcherHandler.Pieces = pieces
	}

	w.State = READY
}

func (w *Work) run() {
	w.State = RUNNING
	if w.Length == 0 {
		// download directly
		if w.Length == 0 {
			_, err := w.FetcherHandler.retrieveAll(w.FileHandler)
			if err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	// support for range request
	amount := len(w.FetcherHandler.Pieces)
	var wg sync.WaitGroup
	for i := 0; i < amount; i++ {
		wg.Add(1)
		log.Println("downloading", i)
		go func(pieceN int) {
			defer wg.Done()
			_, err := w.FetcherHandler.retrievePartial(pieceN, w.FileHandler)
			if err != nil {
				log.Fatal(err)
			}
		}(i)
	}

	wg.Wait()
	w.finish()
}

func (w *Work) stop() {
	w.State = STOPPED
}

func (w *Work) resume() {
	w.State = RUNNING
}

func (w *Work) finish() {
	// checkfile completes
	// if fi, _ := w.FetcherHandler.FileHandler.Stat(); int64(w.Length) != fi.Size() {
	// 	log.Fatalf("Downloaded file is not completed, remote: %v, local: %v", w.Length, fi.Size())
	// }

	log.Println("Downloaded file to", w.FileName)

	w.State = FINISHED
}

func (w *Work) monitor() {

}

// probe makes am HTTP request to the site and return site infomation.
// If site is not reachable, return non-nil error.
// If site supports for range request, return the file length (should be greater than 0).
func probe(url string) (length int64, err error) {
	// Check whether site is reachable
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		log.Printf("Cannot create http request with the URL: %s, error: %v", url, err)
		return
	}

	// Do HTTP HEAD request with range header to this site
	client := &http.Client{
		Timeout: time.Second * 5,
	}
	req.Header.Set("Range", "bytes=0-")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("Remote site is not reachable: %s, error: %v", url, err)
		return
	}
	defer resp.Body.Close()

	// Collect site infomation
	switch resp.StatusCode {
	case http.StatusPartialContent:
		log.Println("Break-point is supported in this downloading task.")

		attr := resp.Header.Get("Content-Length")
		length, err = strconv.ParseInt(attr, 10, 0)
		if err != nil {
			log.Fatal(err)
		}
	case http.StatusOK, http.StatusRequestedRangeNotSatisfiable:
		log.Println(url, "does not support for range request.")
		// set length to N/A or unknown
		length = 0
	default:
		log.Fatal("Got unexpected status code", resp.StatusCode)
		err = errors.New("Unexpected error response when access site: " + url)
	}

	return
}
