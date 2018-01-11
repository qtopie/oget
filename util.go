package main

import (
	"strings"
	"net/url"
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
func validateURL(uri string) bool {
	_, err := url.ParseRequestURI(uri)
	if err != nil {
		return false
	}
	return true
}
