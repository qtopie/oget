package oget

import (
	"context"
	"net/http"
	"os"
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

func TestRequester_PrepareTasks(t *testing.T) {
	server := ogettest.NewSimpleServer()
	defer server.Close()

	fileName := "test_prepare_tasks"
	file, _ := os.Create(fileName)
	defer os.Remove(fileName)
	defer os.Remove(fileName + ".oget.state.json")
	defer file.Close()

	r := NewRequester(server.URL, DefaultConfig())
	r.Fetcher = &HttpFetcher{Client: &http.Client{}}
	
	var tasks []*ChunkTask
	r.SubmitTask = func(task *ChunkTask) {
		tasks = append(tasks, task)
	}

	err := r.PrepareTasks(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(tasks) == 0 {
		t.Errorf("expected tasks, got 0")
	}
}
