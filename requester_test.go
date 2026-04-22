package oget

import (
	"context"
	"testing"

	"github.com/qtopie/oget/ogettest"
)

func TestRequester_Probe(t *testing.T) {
	server := ogettest.NewSimpleServer()
	defer server.Close()

	r := NewRequester(server.URL, DefaultConfig())
	length, etag, lastModified, err := r.probe(context.Background())
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if length != int64(len(ogettest.DefaultWebContent)) {
		t.Errorf("got length %d, want %d", length, len(ogettest.DefaultWebContent))
	}
	_ = etag
	_ = lastModified
}
