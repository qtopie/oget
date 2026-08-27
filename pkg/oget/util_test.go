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

func TestParseFileName_TorrentAndMagnet(t *testing.T) {
	tests := []struct {
		url      string
		filename string
	}{
		{"https://example.com/test.torrent", "test"},
		{"/path/to/sample.torrent", "sample"},
		{"magnet:?xt=urn:btih:c12fe1c06bba254a9d1b00f310b292ae4c9d4354&dn=Ubuntu", "Ubuntu"},
		{"magnet:?xt=urn:btih:c12fe1c06bba254a9d1b00f310b292ae4c9d4354", "c12fe1c06bba254a9d1b00f310b292ae4c9d4354"},
	}

	for _, tt := range tests {
		s := parseFileName(tt.url)
		if s != tt.filename {
			t.Errorf("parseFileName(%q) => %q, want %q", tt.url, s, tt.filename)
		}
	}
}

func TestTorrentAndMagnetIDs(t *testing.T) {
	data := []byte("torrent content dummy")
	id1 := torrentIDFromData(data)
	id2 := torrentIDFromData(data)
	if id1 == "" || id1 != id2 {
		t.Errorf("torrentIDFromData is not deterministic: %s != %s", id1, id2)
	}

	mag1 := magnetIDFromURI("magnet:?xt=urn:btih:C12FE1C06BBA254A9D1B00F310B292AE4C9D4354&dn=Ubuntu")
	mag2 := magnetIDFromURI("magnet:?xt=urn:btih:c12fe1c06bba254a9d1b00f310b292ae4c9d4354")
	if mag1 != "c12fe1c06bba254a9d1b00f310b292ae4c9d4354" || mag1 != mag2 {
		t.Errorf("magnetIDFromURI failed: got %s and %s", mag1, mag2)
	}
}
