package oget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseM3U8Segments_MediaPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `#EXTM3U
#EXT-X-VERSION:3
#EXT-X-TARGETDURATION:10
#EXTINF:9.009,
segment_0.ts
#EXTINF:8.500,
https://cdn.example.com/segment_1.ts
#EXT-X-ENDLIST`)
	}))
	defer server.Close()

	chunks, err := ParseM3U8Segments(context.Background(), server.URL+"/media/playlist.m3u8", nil)
	if err != nil {
		t.Fatalf("ParseM3U8Segments failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}

	if chunks[0].SeqID != 0 || chunks[0].URL != server.URL+"/media/segment_0.ts" {
		t.Errorf("chunk 0 unexpected: %+v", chunks[0])
	}
	if chunks[0].Duration < 9*time.Second || chunks[0].Duration > 10*time.Second {
		t.Errorf("chunk 0 duration unexpected: %v", chunks[0].Duration)
	}

	if chunks[1].SeqID != 1 || chunks[1].URL != "https://cdn.example.com/segment_1.ts" {
		t.Errorf("chunk 1 unexpected: %+v", chunks[1])
	}
}

func TestParseM3U8Segments_MasterPlaylist(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/master.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `#EXTM3U
#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=640x360
360p.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=2500000,RESOLUTION=1280x720
720p.m3u8`)
			return
		}
		if r.URL.Path == "/720p.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, `#EXTM3U
#EXT-X-TARGETDURATION:6
#EXTINF:6.0,
chunk_0.ts
#EXTINF:6.0,
chunk_1.ts
#EXT-X-ENDLIST`)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	chunks, err := ParseM3U8Segments(context.Background(), server.URL+"/master.m3u8", nil)
	if err != nil {
		t.Fatalf("ParseM3U8Segments failed: %v", err)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0].URL != server.URL+"/chunk_0.ts" {
		t.Errorf("expected 720p segment url, got: %s", chunks[0].URL)
	}
}

func TestRemuxConcat_Validation(t *testing.T) {
	err := RemuxConcat(context.Background(), nil, "out.mp4", "")
	if err == nil {
		t.Errorf("expected error for empty ts files list")
	}

	tmpDir := t.TempDir()
	dummyFile := filepath.Join(tmpDir, "dummy.ts")
	_ = os.WriteFile(dummyFile, []byte("fake ts"), 0644)

	// If ffmpeg doesn't exist, should fail gracefully with path error
	err = RemuxConcat(context.Background(), []string{dummyFile}, filepath.Join(tmpDir, "out.mp4"), "/non/existent/ffmpeg")
	if err == nil {
		t.Errorf("expected error for invalid ffmpeg path")
	}
}
