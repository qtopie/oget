package oget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHttpFetcher_Fetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Hello World!")
	}))
	defer server.Close()

	fileName := "test_fetch_basic"
	file, _ := os.Create(fileName)
	defer os.Remove(fileName)
	defer file.Close()

	storage := &FileStorageHandler{File: file}
	// Use standard client for basic test to avoid environment interference
	fetcher := &HttpFetcher{Client: &http.Client{}}

	task := &ChunkTask{
		FileID:         fileName,
		ChunkID:        0,
		Offset:         0,
		Length:         12,
		URL:            server.URL,
		StorageHandler: storage,
		FetcherHandler: fetcher,
	}

	err := fetcher.Fetch(context.TODO(), task)
	if err != nil {
		t.Errorf("Fetch failed: %v", err)
	}

	data, _ := os.ReadFile(fileName)
	if string(data) != "Hello World!" {
		t.Errorf("got %q, want %q", string(data), "Hello World!")
	}
}

func TestHttpFetcher_FetchIncomplete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "20")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "too short") // Only 9 bytes, but promised 20
	}))
	defer server.Close()

	fileName := "test_fetch_incomplete"
	file, _ := os.Create(fileName)
	defer os.Remove(fileName)
	defer file.Close()

	storage := &FileStorageHandler{File: file}
	fetcher := &HttpFetcher{Client: &http.Client{}}

	task := &ChunkTask{
		FileID:         fileName,
		ChunkID:        0,
		Offset:         0,
		Length:         20,
		URL:            server.URL,
		StorageHandler: storage,
		FetcherHandler: fetcher,
	}

	err := fetcher.Fetch(context.TODO(), task)
	if err == nil {
		t.Errorf("Fetch should have failed for incomplete data")
	}
}
