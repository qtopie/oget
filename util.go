package main

import "strings"

func parseFileName(url string) string {
	tokens := strings.Split(url, "/")
	fileName := tokens[len(tokens)-1]

	return fileName
}
