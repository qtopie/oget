package oget

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"
)

// Downloader handles the execution of download tasks for multiple URLs.
type Downloader struct {
	URLs           []string
	Concurrency    int
	Config         *Config
	TotalProcessed int64 // Atomic counter for progress
	TotalSize      int64 // Total size of all files

	// Dynamic Concurrency control
	activeWorkers     int32
	targetConcurrency int32

	// Work Stealing & Connection Reuse support
	Fetcher    Fetcher
	hostQueues sync.Map // map[string]chan *ChunkTask
	hostKeys   []string
	mu         sync.RWMutex
}

// NewDownloader creates a new Downloader instance with dynamic control.
func NewDownloader(urls []string, concurrency int) *Downloader {
	cfg := DefaultConfig()
	if concurrency > 0 {
		cfg.Concurrency = concurrency
	}
	return &Downloader{
		URLs:              urls,
		Concurrency:       cfg.Concurrency,
		Config:            cfg,
		Fetcher:           NewDispatchFetcher(cfg),
		targetConcurrency: int32(cfg.Concurrency),
	}
}

// getHostQueue returns the queue for a specific host, creating it if needed.
func (d *Downloader) getHostQueue(host string) chan *ChunkTask {
	if q, ok := d.hostQueues.Load(host); ok {
		return q.(chan *ChunkTask)
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if q, ok := d.hostQueues.Load(host); ok {
		return q.(chan *ChunkTask)
	}

	q := make(chan *ChunkTask, 100)
	d.hostQueues.Store(host, q)
	d.hostKeys = append(d.hostKeys, host)
	return q
}

// addTask adds tasks to their host-specific queue.
func (d *Downloader) addTask(tasks ...*ChunkTask) {
	if len(tasks) == 0 {
		return
	}
	u, err := url.Parse(tasks[0].URL)
	host := "default"
	if err == nil {
		host = u.Host
	}
	q := d.getHostQueue(host)
	for _, t := range tasks {
		q <- t
	}
}

// spawnWorker starts a new worker goroutine.
func (d *Downloader) spawnWorker(ctx context.Context, wg *sync.WaitGroup) {
	atomic.AddInt32(&d.activeWorkers, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer atomic.AddInt32(&d.activeWorkers, -1)

		var currentHost string
		for {
			if atomic.LoadInt32(&d.activeWorkers) > atomic.LoadInt32(&d.targetConcurrency) {
				return
			}

			var task *ChunkTask
			var ok bool

			if currentHost != "" {
				if q, exists := d.hostQueues.Load(currentHost); exists {
					select {
					case task, ok = <-q.(chan *ChunkTask):
					default:
					}
				}
			}

			if task == nil {
				d.mu.RLock()
				keys := d.hostKeys
				d.mu.RUnlock()

				for _, h := range keys {
					if q, exists := d.hostQueues.Load(h); exists {
						select {
						case task, ok = <-q.(chan *ChunkTask):
							currentHost = h
						default:
						}
					}
					if task != nil {
						break
					}
				}
			}

			if task == nil {
				select {
				case <-ctx.Done():
					return
				default:
					time.Sleep(50 * time.Millisecond)
					if atomic.LoadInt32(&d.activeWorkers) > atomic.LoadInt32(&d.targetConcurrency) {
						return
					}
					continue
				}
			}

			if !ok {
				continue
			}

			err := d.Fetcher.Fetch(ctx, task)
			if err != nil {
				log.Printf("Error fetching chunk %d for %s: %v", task.ChunkID, task.FileID, err)
				task.Retries++
				if task.Retries < MaxFetchRetries {
					// Re-enqueue the chunk for retry with the same parameters.
					// Do NOT call OnChunkComplete — that would mark partial/corrupt data
					// as complete in the bitset and cause the download to finish with a bad file.
					retryTask := NewChunkTask()
					*retryTask = *task // Copy all fields (Retries, StorageHandler, OnChunkComplete, etc.)
					d.addTask(retryTask)
				} else {
					log.Printf("Chunk %d for %s exceeded max retries (%d), file may be corrupt",
						task.ChunkID, task.FileID, MaxFetchRetries)
					// Only mark complete after exhausting retries — last resort to avoid hang.
					if task.OnChunkComplete != nil {
						task.OnChunkComplete(task.ChunkID, "error")
					}
				}
			}
			ReleaseChunkTask(task)
		}
	}()
}

// PrepareAllTasks probes all URLs and returns a flattened list of tasks and the requesters.
func (d *Downloader) PrepareAllTasks(ctx context.Context) ([]*ChunkTask, []*Requester, error) {
	var allTasks []*ChunkTask
	var requesters []*Requester
	for _, u := range d.URLs {
		req := NewRequester(u, d.Config)
		req.Fetcher = d.Fetcher

		var urlTasks []*ChunkTask
		req.SubmitTask = func(tasks ...*ChunkTask) {
			urlTasks = append(urlTasks, tasks...)
		}

		if err := req.PrepareTasks(ctx); err != nil {
			log.Printf("Warning: failed to prepare tasks for %s: %v", u, err)
			continue
		}
		
		for _, t := range urlTasks {
			if t.Length > 0 {
				atomic.AddInt64(&d.TotalSize, t.Length)
			}
		}
		allTasks = append(allTasks, urlTasks...)
		requesters = append(requesters, req)
	}
	return allTasks, requesters, nil
}

// Download starts the download process with Adaptive Concurrency Control.
func (d *Downloader) Download(ctx context.Context) {
	parentCtx := ctx
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var tasksWg sync.WaitGroup

	// 1. Pre-download Probing
	allTasks, requesters, err := d.PrepareAllTasks(ctx)
	if err != nil {
		log.Printf("Error: %v", err)
		return
	}

	// Enhanced Progress Bar
	bar := progressbar.NewOptions64(d.TotalSize,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionSetWriter(os.Stderr),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(),
		progressbar.OptionOnCompletion(func() {
			fmt.Fprintln(os.Stderr, "\nDownload finished.")
		}),
		progressbar.OptionSetWidth(15),
		progressbar.OptionShowElapsedTimeOnFinish(),
		progressbar.OptionSetTheme(progressbar.Theme{
			Saucer:        "=",
			SaucerHead:    ">",
			SaucerPadding: " ",
			BarStart:      "[",
			BarEnd:        "]",
		}),
		progressbar.OptionSetPredictTime(true),
	)

	// Start initial workers
	for i := 0; i < d.Concurrency; i++ {
		d.spawnWorker(ctx, &wg)
	}

	// 2. Submit already prepared tasks
	for _, t := range allTasks {
		// Create a local variable for the progress bar to be used in the closure
		onProgress := func(n int) {
			atomic.AddInt64(&d.TotalProcessed, int64(n))
			_ = bar.Add(n)
		}
		
		t.OnProgress = onProgress
		
		// Track task completion
		tasksWg.Add(1)
		originalOnComplete := t.OnChunkComplete
		t.OnChunkComplete = func(chunkID int, hash string) {
			if originalOnComplete != nil {
				originalOnComplete(chunkID, hash)
			}
			tasksWg.Done()
		}
	}
	d.addTask(allTasks...)


	// 3. Bandwidth Auto-Tuner
	if d.Config.AutoTune {
		go func() {
			var lastProcessed int64
			var lastSpeed int64
			var cooldown int
			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if cooldown > 0 {
						cooldown--
						lastProcessed = atomic.LoadInt64(&d.TotalProcessed)
						continue
					}

					processed := atomic.LoadInt64(&d.TotalProcessed)
					speed := (processed - lastProcessed) / 2
					lastProcessed = processed

					if lastSpeed == 0 {
						lastSpeed = speed
						continue
					}

					target := atomic.LoadInt32(&d.targetConcurrency)
					diff := float64(speed-lastSpeed) / float64(lastSpeed)
					
					if diff > 0.10 && target < int32(d.Config.MaxConcurrency) {
						atomic.AddInt32(&d.targetConcurrency, 1)
						d.spawnWorker(ctx, &wg)
						cooldown = 3
						if d.Config.Verbose {
							log.Printf("\n[AutoTune] Speed improved (%s/s), concurrency: %d", humanizeSize(speed), target+1)
						}
					} else if diff < -0.20 && target > 1 {
						atomic.AddInt32(&d.targetConcurrency, -1)
						cooldown = 2
						if d.Config.Verbose {
							log.Printf("\n[AutoTune] Speed dropped (%s/s), backing off to: %d", humanizeSize(speed), target-1)
						}
					}
					lastSpeed = speed
				}
			}
		}()
	}

	// Wait for all tasks to complete in a separate goroutine
	go func() {
		tasksWg.Wait()
		cancel()
	}()

	wg.Wait()
	_ = bar.Finish()

	// Cleanup state files if download completed successfully
	if parentCtx.Err() == nil {
		for _, r := range requesters {
			r.Cleanup()
		}
	}
}
