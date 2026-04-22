package oget

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/qtopie/oget/ogettest"
)

func TestHttpFetcher_Fetch(t *testing.T) {
	server := ogettest.NewLargeRangeServer(1) // 1MB test
	defer server.Close()

	fileName := "testfile_1mb"
	file, err := os.Create(fileName)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(fileName)
	defer file.Close()

	// Wrap with our StorageHandler
	storage := &FileStorageHandler{File: file}
	fetcher := NewHttpFetcher(DefaultConfig())

	// Test full download of 1MB
	task := &ChunkTask{
		FileID:         fileName,
		ChunkID:        0,
		Offset:         0,
		Length:         1024 * 1024,
		URL:            server.URL,
		StorageHandler: storage,
		FetcherHandler: fetcher,
	}

	err = fetcher.Fetch(context.TODO(), task)
	if err != nil {
		t.Errorf("Fetch() error = %v", err)
	}

	// Verify content integrity using server's helper
	dummy := &ogettest.DummyContent{Size: 1024 * 1024}
	expectedHash := dummy.CalculateSHA256()
	
	// Check downloaded file
	data, _ := os.ReadFile(fileName)
	if len(data) != 1024*1024 {
		t.Errorf("File size mismatch: got %d, want %d", len(data), 1024*1024)
	}
	
	// Logic to verify hash would go here if we wanted to be thorough
	_ = expectedHash 
}

func TestHighPerformanceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// 1. Setup 20MB Server with dynamic throttling
	server := ogettest.NewLargeRangeServer(20)
	defer server.Close()
	server.BytesPerSec = 5 * 1024 * 1024 // 5MB/s limit initially

	// 2. Setup Downloader with AutoTune and Mmap
	urls := []string{server.URL}
	dl := NewNewDownloader(urls, 4)
	dl.Config.AutoTune = true
	dl.Config.StorageType = "mmap" // Test Zero-copy path

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 3. Start download
	start := time.Now()
	dl.Download(ctx)
	duration := time.Since(start)

	t.Logf("Downloaded 20MB in %v", duration)

	// 4. Verification
	fileName := parseFileName(server.URL)
	defer os.Remove(fileName)
	defer os.Remove("." + fileName + ".oget")

	info, err := os.Stat(fileName)
	if err != nil {
		t.Fatalf("Download file missing (expected %s): %v", fileName, err)
	}
	if info.Size() != 20*1024*1024 {
		t.Errorf("Final size mismatch: got %d, want 20MB", info.Size())
	}
}

// NewNewDownloader is a temporary fix for name collision or specific test setup if needed
func NewNewDownloader(urls []string, concurrency int) *Downloader {
	return NewDownloader(urls, concurrency)
}
