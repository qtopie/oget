package main

import (
	"log"
	"os"
)

type Command struct {
	URL       []string
	FileName  string
	BlockSize string
}

func (cmd *Command) execute() {
	for _, url := range cmd.URL {
		if !validate(url) {
			log.Fatal(url, " is invalid or not supported")
			continue
		}
		if !reachable(url) {
			log.Fatal(url, " is not reachable")
			continue
		}

		fileName := parseFileName(url)
		log.Println("Downloading file", url, fileName)

		file, err := os.Create(fileName)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()

		fetcher := &Fetcher{
			URL:         url,
			FileName:    fileName,
			FileHandler: file,
		}

		fetcher.Download()

		log.Println("Downloaded file", url, "to", fileName)

	}
}
