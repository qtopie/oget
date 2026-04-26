package oget

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
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

func TestRequester_ProbeFallbackGet(t *testing.T) {
	// Start a server that returns 405 Method Not Allowed for HEAD
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// GET works
		w.Header().Set("Content-Length", "12")
		io.WriteString(w, "Hello World!")
	}))
	defer server.Close()

	config := DefaultConfig()
	r := NewRequester(server.URL, config)
	
	meta, err := r.Prober.Probe(context.Background(), r.Resource)
	if err != nil {
		t.Fatalf("expected fallback to GET to succeed, got error: %v", err)
	}
	
	if meta.Size != 12 {
		t.Errorf("got size %d, want 12", meta.Size)
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
