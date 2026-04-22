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
	var verbose bool

	flag.StringVar(&fileName, "file", "", "name or path to save file (only for single URL)")
	flag.IntVar(&concurrency, "concurrency", 0, "number of concurrent workers (default 8 with autotune, 32 without)")
	flag.BoolVar(&verbose, "v", false, "enable verbose output for dynamic detection")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL1> [URL2] ...\n", os.Args[0])
		flag.PrintDefaults()
		return
	}

	downloader := oget.NewDownloader(args, concurrency)
	downloader.Config.Verbose = verbose
	downloader.Download(context.Background())
}
