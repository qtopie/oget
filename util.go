package main

import (
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
