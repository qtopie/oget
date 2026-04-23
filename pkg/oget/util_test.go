package oget

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
		{"https://www.example.com/path/file2.txt", "file2.txt"},
		{"https://www.example.com/file3?arg=val", "file3"},
		{"https://www.example.com/file4#frag", "file4"},
		{"https://www.example.com/path/", "index.html"},
	}

	for _, tt := range tests {
		s := parseFileName(tt.url)
		if s != tt.filename {
			t.Errorf("parseFileName(%q) => %q, want %q", tt.url, s, tt.filename)
		}
	}
}

func TestValidateURL(t *testing.T) {
	tests := []struct {
		url   string
		valid bool
	}{
		{"http://google.com", true},
		{"https://google.com", true},
		{"google.com", false},
		{"ftp://google.com", true},
		{"invalid-url", false},
	}

	for _, tt := range tests {
		got := validateURL(tt.url)
		if got != tt.valid {
			t.Errorf("validateURL(%q) => %v, want %v", tt.url, got, tt.valid)
		}
	}
}
