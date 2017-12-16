package main

import "log"

type Command struct {
	URL      []string
	FileName string
}

func (cmd *Command) execute() {
	for _, url := range cmd.URL {
		fileName := parseFileName(url)
		log.Println("Downloading file", url, fileName)

		err := Download(url, fileName)
		if err != nil {
			log.Println(err)
		}
	}
}
