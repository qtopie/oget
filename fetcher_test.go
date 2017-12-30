package main

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"os"
	"strings"
	"testing"
	"time"
)

type ContentBuffer struct {
	Data  []byte
	Index int64
}

func (b *ContentBuffer) Read(p []byte) (n int, err error) {
	n = copy(p, b.Data[b.Index:])
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

func TestFetcher_retrieveAll(t *testing.T) {
	wanted := "abc"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, wanted)
	}))
	defer server.Close()

	tmpfile, err := ioutil.TempFile("", "retrieve_all_test")
	if err != nil {
		t.Errorf("Cannot create temporary file: %v", err)
	}
	defer os.Remove(tmpfile.Name()) // clean up

	fetcher := &Fetcher{
		URL:         server.URL,
		FileName:    tmpfile.Name(),
		FileHandler: tmpfile,
	}

	fetcher.retrieveAll()

	data, err := ioutil.ReadFile(fetcher.FileName)
	if err != nil {
		t.Errorf("Error while reading file: %v", err)
	}
	actual := string(data)

	if 0 != strings.Compare(wanted, actual) {
		t.Errorf("Expected to be %s but instead got %s", wanted, actual)
	}
}

func TestFetcher_retrievePartial(t *testing.T) {
	wanted := "-inte"
	provided := "A non-interactive network downloader tool."

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqData, _ := httputil.DumpRequest(r, false)
		t.Log(string(reqData))

		content := &ContentBuffer{
			Data:  []byte(provided),
			Index: 0,
		}
		http.ServeContent(w, r, "sample.txt", time.Now(), content)
	}))
	defer server.Close()

	tmpfile, err := ioutil.TempFile("", "retrieve_partial_test")
	if err != nil {
		t.Errorf("Cannot create tmp file: %v", err)
	}
	defer tmpfile.Close()
	defer os.Remove(tmpfile.Name()) // clean up
	t.Log(tmpfile.Name())

	var pieces []RangeHeader
	pieces = append(pieces, RangeHeader{StartPos: 5, EndPos: 9})
	fetcher := &Fetcher{
		URL:         server.URL,
		FileName:    tmpfile.Name(),
		Pieces:      pieces,
		FileHandler: tmpfile,
	}

	err = fetcher.retrievePartial(0)
	if err != nil {
		t.Errorf("Got an error: %v", err)
	}

	length := 9 - 5 + 1
	data := make([]byte, length)
	n, err := fetcher.FileHandler.ReadAt(data, 5)
	if err != nil {
		t.Errorf("Error when reading file: %v", err)
	}
	if n != length {
		t.Error("Downloaded file is not complete")
	}
	actual := string(data)

	if 0 != strings.Compare(wanted, actual) {
		t.Errorf("Expected to be %s but instead got %s", wanted, actual)
	}
}
