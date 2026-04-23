package oget

import (
	"fmt"
	"net/url"
	"strings"
)

func parseFileName(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		tokens := strings.Split(uri, "/")
		return tokens[len(tokens)-1]
	}

	path := u.Path
	if path == "" || path == "/" {
		return "index.html"
	}

	tokens := strings.Split(path, "/")
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

// humanizeSize converts bytes to a human-readable string (KB, MB, GB, etc.)
func humanizeSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
