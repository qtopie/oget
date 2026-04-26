package oget

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/qtopie/oget/ogettest"
)

func TestRequester_Probe(t *testing.T) {
	server := ogettest.NewSimpleServer()
	defer server.Close()

	r := NewRequester(server.URL, DefaultConfig())
	meta, err := r.Prober.Probe(context.Background(), r.Resource)
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}

	if meta.Size != int64(len(ogettest.DefaultWebContent)) {
		t.Errorf("got length %d, want %d", meta.Size, len(ogettest.DefaultWebContent))
	}
	_ = meta.ETag
	_ = meta.LastModified
}

func TestRequester_ProbeTimeout(t *testing.T) {
	// Start a server that sleeps longer than the timeout
	server := ogettest.NewSlowServer(2 * time.Second)
	defer server.Close()

	config := DefaultConfig()
	config.Timeout = 1 // 1 second timeout
	r := NewRequester(server.URL, config)
	
	_, err := r.Prober.Probe(context.Background(), r.Resource)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
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
