package oget

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
)

// FFmpegFetcher delegates video stream downloading and remuxing to ffmpeg CLI.
type FFmpegFetcher struct {
	Config *Config
}

// NewFFmpegFetcher creates a new FFmpegFetcher instance.
func NewFFmpegFetcher(config *Config) *FFmpegFetcher {
	return &FFmpegFetcher{Config: config}
}

// Fetch executes ffmpeg to download and remux the stream to the destination file.
func (f *FFmpegFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	var customPath string
	if f.Config != nil {
		customPath = f.Config.FFmpegPath
	}

	ffmpegBin, err := FindFFmpeg(customPath)
	if err != nil {
		return err
	}

	outputFile := task.FileID
	if dir := filepath.Dir(outputFile); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	args := BuildFFmpegArgs(task.URL, outputFile, f.Config)
	if f.Config != nil && f.Config.Verbose {
		log.Printf("[FFmpeg] Executing: %s %s", ffmpegBin, strings.Join(args, " "))
	}

	cmd := exec.CommandContext(ctx, ffmpegBin, args...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdout pipe for ffmpeg: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to create stderr pipe for ffmpeg: %w", err)
	}

	// Capture stderr for diagnostics in case ffmpeg errors out
	var stderrBuf bytes.Buffer
	stderrDone := make(chan struct{})
	go func() {
		defer close(stderrDone)
		_, _ = io.Copy(&stderrBuf, stderrPipe)
	}()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ffmpeg: %w", err)
	}

	var lastTotalSize int64
	progressDone := make(chan error, 1)
	go func() {
		err := ParseFFmpegProgress(stdoutPipe, func(p FFmpegProgress) {
			currentSize := p.TotalSize
			if currentSize > atomic.LoadInt64(&lastTotalSize) {
				delta := currentSize - atomic.LoadInt64(&lastTotalSize)
				atomic.StoreInt64(&lastTotalSize, currentSize)
				if task.OnProgress != nil {
					task.OnProgress(int(delta))
				}
			}
		})
		progressDone <- err
	}()

	waitErr := cmd.Wait()
	<-stderrDone
	_ = <-progressDone

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if waitErr != nil {
		stderrOutput := strings.TrimSpace(stderrBuf.String())
		if len(stderrOutput) > 1024 {
			stderrOutput = stderrOutput[len(stderrOutput)-1024:]
		}
		return fmt.Errorf("ffmpeg exited with error: %w (stderr: %s)", waitErr, stderrOutput)
	}

	if task.OnChunkComplete != nil {
		task.OnChunkComplete(task.ChunkID, "")
	}

	return nil
}
