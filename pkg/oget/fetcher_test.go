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

func TestHttpFetcher_FetchChecksum(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "Hello World!")
	}))
	defer server.Close()

	// Case 1: Checksum disabled by default
	{
		fileName := "test_fetch_no_checksum"
		file, _ := os.Create(fileName)
		defer os.Remove(fileName)
		defer file.Close()

		storage := &FileStorageHandler{File: file}
		fetcher := &HttpFetcher{
			Client: &http.Client{},
			Config: &Config{Checksum: false},
		}

		var gotHash string
		completed := false
		task := &ChunkTask{
			FileID:         fileName,
			ChunkID:        0,
			Offset:         0,
			Length:         12,
			URL:            server.URL,
			StorageHandler: storage,
			FetcherHandler: fetcher,
			OnChunkComplete: func(chunkID int, hash string) {
				gotHash = hash
				completed = true
			},
		}

		err := fetcher.Fetch(context.TODO(), task)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if !completed {
			t.Error("OnChunkComplete was not called")
		}
		if gotHash != "" {
			t.Errorf("expected empty hash when checksum is disabled, got %q", gotHash)
		}
	}

	// Case 2: Checksum enabled
	{
		fileName := "test_fetch_with_checksum"
		file, _ := os.Create(fileName)
		defer os.Remove(fileName)
		defer file.Close()

		storage := &FileStorageHandler{File: file}
		fetcher := &HttpFetcher{
			Client: &http.Client{},
			Config: &Config{Checksum: true},
		}

		var gotHash string
		completed := false
		task := &ChunkTask{
			FileID:         fileName,
			ChunkID:        0,
			Offset:         0,
			Length:         12,
			URL:            server.URL,
			StorageHandler: storage,
			FetcherHandler: fetcher,
			OnChunkComplete: func(chunkID int, hash string) {
				gotHash = hash
				completed = true
			},
		}

		err := fetcher.Fetch(context.TODO(), task)
		if err != nil {
			t.Fatalf("Fetch failed: %v", err)
		}
		if !completed {
			t.Error("OnChunkComplete was not called")
		}
		// "Hello World!" SHA-256 is "7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069"
		expectedHash := "7f83b1657ff1fc53b92dc18148a1d65dfc2d4b1fa3d677284addd200126d9069"
		if gotHash != expectedHash {
			t.Errorf("expected hash %q, got %q", expectedHash, gotHash)
		}
	}
}

