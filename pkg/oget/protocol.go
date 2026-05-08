package oget

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cenkalti/rain/torrent"
	"github.com/jlaffaye/ftp"
)

var (
	rainSession      *torrent.Session
	rainSessionOnce  sync.Once
	cachedTrackers   []string
	trackersOnce     sync.Once
)

func getTrackers(ctx context.Context, config *Config) []string {
	trackersOnce.Do(func() {
		if config != nil && len(config.TrackerURLs) > 0 {
			var allTrackers []string
			uniqueTrackers := make(map[string]bool)

			for _, trackerURL := range config.TrackerURLs {
				if config.Verbose {
					log.Printf("[Magnet] Fetching external trackers from: %s", trackerURL)
				}
				trackers := fetchTrackers(ctx, trackerURL)
				for _, t := range trackers {
					if !uniqueTrackers[t] {
						uniqueTrackers[t] = true
						allTrackers = append(allTrackers, t)
					}
				}
			}
			cachedTrackers = allTrackers
			if config.Verbose {
				log.Printf("[Magnet] Fetched %d unique external trackers from %d sources", len(cachedTrackers), len(config.TrackerURLs))
			}
		}
	})
	return cachedTrackers
}

func fetchTrackers(ctx context.Context, trackerURL string) []string {
	if trackerURL == "" {
		return nil
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", trackerURL, nil)
	if err != nil {
		return nil
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var trackers []string
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			trackers = append(trackers, line)
		}
	}
	return trackers
}

func getRainSession(config *Config) (*torrent.Session, error) {
	var err error
	rainSessionOnce.Do(func() {
		cfg := torrent.DefaultConfig
		cfg.DataDir = filepath.Join(os.TempDir(), "oget_rain")
		cfg.Database = filepath.Join(os.TempDir(), "oget_rain.db")
		cfg.TrackerHTTPVerifyTLS = false // Bypass TLS verification for trackers
		rainSession, err = torrent.NewSession(cfg)
	})
	return rainSession, err
}

// CleanupProtocols handles resource cleanup for all protocols.
func CleanupProtocols(config *Config) {
	if rainSession != nil {
		duration := 30
		if config != nil {
			duration = config.SeedingDuration
		}
		
		if duration > 0 {
			fmt.Printf("\n[Magnet] All magnet tasks finished. Seeding for %ds (Privacy Grace Period)...\n", duration)
			time.Sleep(time.Duration(duration) * time.Second)
		}
		rainSession.Close()
		fmt.Println("[Magnet] Seeding stopped. Privacy secured.")
	}
}

// DispatchFetcher dispatches the fetch request to the appropriate fetcher based on the URL scheme.
type DispatchFetcher struct {
	Config   *Config
	fetchers sync.Map // map[string]Fetcher
}

func NewDispatchFetcher(config *Config) *DispatchFetcher {
	return &DispatchFetcher{Config: config}
}

func (f *DispatchFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	u, err := url.Parse(task.URL)
	scheme := "http"
	if err == nil {
		scheme = strings.ToLower(u.Scheme)
	}

	if fetcher, ok := f.fetchers.Load(scheme); ok {
		return fetcher.(Fetcher).Fetch(ctx, task)
	}

	fetcher := GetFetcher(task.URL, f.Config)
	actual, loaded := f.fetchers.LoadOrStore(scheme, fetcher)
	if loaded {
		return actual.(Fetcher).Fetch(ctx, task)
	}
	return fetcher.Fetch(ctx, task)
}

// GetProber returns the appropriate Prober for the given resource.
func GetProber(resource string, config *Config) Prober {
	u, err := url.Parse(resource)
	if err != nil {
		return NewHttpProber(config)
	}

	switch strings.ToLower(u.Scheme) {
	case "ftp":
		return NewFtpProber(config)
	case "magnet":
		return NewMagnetProber(config)
	default:
		return NewHttpProber(config)
	}
}

// GetFetcher returns the appropriate Fetcher for the given resource.
func GetFetcher(resource string, config *Config) Fetcher {
	u, err := url.Parse(resource)
	if err != nil {
		return NewHttpFetcher(config)
	}

	switch strings.ToLower(u.Scheme) {
	case "ftp":
		return NewFtpFetcher(config)
	case "magnet":
		return NewMagnetFetcher(config)
	default:
		return NewHttpFetcher(config)
	}
}

// FtpProber implements Prober for FTP protocol.
type FtpProber struct {
	Config *Config
}

func NewFtpProber(config *Config) *FtpProber {
	return &FtpProber{Config: config}
}

func (p *FtpProber) Probe(ctx context.Context, resource string) (*ResourceMetadata, error) {
	c, err := dialFtp(resource, p.Config.Timeout)
	if err != nil {
		return nil, err
	}
	defer c.Quit()

	u, _ := url.Parse(resource)
	size, err := c.FileSize(u.Path)
	if err != nil {
		// Fallback: try to list and find the file
		dir := filepath.Dir(u.Path)
		base := filepath.Base(u.Path)
		entries, err := c.List(dir)
		if err == nil {
			for _, entry := range entries {
				if entry.Name == base {
					return &ResourceMetadata{
						Size: int64(entry.Size),
					}, nil
				}
			}
		}
		return nil, fmt.Errorf("failed to get ftp file size: %w", err)
	}

	return &ResourceMetadata{
		Size: size,
	}, nil
}

// FtpFetcher implements Fetcher for FTP protocol.
type FtpFetcher struct {
	Config *Config
}

func NewFtpFetcher(config *Config) *FtpFetcher {
	return &FtpFetcher{Config: config}
}

func (f *FtpFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	c, err := dialFtp(task.URL, f.Config.Timeout)
	if err != nil {
		return err
	}
	defer c.Quit()

	u, _ := url.Parse(task.URL)
	resp, err := c.RetrFrom(u.Path, uint64(task.Offset))
	if err != nil {
		return fmt.Errorf("ftp RETR failed: %w", err)
	}
	defer resp.Close()

	var written int64
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		remaining := task.Length - written
		if task.Length == -1 {
			remaining = 32 * 1024
		}
		if remaining <= 0 && task.Length != -1 {
			break
		}

		lr := io.LimitReader(resp, remaining)
		n, err := task.StorageHandler.ReadAtFrom(lr, task.Offset+written, remaining)
		if n > 0 {
			written += n
			if task.OnProgress != nil {
				task.OnProgress(int(n))
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if n == 0 {
			break
		}
	}

	if task.Length != -1 && written < task.Length {
		return fmt.Errorf("ftp download incomplete: got %d bytes, want %d", written, task.Length)
	}

	if task.OnChunkComplete != nil {
		task.OnChunkComplete(task.ChunkID, "")
	}

	return nil
}

func dialFtp(resource string, timeout int) (*ftp.ServerConn, error) {
	u, err := url.Parse(resource)
	if err != nil {
		return nil, err
	}

	host := u.Host
	if !strings.Contains(host, ":") {
		host += ":21"
	}

	c, err := ftp.Dial(host, ftp.DialWithTimeout(time.Duration(timeout)*time.Second))
	if err != nil {
		return nil, err
	}

	user := "anonymous"
	pass := "anonymous"
	if u.User != nil {
		user = u.User.Username()
		p, ok := u.User.Password()
		if ok {
			pass = p
		}
	}

	err = c.Login(user, pass)
	if err != nil {
		c.Quit()
		return nil, err
	}

	return c, nil
}

func addTrackersInBatches(ctx context.Context, t *torrent.Torrent, trackers []string) {
	go func() {
		batchSize := 30
		for i := 0; i < len(trackers); i += batchSize {
			end := i + batchSize
			if end > len(trackers) {
				end = len(trackers)
			}
			for _, tr := range trackers[i:end] {
				_ = t.AddTracker(tr)
			}
			
			select {
			case <-ctx.Done():
				return
			case <-t.NotifyMetadata():
				return // Stop adding trackers once metadata is found
			case <-time.After(2 * time.Second): // Delay next batch to prevent file descriptor exhaustion
			}
		}
	}()
}

// MagnetProber implements Prober for Magnet/BitTorrent protocol.
type MagnetProber struct {
	Config *Config
}

func NewMagnetProber(config *Config) *MagnetProber {
	return &MagnetProber{Config: config}
}

func (p *MagnetProber) Probe(ctx context.Context, resource string) (*ResourceMetadata, error) {
	session, err := getRainSession(p.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create rain session: %w", err)
	}

	t, err := session.AddURI(resource, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}

	// Add external trackers in batches (limit to 50 max to prevent FD exhaustion)
	trackers := getTrackers(ctx, p.Config)
	if len(trackers) > 0 {
		maxTrackers := 50
		if len(trackers) > maxTrackers {
			trackers = trackers[:maxTrackers]
		}
		addTrackersInBatches(ctx, t, trackers)
	}

	// Wait for metadata
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.NotifyMetadata():
	case <-time.After(time.Duration(p.Config.MagnetProbeTimeout) * time.Second):
		return nil, fmt.Errorf("magnet probe timed out after %ds", p.Config.MagnetProbeTimeout)
	}

	files, err := t.Files()
	if err != nil {
		return nil, err
	}

	var totalSize int64
	for _, f := range files {
		totalSize += f.Length()
	}

	return &ResourceMetadata{
		Size: totalSize,
	}, nil
}

// MagnetFetcher implements Fetcher for Magnet/BitTorrent protocol.
type MagnetFetcher struct {
	Config *Config
}

func NewMagnetFetcher(config *Config) *MagnetFetcher {
	return &MagnetFetcher{Config: config}
}

func (f *MagnetFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	session, err := getRainSession(f.Config)
	if err != nil {
		return err
	}

	t, err := session.AddURI(task.URL, nil)
	if err != nil {
		return err
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.NotifyMetadata():
	}

	t.Start()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stats := t.Stats()
			if stats.Bytes.Completed >= task.Length {
				return nil 
			}
		}
	}
}
