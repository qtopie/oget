package oget

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// FindFFmpeg resolves the path to the ffmpeg executable.
func FindFFmpeg(customPath string) (string, error) {
	if customPath != "" {
		if path, err := exec.LookPath(customPath); err == nil {
			return path, nil
		}
		if _, err := os.Stat(customPath); err == nil {
			return customPath, nil
		}
		return "", fmt.Errorf("specified ffmpeg executable not found at %q", customPath)
	}

	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", fmt.Errorf("ffmpeg not found in PATH: please install ffmpeg (e.g. 'sudo apt install ffmpeg' or 'brew install ffmpeg') or specify its path with --ffmpeg-path")
	}
	return path, nil
}

// BuildFFmpegArgs constructs command arguments for stream downloading and remuxing.
func BuildFFmpegArgs(resourceURL string, outputFile string, config *Config) []string {
	var args []string

	args = append(args, "-y") // Overwrite output file

	// Custom Headers
	var headerLines []string
	if config != nil && len(config.Headers) > 0 {
		for k, v := range config.Headers {
			headerLines = append(headerLines, fmt.Sprintf("%s: %s", k, v))
		}
	} else {
		headerLines = append(headerLines, fmt.Sprintf("User-Agent: oget/%s", Version))
	}
	if len(headerLines) > 0 {
		args = append(args, "-headers", strings.Join(headerLines, "\r\n")+"\r\n")
	}

	// HTTP Proxy
	if config != nil && config.ProxyURL != "" {
		args = append(args, "-http_proxy", config.ProxyURL)
	}

	// Network reconnection resilience
	args = append(args,
		"-reconnect", "1",
		"-reconnect_at_eof", "1",
		"-reconnect_streamed", "1",
		"-reconnect_delay_max", "5",
	)

	// Input URL
	args = append(args, "-i", resourceURL)

	// Stream copy & remux options
	args = append(args,
		"-c", "copy",
		"-bsf:a", "aac_adtstoasc",
		"-movflags", "+faststart",
		"-progress", "pipe:1",
		"-nostats",
	)

	// Output target file
	args = append(args, outputFile)

	return args
}
