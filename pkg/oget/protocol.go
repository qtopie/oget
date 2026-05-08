package oget

import (
	"bufio"
	"bytes"
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

func fetchTorrentContent(ctx context.Context, resource string, timeout int) ([]byte, error) {
	// Check if it's a local file first
	if _, err := os.Stat(resource); err == nil {
		return os.ReadFile(resource)
	}

	// Otherwise treat as URL
	u, err := url.Parse(resource)
	if err != nil {
		return nil, err
	}

	if u.Scheme == "" {
		return nil, fmt.Errorf("invalid resource: %s (file not found or invalid URL)", resource)
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", resource, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch torrent: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Optimization: Save the remote .torrent file to current directory for future use
	torrentFileName := filepath.Base(u.Path)
	if torrentFileName == "" || torrentFileName == "." || torrentFileName == "/" {
		torrentFileName = "download.torrent"
	}
	if !strings.HasSuffix(strings.ToLower(torrentFileName), ".torrent") {
		torrentFileName += ".torrent"
	}

	if _, err := os.Stat(torrentFileName); os.IsNotExist(err) {
		_ = os.WriteFile(torrentFileName, data, 0644)
		log.Printf("[BitTorrent] Saved remote torrent file to: %s", torrentFileName)
	}

	return data, nil
}

// TorrentProber implements Prober for .torrent files.
type TorrentProber struct {
	Config *Config
}

func NewTorrentProber(config *Config) *TorrentProber {
	return &TorrentProber{Config: config}
}

func (p *TorrentProber) Probe(ctx context.Context, resource string) (*ResourceMetadata, error) {
	data, err := fetchTorrentContent(ctx, resource, p.Config.Timeout)
	if err != nil {
		return nil, err
	}

	session, err := getRainSession(p.Config)
	if err != nil {
		return nil, err
	}

	var t *torrent.Torrent
	t, err = session.AddTorrent(bytes.NewReader(data), nil)
	if err != nil {
		if strings.Contains(err.Error(), "already added") {
			for _, existing := range session.ListTorrents() {
				t = existing
				break
			}
		} else {
			return nil, fmt.Errorf("failed to add torrent: %w", err)
		}
	}

	if t == nil {
		return nil, fmt.Errorf("failed to add or find torrent")
	}

	// Add external trackers to boost discovery
	extTrackers := getTrackers(ctx, p.Config)
	if len(extTrackers) > 0 {
		maxTrackers := 200
		if len(extTrackers) > maxTrackers {
			extTrackers = extTrackers[:maxTrackers]
		}
		addTrackersInBatches(ctx, t, extTrackers, p.Config.Verbose)
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

// TorrentFetcher implements Fetcher for .torrent files.
type TorrentFetcher struct {
	Config *Config
}

func NewTorrentFetcher(config *Config) *TorrentFetcher {
	return &TorrentFetcher{Config: config}
}

func (f *TorrentFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	session, err := getRainSession(f.Config)
	if err != nil {
		return err
	}

	// Try to find if already added
	var t *torrent.Torrent
	data, err := fetchTorrentContent(ctx, task.URL, f.Config.Timeout)
	if err != nil {
		return err
	}

	t, err = session.AddTorrent(bytes.NewReader(data), nil)
	if err != nil {
		if strings.Contains(err.Error(), "already added") {
			for _, existing := range session.ListTorrents() {
				t = existing
				break
			}
		} else {
			return err
		}
	}

	if t == nil {
		return fmt.Errorf("failed to add or find torrent for %s", task.URL)
	}

	// Add external trackers to boost discovery
	extTrackers := getTrackers(ctx, f.Config)
	if len(extTrackers) > 0 {
		maxTrackers := 200
		if len(extTrackers) > maxTrackers {
			extTrackers = extTrackers[:maxTrackers]
		}
		addTrackersInBatches(ctx, t, extTrackers, f.Config.Verbose)
	}

	t.Start()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	lastCompleted := t.Stats().Bytes.Completed
	if task.OnProgress != nil && lastCompleted > 0 {
		// Report initial progress for resumption
		task.OnProgress(int(lastCompleted))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stats := t.Stats()
			
			// Report delta progress to oget bar
			newCompleted := stats.Bytes.Completed
			if task.OnProgress != nil && newCompleted > lastCompleted {
				task.OnProgress(int(newCompleted - lastCompleted))
				lastCompleted = newCompleted
			}

			if f.Config.Verbose {
				trackers := t.Trackers()
				working := 0
				for _, tr := range trackers {
					if tr.Error == nil && !tr.LastAnnounce.IsZero() {
						working++
					}
				}
				log.Printf("[Torrent] Download stats... Peers: %d, Trackers: %d/%d working, Speed: %d KB/s, Progress: %d/%d", 
					stats.Peers.Total, working, len(trackers), stats.Speed.Download/1024, stats.Bytes.Completed, task.Length)
			}
			if stats.Bytes.Completed >= task.Length {
				if task.OnChunkComplete != nil {
					task.OnChunkComplete(task.ChunkID, "")
				}
				return nil 
			}
		}
	}
}

func getTrackers(ctx context.Context, config *Config) []string {
	trackersOnce.Do(func() {
		if config != nil && len(config.TrackerURLs) > 0 {
			var allTrackers []string
			uniqueTrackers := make(map[string]bool)

			for _, trackerURL := range config.TrackerURLs {
				if config.Verbose {
					log.Printf("[BitTorrent] Fetching external trackers from: %s", trackerURL)
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
				log.Printf("[BitTorrent] Fetched %d unique external trackers from %d sources", len(cachedTrackers), len(config.TrackerURLs))
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
		// 1. Setup user-level metadata directory
		home, _ := os.UserHomeDir()
		metaDir := filepath.Join(home, ".oget", "bt")
		if err := os.MkdirAll(metaDir, 0755); err != nil {
			log.Printf("Warning: failed to create BitTorrent metadata directory at %s: %v", metaDir, err)
			// Fallback to local if home is not accessible for some reason
			metaDir = ".oget_bt"
			_ = os.MkdirAll(metaDir, 0755)
		}

		cfg := torrent.DefaultConfig
		cfg.DataDir = "." // Download directly to current directory for consistency
		cfg.Database = filepath.Join(metaDir, "session.db")
		cfg.TrackerHTTPVerifyTLS = false // Bypass TLS verification for trackers
		rainSession, err = torrent.NewSession(cfg)

		if err == nil {
			// 2. Start the janitor goroutine to clean up old records
			go startJanitor(rainSession)
		}
	})
	return rainSession, err
}

// startJanitor periodically cleans up download records older than 30 days.
func startJanitor(s *torrent.Session) {
	// Run cleanup once at start and then every 24 hours
	cleanup := func() {
		now := time.Now()
		expiration := 30 * 24 * time.Hour
		torrents := s.ListTorrents()
		
		removedCount := 0
		for _, t := range torrents {
			// If torrent was added more than 30 days ago
			if now.Sub(t.AddedAt()) > expiration {
				// We only remove if it's not currently downloading/active? 
				// Actually, if it's been there for 30 days, we remove the "record".
				// Rain's RemoveTorrent only removes it from the database, not the disk data.
				err := s.RemoveTorrent(t.ID())
				if err == nil {
					removedCount++
				}
			}
		}
		if removedCount > 0 {
			log.Printf("[BitTorrent] Janitor: Cleaned up %d old download records (older than 30 days)", removedCount)
		}
	}

	cleanup() // Run once on startup

	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for range ticker.C {
		cleanup()
	}
}

// CleanupProtocols handles resource cleanup for all protocols.
func CleanupProtocols(config *Config) {
	if rainSession != nil {
		duration := 30
		if config != nil {
			duration = config.SeedingDuration
		}

		if duration > 0 {
			fmt.Printf("\n[BitTorrent] All tasks finished. Seeding for %ds (Privacy Grace Period)...\n", duration)
			time.Sleep(time.Duration(duration) * time.Second)
		}
		rainSession.Close()
		fmt.Println("[BitTorrent] Seeding stopped. Privacy secured.")
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

func isTorrentResource(resource string) bool {
	lowerRes := strings.ToLower(resource)
	if strings.HasSuffix(lowerRes, ".torrent") {
		return true
	}
	u, err := url.Parse(resource)
	if err != nil {
		return false
	}
	return strings.HasSuffix(strings.ToLower(u.Path), ".torrent")
}

// GetProber returns the appropriate Prober for the given resource.
func GetProber(resource string, config *Config) Prober {
	if isTorrentResource(resource) {
		return NewTorrentProber(config)
	}

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
	if isTorrentResource(resource) {
		return NewTorrentFetcher(config)
	}

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

func addTrackersInBatches(ctx context.Context, t *torrent.Torrent, trackers []string, verbose bool) {
	go func() {
		batchSize := 30
		for i := 0; i < len(trackers); i += batchSize {
			end := i + batchSize
			if end > len(trackers) {
				end = len(trackers)
			}
			for _, tr := range trackers[i:end] {
				_ = t.AddTracker(tr)
				if verbose {
					log.Printf("[BitTorrent] Added tracker: %s", tr)
				}
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

	// Add external trackers in batches (limit to 200 max to prevent FD exhaustion)
	trackers := getTrackers(ctx, p.Config)
	if len(trackers) > 0 {
		maxTrackers := 200
		if len(trackers) > maxTrackers {
			trackers = trackers[:maxTrackers]
		}
		addTrackersInBatches(ctx, t, trackers, p.Config.Verbose)
	}

	// Wait for metadata
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	timeout := time.After(time.Duration(p.Config.MagnetProbeTimeout) * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.NotifyMetadata():
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
		case <-ticker.C:
			if p.Config.Verbose {
				stats := t.Stats()
				trackers := t.Trackers()
				working := 0
				for _, tr := range trackers {
					if tr.Error == nil && !tr.LastAnnounce.IsZero() {
						working++
					}
				}
				log.Printf("[BitTorrent] Probing metadata... Peers: %d, Trackers: %d/%d working, DHT: %d nodes", 
					stats.Peers.Total, working, len(trackers), stats.Addresses.DHT)
			}
		case <-timeout:
			return nil, fmt.Errorf("magnet probe timed out after %ds", p.Config.MagnetProbeTimeout)
		}
	}
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

	lastCompleted := t.Stats().Bytes.Completed
	if task.OnProgress != nil && lastCompleted > 0 {
		// Report initial progress for resumption
		task.OnProgress(int(lastCompleted))
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			stats := t.Stats()
			
			// Report delta progress to oget bar
			newCompleted := stats.Bytes.Completed
			if task.OnProgress != nil && newCompleted > lastCompleted {
				task.OnProgress(int(newCompleted - lastCompleted))
				lastCompleted = newCompleted
			}

			if f.Config.Verbose {
				trackers := t.Trackers()
				working := 0
				for _, tr := range trackers {
					if tr.Error == nil && !tr.LastAnnounce.IsZero() {
						working++
					}
				}
				log.Printf("[Magnet] Download stats... Peers: %d, Trackers: %d/%d working, Speed: %d KB/s, Progress: %d/%d", 
					stats.Peers.Total, working, len(trackers), stats.Speed.Download/1024, stats.Bytes.Completed, task.Length)
			}
			if stats.Bytes.Completed >= task.Length {
				if task.OnChunkComplete != nil {
					task.OnChunkComplete(task.ChunkID, "")
				}
				return nil 
			}
		}
	}
}
