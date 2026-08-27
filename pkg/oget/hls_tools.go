package oget

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// HLSChunk represents a discrete media segment in an M3U8 playlist.
type HLSChunk struct {
	SeqID    int           `json:"seq_id"`
	URL      string        `json:"url"`
	Duration time.Duration `json:"duration"`
}

// resolveAbsoluteURL resolves a relative target URL against a base URL.
func resolveAbsoluteURL(baseURLStr, targetURLStr string) (string, error) {
	targetURL, err := url.Parse(strings.TrimSpace(targetURLStr))
	if err != nil {
		return "", err
	}
	if targetURL.IsAbs() {
		return targetURL.String(), nil
	}

	baseURL, err := url.Parse(baseURLStr)
	if err != nil {
		return "", err
	}
	return baseURL.ResolveReference(targetURL).String(), nil
}

// ParseM3U8Segments parses an M3U8 playlist (Master or Media) and returns all segment chunks with absolute URLs.
func ParseM3U8Segments(ctx context.Context, m3u8URL string, headers map[string]string) ([]HLSChunk, error) {
	client := &http.Client{
		Timeout: 15 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m3u8URL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", fmt.Sprintf("oget/%s", Version))
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch m3u8 playlist: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("failed to fetch m3u8 playlist: HTTP %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read m3u8 content: %w", err)
	}

	content := string(bodyBytes)
	lines := strings.Split(content, "\n")

	// Check if this is a Master Playlist (contains #EXT-X-STREAM-INF)
	var variantURLs []string
	var maxBandwidth int64
	var bestVariantURL string

	isMaster := false
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "#EXT-X-STREAM-INF:") {
			isMaster = true
			var bw int64
			// Extract BANDWIDTH=...
			if idx := strings.Index(line, "BANDWIDTH="); idx != -1 {
				bwStr := line[idx+len("BANDWIDTH="):]
				if endIdx := strings.IndexAny(bwStr, ", \r\n"); endIdx != -1 {
					bwStr = bwStr[:endIdx]
				}
				bw, _ = strconv.ParseInt(bwStr, 10, 64)
			}

			// Next non-empty, non-comment line is the variant URL
			for j := i + 1; j < len(lines); j++ {
				next := strings.TrimSpace(lines[j])
				if next != "" && !strings.HasPrefix(next, "#") {
					absVariant, err := resolveAbsoluteURL(m3u8URL, next)
					if err == nil {
						variantURLs = append(variantURLs, absVariant)
						if bw > maxBandwidth || bestVariantURL == "" {
							maxBandwidth = bw
							bestVariantURL = absVariant
						}
					}
					i = j
					break
				}
			}
		}
	}

	if isMaster && bestVariantURL != "" {
		// Recursively parse the highest-bandwidth media playlist
		return ParseM3U8Segments(ctx, bestVariantURL, headers)
	}

	// Parse Media Playlist
	var chunks []HLSChunk
	var currentDuration time.Duration
	seqID := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "#EXTINF:") {
			// #EXTINF:10.000,
			durStr := strings.TrimPrefix(trimmed, "#EXTINF:")
			if commaIdx := strings.Index(durStr, ","); commaIdx != -1 {
				durStr = durStr[:commaIdx]
			}
			if durSec, err := strconv.ParseFloat(strings.TrimSpace(durStr), 64); err == nil {
				currentDuration = time.Duration(durSec * float64(time.Second))
			}
			continue
		}

		if strings.HasPrefix(trimmed, "#") {
			// Skip other tags (e.g. #EXT-X-TARGETDURATION, #EXT-X-DISCONTINUITY, etc.)
			continue
		}

		// This line is a segment URI
		absSegmentURL, err := resolveAbsoluteURL(m3u8URL, trimmed)
		if err != nil {
			continue
		}

		chunks = append(chunks, HLSChunk{
			SeqID:    seqID,
			URL:      absSegmentURL,
			Duration: currentDuration,
		})
		seqID++
		currentDuration = 0
	}

	if len(chunks) == 0 {
		return nil, fmt.Errorf("no media segments found in m3u8 playlist at %s", m3u8URL)
	}

	return chunks, nil
}

// RemuxConcat concatenates a sequence of local TS files into a single destination video file using FFmpeg.
func RemuxConcat(ctx context.Context, tsFiles []string, outputFile string, ffmpegPath string) error {
	if len(tsFiles) == 0 {
		return fmt.Errorf("no ts files provided for concatenation")
	}

	ffmpegBin, err := FindFFmpeg(ffmpegPath)
	if err != nil {
		return err
	}

	if outDir := filepath.Dir(outputFile); outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory %s: %w", outDir, err)
		}
	}

	// Create temporary concat list file
	listFile, err := os.CreateTemp("", "oget_concat_*.txt")
	if err != nil {
		return fmt.Errorf("failed to create temporary concat list: %w", err)
	}
	listPath := listFile.Name()
	defer os.Remove(listPath)

	writer := bufio.NewWriter(listFile)
	for _, f := range tsFiles {
		absPath, err := filepath.Abs(f)
		if err != nil {
			absPath = f
		}
		// Escape single quotes for ffmpeg concat demuxer
		escaped := strings.ReplaceAll(absPath, "'", "'\\''")
		if _, err := fmt.Fprintf(writer, "file '%s'\n", escaped); err != nil {
			listFile.Close()
			return fmt.Errorf("failed to write to concat list: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		listFile.Close()
		return fmt.Errorf("failed to flush concat list: %w", err)
	}
	listFile.Close()

	// Execute ffmpeg concat demuxer
	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listPath,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		outputFile,
	}

	cmd := exec.CommandContext(ctx, ffmpegBin, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg remux failed: %w (output: %s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}
