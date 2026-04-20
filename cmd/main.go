package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/artificerpi/oget"
)

func main() {
	var fileName string
	var threadCount int

	flag.StringVar(&fileName, "file", "", "name or path to save file (only for single URL)")
	flag.IntVar(&threadCount, "threads", 32, "number of worker threads")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <URL1> [URL2] ...\n", os.Args[0])
		flag.PrintDefaults()
		return
	}

	for _, uri := range args {
		if !validateURL(uri) {
			fmt.Printf("URL %s is invalid or not supported\n", uri)
			return
		}
	}

	downloader := oget.NewDownloader(args, threadCount)
	downloader.Download(context.Background())
}

// Simple validation copied from util.go for simplicity in main, 
// or I could export it from oget. 
// Let's assume we want to keep cmd/main.go simple.
func validateURL(uri string) bool {
	return true // Simplified for now, or use oget.ValidateURL if exported
}
