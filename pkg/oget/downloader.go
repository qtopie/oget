package oget

import (
	"context"
	"log"
	"net/url"
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
		Fetcher:           NewHttpFetcher(cfg),
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

// addTask adds a task to its host-specific queue.
func (d *Downloader) addTask(task *ChunkTask) {
	u, err := url.Parse(task.URL)
	host := "default"
	if err == nil {
		host = u.Host
	}
	q := d.getHostQueue(host)
	q <- task
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

			_ = d.Fetcher.Fetch(ctx, task)
		}
	}()
}

// Download starts the download process with Adaptive Concurrency Control.
func (d *Downloader) Download(ctx context.Context) {
	var wg sync.WaitGroup

	// Enhanced Progress Bar without redundant it/s
	bar := progressbar.NewOptions64(-1,
		progressbar.OptionSetDescription("Downloading"),
		progressbar.OptionSetWriter(log.Writer()),
		progressbar.OptionShowBytes(true),
		progressbar.OptionShowCount(), // Fixed: no arguments
		progressbar.OptionOnCompletion(func() {
			log.Println("\nDownload finished.")
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
		progressbar.OptionSetPredictTime(true), // Fixed: correct method name
	)

	// Start initial workers
	for i := 0; i < d.Concurrency; i++ {
		d.spawnWorker(ctx, &wg)
	}

	// 2. Start Requesters (Producers)
	var producerWg sync.WaitGroup
	for _, u := range d.URLs {
		producerWg.Add(1)
		go func(urlStr string) {
			defer producerWg.Done()
			requester := NewRequester(urlStr, d.Config)
			requester.Fetcher = d.Fetcher
			requester.OnProgress = func(n int) {
				atomic.AddInt64(&d.TotalProcessed, int64(n))
				_ = bar.Add(n)
			}
			requester.SubmitTask = d.addTask
			_ = requester.PrepareTasks(ctx)
		}(u)
	}

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

	producerWg.Wait()
	wg.Wait()
	_ = bar.Finish()
}
