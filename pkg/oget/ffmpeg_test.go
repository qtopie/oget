package oget

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsVideoStreamResource(t *testing.T) {
	tests := []struct {
		url      string
		expected bool
	}{
		{"https://example.com/live/playlist.m3u8", true},
		{"https://example.com/video.M3U8?token=xyz", true},
		{"https://example.com/manifest.mpd", true},
		{"https://example.com/manifest.MPD?auth=123", true},
		{"https://example.com/file.zip", false},
		{"https://example.com/archive.tar.gz", false},
		{"magnet:?xt=urn:btih:abc", false},
	}

	for _, tt := range tests {
		got := isVideoStreamResource(tt.url)
		if got != tt.expected {
			t.Errorf("isVideoStreamResource(%q) = %v; want %v", tt.url, got, tt.expected)
		}
	}
}

func TestParseFileName_VideoStream(t *testing.T) {
	tests := []struct {
		url      string
		expected string
	}{
		{"https://example.com/live/playlist.m3u8", "playlist.mp4"},
		{"https://example.com/live/STREAM.M3U8?token=123", "STREAM.mp4"},
		{"https://example.com/media/manifest.mpd", "manifest.mp4"},
		{"https://example.com/media/video.mp4.mpd", "video.mp4"},
		{"https://example.com/archive.zip", "archive.zip"},
	}

	for _, tt := range tests {
		got := parseFileName(tt.url)
		if got != tt.expected {
			t.Errorf("parseFileName(%q) = %q; want %q", tt.url, got, tt.expected)
		}
	}
}

func TestParseFFmpegProgress(t *testing.T) {
	sampleOutput := `
frame=100
fps=25.0
stream_0_0_q=-1.0
bitrate=1234.5kbits/s
total_size=1048576
out_time_us=4000000
out_time=00:00:04.000000
dup_frames=0
drop_frames=0
speed=2.5x
progress=continue
frame=200
fps=25.0
total_size=2097152
out_time_us=8000000
speed=3.0x
progress=end
`

	var updates []FFmpegProgress
	err := ParseFFmpegProgress(strings.NewReader(sampleOutput), func(p FFmpegProgress) {
		updates = append(updates, p)
	})

	if err != nil {
		t.Fatalf("ParseFFmpegProgress returned unexpected error: %v", err)
	}

	if len(updates) != 2 {
		t.Fatalf("expected 2 progress updates, got %d", len(updates))
	}

	u1 := updates[0]
	if u1.Frame != 100 || u1.TotalSize != 1048576 || u1.OutTimeUS != 4000000 || u1.Speed != "2.5x" || u1.IsEnd {
		t.Errorf("unexpected values in update 1: %+v", u1)
	}

	u2 := updates[1]
	if u2.Frame != 200 || u2.TotalSize != 2097152 || u2.OutTimeUS != 8000000 || u2.Speed != "3.0x" || !u2.IsEnd {
		t.Errorf("unexpected values in update 2: %+v", u2)
	}
}

func TestBuildFFmpegArgs(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Headers = map[string]string{
		"Referer":    "https://example.com/ref",
		"User-Agent": "CustomAgent/1.0",
	}
	cfg.ProxyURL = "http://127.0.0.1:8080"

	args := BuildFFmpegArgs("https://example.com/live.m3u8", "output.mp4", cfg)
	argsStr := strings.Join(args, " ")

	if !strings.Contains(argsStr, "-y") {
		t.Errorf("expected -y in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-http_proxy http://127.0.0.1:8080") {
		t.Errorf("expected -http_proxy in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-headers") || !strings.Contains(argsStr, "Referer: https://example.com/ref") {
		t.Errorf("expected custom headers in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-c copy") {
		t.Errorf("expected -c copy in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-bsf:a aac_adtstoasc") {
		t.Errorf("expected -bsf:a aac_adtstoasc in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-movflags +faststart") {
		t.Errorf("expected -movflags +faststart in args, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-progress pipe:1") {
		t.Errorf("expected -progress pipe:1 in args, got: %s", argsStr)
	}
	if !strings.HasSuffix(argsStr, "output.mp4") {
		t.Errorf("expected output.mp4 at end of args, got: %s", argsStr)
	}
}

func TestFindFFmpeg(t *testing.T) {
	// Should find system ffmpeg if installed
	path, err := FindFFmpeg("")
	if err == nil {
		if path == "" {
			t.Errorf("expected non-empty path for found ffmpeg")
		}
	}

	// Should return error for non-existent custom path
	_, err = FindFFmpeg("/non/existent/path/to/ffmpeg_fake_bin")
	if err == nil {
		t.Errorf("expected error for non-existent ffmpeg path")
	}
}

func TestFFmpegProberAndFetcher_LocalFile(t *testing.T) {
	ffmpegPath, err := FindFFmpeg("")
	if err != nil {
		t.Skip("ffmpeg not available on host, skipping execution test")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXT-X-ENDLIST")
	}))
	defer server.Close()

	testM3U8URL := server.URL + "/test.m3u8"
	tmpDir := t.TempDir()
	cfg := DefaultConfig()
	cfg.FFmpegPath = ffmpegPath

	prober := NewFFmpegProber(cfg)
	meta, err := prober.Probe(context.Background(), testM3U8URL)
	if err != nil {
		t.Fatalf("Probe failed: %v", err)
	}
	if meta.Size != 0 {
		t.Errorf("expected size 0 for stream prober, got %d", meta.Size)
	}

	// Test fetcher dispatch routing
	targetFile := filepath.Join(tmpDir, "dummy_out.mp4")
	fetcher := GetFetcher(testM3U8URL, cfg)
	if _, ok := fetcher.(*FFmpegFetcher); !ok {
		t.Fatalf("expected *FFmpegFetcher for .m3u8 URL, got %T", fetcher)
	}

	// Clean up after test
	_ = os.Remove(targetFile)
}
