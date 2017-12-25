package main

import (
	"testing"
)

func TestParseFileName(t *testing.T) {
	var tests = []struct {
		url      string
		filename string
	}{
		{"https://www.example.com/", "index.html"},
		{"https://www.example.com/file1", "file1"},
	}

	for _, tt := range tests {
		s := parseFileName(tt.url)
		if s != tt.filename {
			t.Errorf("parseFileName(%q) => %q, want %q", tt.url, s, tt.filename)
		}
	}
}
