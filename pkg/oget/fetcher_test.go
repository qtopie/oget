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

func TestRequester_Probe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "12")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	r := NewRequester(server.URL, DefaultConfig())
	length, _, _, err := r.probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if length != 12 {
		t.Errorf("got %d, want 12", length)
	}
}
