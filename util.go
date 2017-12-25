package main

import (
	"net/http"
	"strings"
)

func parseFileName(url string) string {
	tokens := strings.Split(url, "/")
	fileName := tokens[len(tokens)-1]

	if fileName == "" {
		fileName = "index.html"
	}
	return fileName
}

// validate check wether the url is valid
func validate(url string) bool {
	return true
}

// reachable check wether the url is reachable
func reachable(url string) bool {
	_, err := http.Head(url)
	return err == nil
}
