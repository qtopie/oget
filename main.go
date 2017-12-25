package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

var (
	cmd Command
)

func main() {
	flag.StringVar(&cmd.FileName, "file", "", "name or path to save file")
	flag.Parse()
	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintf(os.Stderr, "%s: invalid URL %s to download from\n", os.Args[0], args)
		fmt.Fprintf(os.Stdout, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
		return
	}

	cmd.URL = args[0]
	log.Println("cmd.URL", cmd.URL)

	cmd.execute()
}
