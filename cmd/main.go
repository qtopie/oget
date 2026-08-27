package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/qtopie/oget/pkg/oget"
)

type headerList []string

func (h *headerList) String() string {
	return strings.Join(*h, ", ")
}

func (h *headerList) Set(val string) error {
	*h = append(*h, val)
	return nil
}

func main() {
	var fileName string
	var concurrency int
	var timeout int
	var verbose bool
	var version bool
	var checksum bool
	var dnsServer string
	var ffmpegPath string
	var proxyURL string
	var headers headerList

	// Handle subcommands like 'oget bt clean'
	if len(os.Args) >= 2 && os.Args[1] == "bt" {
		if len(os.Args) >= 3 && os.Args[2] == "clean" {
			targetDir := "."
			if len(os.Args) >= 4 {
				targetDir = os.Args[3]
			}
			_ = oget.CleanBT(targetDir)
			return
		}
		fmt.Fprintf(os.Stderr, "Usage: %s bt clean [optional_directory]\n", os.Args[0])
		return
	}

	flag.StringVar(&fileName, "file", "", "name or path to save file (only for single URL)")
	flag.IntVar(&concurrency, "concurrency", 0, "number of concurrent workers (default 8 with autotune, 32 without)")
	flag.IntVar(&timeout, "timeout", 0, "timeout for network operations in seconds (default 30)")
	flag.BoolVar(&verbose, "verbose", false, "enable verbose output for dynamic detection")
	flag.BoolVar(&version, "version", false, "show version information")
	flag.BoolVar(&checksum, "checksum", false, "enable per-chunk SHA-256 checksum verification")
	flag.StringVar(&dnsServer, "dns", "", "custom DNS server for BT tracker/peer resolution (e.g. 8.8.8.8 or 8.8.8.8:53)")
	flag.StringVar(&ffmpegPath, "ffmpeg-path", "", "custom path to ffmpeg executable")
	flag.StringVar(&proxyURL, "proxy", "", "proxy URL (e.g. http://127.0.0.1:7890)")
	flag.Var(&headers, "H", "custom HTTP header (e.g. -H 'Referer: https://example.com') (can be specified multiple times)")
	flag.Var(&headers, "header", "custom HTTP header (e.g. --header 'Referer: https://example.com')")
	flag.Parse()

	if version {
		fmt.Printf("oget version %s (commit: %s)\n", oget.Version, oget.Commit)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL1> [URL2] ...\n       %s bt clean [directory]\n", os.Args[0], os.Args[0])
		flag.PrintDefaults()
		return
	}

	if args[0] == "bt" {
		if len(args) >= 2 && args[1] == "clean" {
			targetDir := "."
			if len(args) >= 3 {
				targetDir = args[2]
			}
			_ = oget.CleanBT(targetDir)
			return
		}
		fmt.Fprintf(os.Stderr, "Usage: %s bt clean [optional_directory]\n", os.Args[0])
		return
	}

	downloader := oget.NewDownloader(args, concurrency)
	downloader.Config.Verbose = verbose
	downloader.Config.Checksum = checksum
	downloader.Config.DNS = dnsServer
	if ffmpegPath != "" {
		downloader.Config.FFmpegPath = ffmpegPath
	}
	if proxyURL != "" {
		downloader.Config.ProxyURL = proxyURL
	}
	if len(headers) > 0 {
		for _, h := range headers {
			parts := strings.SplitN(h, ":", 2)
			if len(parts) == 2 {
				downloader.Config.Headers[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}
	if fileName != "" {
		downloader.Config.OutputDir = fileName
	}
	if timeout > 0 {
		downloader.Config.Timeout = timeout
	}
	downloader.Fetcher = oget.NewDispatchFetcher(downloader.Config)

	downloader.Download(context.Background())
	oget.CleanupProtocols(downloader.Config)
}
