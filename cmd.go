package main

import (
	"log"
	"os"
)

type Command struct {
	URL      string
	FileName string
}

func (cmd *Command) execute() {
	if !validate(cmd.URL) {
		log.Fatal(cmd.URL, " is invalid or not supported")
		return
	}

	fileName := parseFileName(cmd.URL)
	log.Println("Downloading file", cmd.URL, fileName)

	file, err := os.Create(fileName)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	fetcher := &Fetcher{
		URL:         cmd.URL,
		FileName:    fileName,
		FileHandler: file,
	}

	fetcher.Download()

	log.Println("Downloaded file", cmd.URL, "to", fileName)

}
