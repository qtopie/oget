package oget

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadState(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "oget-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	statePath := filepath.Join(tmpDir, "test.oget")
	url := "http://example.com/testfile"
	fileSize := int64(10 * 1024 * 1024) // 10MB
	chunkSize := int64(1 * 1024 * 1024) // 1MB

	// 1. Create new state
	state, err := NewDownloadState(url, fileSize, chunkSize, statePath)
	if err != nil {
		t.Fatalf("failed to create state: %v", err)
	}
	state.ETag = "test-etag"

	// 2. Mark some chunks complete
	state.MarkComplete(0, "hash0")
	state.MarkComplete(5, "hash5")
	state.MarkComplete(9, "hash9")

	if !state.IsComplete(0) || !state.IsComplete(5) || !state.IsComplete(9) {
		t.Error("expected chunks to be complete")
	}
	if state.IsComplete(1) {
		t.Error("expected chunk 1 to be incomplete")
	}

	// 3. Save and Close
	if err := state.Save(); err != nil {
		t.Fatalf("failed to save state: %v", err)
	}
	state.Close()

	// 4. Reload state
	reloaded, err := LoadState(statePath)
	if err != nil {
		t.Fatalf("failed to load state: %v", err)
	}
	defer reloaded.Close()

	// 5. Verify metadata
	if reloaded.URL != url || reloaded.FileSize != fileSize || reloaded.ETag != "test-etag" {
		t.Errorf("metadata mismatch: %+v", reloaded)
	}

	// 6. Verify bitset
	if !reloaded.IsComplete(0) || !reloaded.IsComplete(5) || !reloaded.IsComplete(9) {
		t.Error("reloaded state: expected chunks to be complete")
	}
	if reloaded.IsComplete(1) {
		t.Error("reloaded state: expected chunk 1 to be incomplete")
	}

	// 7. Verify PercentComplete
	percent := reloaded.PercentComplete()
	expected := 30.0 // 3 out of 10 chunks
	if percent != expected {
		t.Errorf("expected %.1f%% complete, got %.1f%%", expected, percent)
	}
}
