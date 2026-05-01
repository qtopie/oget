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

	"github.com/anacrolix/torrent"
	"github.com/jlaffaye/ftp"
)

var (
	torrentClient     *torrent.Client
	torrentClientOnce sync.Once
	cachedTrackers    []string
	trackersOnce      sync.Once
)

func getTrackers(ctx context.Context, config *Config) []string {
	trackersOnce.Do(func() {
		if config != nil && config.TrackerURL != "" {
			if config.Verbose {
				log.Printf("[Magnet] Fetching external trackers from: %s", config.TrackerURL)
			}
			cachedTrackers = fetchTrackers(ctx, config.TrackerURL)
			if config.Verbose {
				log.Printf("[Magnet] Fetched %d external trackers", len(cachedTrackers))
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

func getTorrentClient(config *Config) (*torrent.Client, error) {
	var err error
	torrentClientOnce.Do(func() {
		cfg := torrent.NewDefaultClientConfig()
		cfg.DataDir = os.TempDir()
		if config != nil && !config.Verbose {
			cfg.NoDefaultPortForwarding = true
		}
		torrentClient, err = torrent.NewClient(cfg)
	})
	return torrentClient, err
}

// CleanupProtocols handles resource cleanup for all protocols.
// For Magnet links, it implements a configurable seeding grace period before stopping for privacy.
func CleanupProtocols(config *Config) {
	if torrentClient != nil {
		duration := 30
		if config != nil {
			duration = config.SeedingDuration
		}
		
		if duration > 0 {
			fmt.Printf("\n[Magnet] All magnet tasks finished. Seeding for %ds (Privacy Grace Period)...\n", duration)
			time.Sleep(time.Duration(duration) * time.Second)
		}
		torrentClient.Close()
		fmt.Println("[Magnet] Seeding stopped. Privacy secured.")
	}
}

// DispatchFetcher dispatches the fetch request to the appropriate fetcher based on the URL scheme.
type DispatchFetcher struct {
	Config *Config
}

func NewDispatchFetcher(config *Config) *DispatchFetcher {
	return &DispatchFetcher{Config: config}
}

func (f *DispatchFetcher) Fetch(ctx context.Context, task *ChunkTask) error {
	fetcher := GetFetcher(task.URL, f.Config)
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

		// Read from FTP response and write to storage
		// Note: We use a limited reader to ensure we don't read beyond the chunk length
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
		// For FTP we don't have a simple way to get chunk hash from server,
		// so we just signal completion.
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

// MagnetProber implements Prober for Magnet/BitTorrent protocol.
type MagnetProber struct {
	Config *Config
}

func NewMagnetProber(config *Config) *MagnetProber {
	return &MagnetProber{Config: config}
}

func (p *MagnetProber) Probe(ctx context.Context, resource string) (*ResourceMetadata, error) {
	client, err := getTorrentClient(p.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create torrent client: %w", err)
	}

	t, err := client.AddMagnet(resource)
	if err != nil {
		return nil, fmt.Errorf("failed to add magnet: %w", err)
	}

	// Add external trackers
	trackers := getTrackers(ctx, p.Config)
	if len(trackers) > 0 {
		var groups [][]string
		for _, tr := range trackers {
			groups = append(groups, []string{tr})
		}
		t.AddTrackers(groups)
	}

	// Set a timeout for probing metadata
	probeCtx, cancel := context.WithTimeout(ctx, time.Duration(p.Config.MagnetProbeTimeout)*time.Second)
	defer cancel()

	if p.Config.Verbose {
		ticker := time.NewTicker(2 * time.Second)
		go func() {
			defer ticker.Stop()
			for {
				select {
				case <-probeCtx.Done():
					return
				case <-t.GotInfo():
					return
				case <-ticker.C:
					stats := t.Stats()
					log.Printf("[Magnet Verbose] Finding metadata... Active Peers: %d, Total Peers: %d", 
						stats.ActivePeers, stats.TotalPeers)
				}
			}
		}()
	}

	select {
	case <-probeCtx.Done():
		if probeCtx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("magnet probe timed out after %ds (no peers found or metadata unavailable)", p.Config.MagnetProbeTimeout)
		}
		return nil, probeCtx.Err()
	case <-t.GotInfo():
	}

	// For oget, we pick the largest file in the torrent for now
	var targetFile *torrent.File
	for _, f := range t.Files() {
		if targetFile == nil || f.Length() > targetFile.Length() {
			targetFile = f
		}
	}

	if targetFile == nil {
		return nil, fmt.Errorf("no files found in torrent")
	}

	return &ResourceMetadata{
		Size: targetFile.Length(),
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
	client, err := getTorrentClient(f.Config)
	if err != nil {
		return err
	}

	t, err := client.AddMagnet(task.URL)
	if err != nil {
		return err
	}

	// Add external trackers
	trackers := getTrackers(ctx, f.Config)
	if len(trackers) > 0 {
		var groups [][]string
		for _, tr := range trackers {
			groups = append(groups, []string{tr})
		}
		t.AddTrackers(groups)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.GotInfo():
	}

	if f.Config.Verbose && task.ChunkID%10 == 0 {
		stats := t.Stats()
		log.Printf("[Magnet Verbose] Chunk %d: Connected Peers: %d, Downloaded: %s", 
			task.ChunkID, stats.ActivePeers, humanizeSize(stats.BytesReadData.Int64()))
	}

	var targetFile *torrent.File
	for _, fi := range t.Files() {
		if fi.Length() == task.Length || (task.Length == -1 && fi.Length() > 0) { // Simple heuristic
			targetFile = fi
			break
		}
	}

	if targetFile == nil {
		// Fallback to largest
		for _, fi := range t.Files() {
			if targetFile == nil || fi.Length() > targetFile.Length() {
				targetFile = fi
			}
		}
	}

	reader := targetFile.NewReader()
	defer reader.Close()

	_, err = reader.Seek(task.Offset, io.SeekStart)
	if err != nil {
		return err
	}

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

		lr := io.LimitReader(reader, remaining)
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
		return fmt.Errorf("magnet download incomplete: got %d bytes, want %d", written, task.Length)
	}

	if task.OnChunkComplete != nil {
		task.OnChunkComplete(task.ChunkID, "")
	}

	return nil
}
