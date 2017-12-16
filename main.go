package main

import (
	"flag"
	"log"
)

var (
	cmd Command
)

func main() {
	flag.StringVar(&cmd.FileName, "file", "", "filename to download")
	flag.Parse()
	cmd.URL = flag.Args()
	log.Println("cmd.URL", cmd.URL)

	cmd.execute()
}
