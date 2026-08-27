package oget

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FFmpegProber implements Prober for M3U8 / HLS and streaming video resources.
type FFmpegProber struct {
	Config *Config
}

// NewFFmpegProber creates a new FFmpegProber instance.
func NewFFmpegProber(config *Config) *FFmpegProber {
	return &FFmpegProber{Config: config}
}

// Probe checks the availability of ffmpeg and verifies the stream resource.
func (p *FFmpegProber) Probe(ctx context.Context, resource string) (*ResourceMetadata, error) {
	// 1. Verify ffmpeg availability early
	var customPath string
	if p.Config != nil {
		customPath = p.Config.FFmpegPath
	}
	if _, err := FindFFmpeg(customPath); err != nil {
		return nil, err
	}

	// 2. Validate URL structure
	u, err := url.Parse(resource)
	if err != nil || u.Scheme == "" {
		return &ResourceMetadata{Size: 0}, nil
	}

	// 3. Quick connectivity check for HTTP/HTTPS resources
	scheme := strings.ToLower(u.Scheme)
	if scheme == "http" || scheme == "https" {
		timeout := 10
		if p.Config != nil && p.Config.Timeout > 0 {
			timeout = p.Config.Timeout
		}

		transport := &http.Transport{}
		if p.Config != nil && p.Config.ProxyURL != "" {
			if proxyURL, err := url.Parse(p.Config.ProxyURL); err == nil {
				transport.Proxy = http.ProxyURL(proxyURL)
			}
		}

		client := &http.Client{
			Timeout:   time.Duration(timeout) * time.Second,
			Transport: transport,
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodHead, resource, nil)
		if err == nil {
			req.Header.Set("User-Agent", fmt.Sprintf("oget/%s", Version))
			if p.Config != nil {
				for k, v := range p.Config.Headers {
					req.Header.Set(k, v)
				}
			}
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode >= 400 {
					// Fallback to limited GET in case HEAD is not allowed (e.g. 405 Method Not Allowed)
					getReq, getErr := http.NewRequestWithContext(ctx, http.MethodGet, resource, nil)
					if getErr == nil {
						getReq.Header.Set("User-Agent", fmt.Sprintf("oget/%s", Version))
						if p.Config != nil {
							for k, v := range p.Config.Headers {
								getReq.Header.Set(k, v)
							}
						}
						getResp, getErr := client.Do(getReq)
						if getErr == nil {
							getResp.Body.Close()
							if getResp.StatusCode >= 400 {
								return nil, fmt.Errorf("remote video resource returned HTTP %d: %s", getResp.StatusCode, resource)
							}
						}
					}
				}
			}
		}
	}

	return &ResourceMetadata{
		Size: 0, // Video stream size is dynamic
	}, nil
}
