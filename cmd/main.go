package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/qtopie/oget/pkg/oget"
)

func main() {
	var fileName string
	var concurrency int
	var timeout int
	var verbose bool
	var version bool

	flag.StringVar(&fileName, "file", "", "name or path to save file (only for single URL)")
	flag.IntVar(&concurrency, "concurrency", 0, "number of concurrent workers (default 8 with autotune, 32 without)")
	flag.IntVar(&timeout, "timeout", 0, "timeout for network operations in seconds (default 30)")
	flag.BoolVar(&verbose, "verbose", false, "enable verbose output for dynamic detection")
	flag.BoolVar(&version, "version", false, "show version information")
	flag.Parse()

	if version {
		fmt.Printf("oget version %s (commit: %s)\n", oget.Version, oget.Commit)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL1> [URL2] ...\n", os.Args[0])
		flag.PrintDefaults()
		return
	}

	downloader := oget.NewDownloader(args, concurrency)
	if timeout > 0 {
		downloader.Config.Timeout = timeout
		// Re-create fetcher with new timeout if it was already created
		downloader.Fetcher = oget.NewHttpFetcher(downloader.Config)
	}
	downloader.Config.Verbose = verbose
	downloader.Download(context.Background())
}
