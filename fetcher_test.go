package oget

import (
	"context"
	"os"
	"testing"

	"github.com/qtopie/oget/ogettest"
)

func TestHttpFetcher_Fetch(t *testing.T) {
	server := ogettest.NewSimpleRangeServer()
	defer server.Close()

	fileName := "testfile"
	file, _ := os.Create(fileName)
	defer os.Remove(fileName)
	defer file.Close()

	fetcher := NewHttpFetcher()

	// Test full download (if offset 0 and length is known)
	task := &ChunkTask{
		FileID:         fileName,
		ChunkID:        0,
		Offset:         0,
		Length:         12, // "Hello World!"
		URL:            server.URL,
		FileHandler:    file,
		FetcherHandler: fetcher,
	}

	err := fetcher.Fetch(context.TODO(), task)
	if err != nil {
		t.Errorf("Fetch() error = %v", err)
	}

	data, _ := os.ReadFile(fileName)
	if string(data) != ogettest.DefaultWebContent {
		t.Errorf("Fetch() got = %v, want %v", string(data), ogettest.DefaultWebContent)
	}
}

func TestHttpFetcher_FetchPartial(t *testing.T) {
	server := ogettest.NewSimpleRangeServer()
	defer server.Close()

	fileName := "testfile_partial"
	file, _ := os.Create(fileName)
	defer os.Remove(fileName)
	defer file.Close()

	fetcher := NewHttpFetcher()

	// Fetch "World" (index 6 to 10)
	task := &ChunkTask{
		FileID:         fileName,
		ChunkID:        1,
		Offset:         6,
		Length:         5,
		URL:            server.URL,
		FileHandler:    file,
		FetcherHandler: fetcher,
	}

	err := fetcher.Fetch(context.TODO(), task)
	if err != nil {
		t.Errorf("Fetch() error = %v", err)
	}

	data := make([]byte, 5)
	file.ReadAt(data, 6)
	if string(data) != "World" {
		t.Errorf("Fetch() got = %v, want %v", string(data), "World")
	}
}
