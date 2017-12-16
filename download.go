package main

import (
	"io"
	"net/http"
	"os"
)

func Download(url, filePath string) (err error) {
	file, err := os.Create(filePath)
	if err != nil {
		return
	}
	defer file.Close()

	resp, err := http.Get(url)
	if err != nil {
		return
	}

	_, err = io.Copy(file, resp.Body)

	return
}
