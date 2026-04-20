package oget

import (
	"context"
	"log"
	"sync"
)

// Global task queue used by the library.
var TaskQueue chan *ChunkTask

// Downloader handles the execution of download tasks for multiple URLs.
type Downloader struct {
	URLs        []string
	ThreadCount int
}

// NewDownloader creates a new Downloader instance.
func NewDownloader(urls []string, threadCount int) *Downloader {
	return &Downloader{
		URLs:        urls,
		ThreadCount: threadCount,
	}
}

// Download starts the download process with context for cancellation.
func (d *Downloader) Download(ctx context.Context) {
	// Initialize the global task queue.
	TaskQueue = make(chan *ChunkTask, 1000)

	var wg sync.WaitGroup

	// 1. Start Worker Pool
	for i := 0; i < d.ThreadCount; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-TaskQueue:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					err := task.FetcherHandler.Fetch(ctx, task)
					if err != nil {
						log.Printf("Worker %d: Task failed: %s chunk %d: %v", workerID, task.FileID, task.ChunkID, err)
					}
				}
			}
		}(i)
	}

	// 2. Start Multiple Requesters (one for each URL)
	var producerWg sync.WaitGroup
	for _, url := range d.URLs {
		producerWg.Add(1)
		go func(u string) {
			defer producerWg.Done()
			requester := NewRequester(u)
			err := requester.PrepareTasks(ctx)
			if err != nil {
				if err != context.Canceled {
					log.Printf("Error preparing tasks for %s: %v", u, err)
				}
			}
		}(url)
	}

	// 3. Closer: Close TaskQueue when all producers are done
	go func() {
		producerWg.Wait()
		close(TaskQueue)
	}()

	// 4. Wait for all workers to finish
	wg.Wait()
	if ctx.Err() != nil {
		log.Printf("Download cancelled: %v", ctx.Err())
	} else {
		log.Println("All downloads completed.")
	}
}
