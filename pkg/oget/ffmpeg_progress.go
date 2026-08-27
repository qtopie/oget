package oget

import (
	"bufio"
	"io"
	"strconv"
	"strings"
)

// FFmpegProgress represents a snapshot of progress emitted by ffmpeg with -progress pipe:1
type FFmpegProgress struct {
	Frame       int64
	FPS         float64
	TotalSize   int64
	OutTimeUS   int64
	OutTimeStr  string
	Speed       string
	IsEnd       bool
}

// ParseFFmpegProgress streams and parses key=value progress output from ffmpeg.
// onUpdate is invoked whenever a "progress=" block delimiter is encountered or relevant fields are updated.
func ParseFFmpegProgress(r io.Reader, onUpdate func(p FFmpegProgress)) error {
	scanner := bufio.NewScanner(r)
	var current FFmpegProgress

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "frame":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.Frame = n
			}
		case "fps":
			if f, err := strconv.ParseFloat(val, 64); err == nil {
				current.FPS = f
			}
		case "total_size":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.TotalSize = n
			}
		case "out_time_us":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.OutTimeUS = n
			}
		case "out_time_ms":
			if n, err := strconv.ParseInt(val, 10, 64); err == nil {
				current.OutTimeUS = n * 1000
			}
		case "out_time":
			current.OutTimeStr = val
		case "speed":
			current.Speed = val
		case "progress":
			if val == "end" {
				current.IsEnd = true
			}
			if onUpdate != nil {
				onUpdate(current)
			}
		}
	}

	return scanner.Err()
}
