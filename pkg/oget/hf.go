package oget

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"
)

// HFSpec represents parsed Hugging Face model identifier
// e.g. "lmstudio-community/Qwen3.5-4B-GGUF:Q4_K_M"
// or "https://huggingface.co/lmstudio-community/Qwen3.5-4B-GGUF"
// or "hf.co/lmstudio-community/Qwen3.5-4B-GGUF:Q4_K_M"
type HFSpec struct {
	Repo     string // "lmstudio-community/Qwen3.5-4B-GGUF"
	Target   string // "Q4_K_M" or filename pattern
	Revision string // "main" by default
}

// HFFileItem represents a file in HF model tree
type HFFileItem struct {
	Type     string `json:"type"`
	Oid      string `json:"oid"`
	Size     int64  `json:"size"`
	Path     string `json:"path"`
	XetHash  string `json:"xetHash,omitempty"`
	LFS      *struct {
		Oid         string `json:"oid"`
		Size        int64  `json:"size"`
		PointerSize int64  `json:"pointerSize"`
	} `json:"lfs,omitempty"`
}

// IsHFResource checks whether the resource string specifies a Hugging Face model
func IsHFResource(resource string) bool {
	res := strings.TrimSpace(resource)
	lower := strings.ToLower(res)
	if strings.HasPrefix(lower, "hf:") || strings.HasPrefix(lower, "hf://") {
		return true
	}
	if strings.HasPrefix(lower, "https://huggingface.co/") || strings.HasPrefix(lower, "http://huggingface.co/") {
		return true
	}
	if strings.HasPrefix(lower, "https://hf-mirror.com/") || strings.HasPrefix(lower, "http://hf-mirror.com/") {
		return true
	}
	if strings.HasPrefix(lower, "hf.co/") {
		return true
	}
	return false
}

// ParseHFSpec parses resource into HFSpec
func ParseHFSpec(resource string) (*HFSpec, error) {
	raw := strings.TrimSpace(resource)
	// Remove hf:// or hf:
	if strings.HasPrefix(strings.ToLower(raw), "hf://") {
		raw = raw[5:]
	} else if strings.HasPrefix(strings.ToLower(raw), "hf:") {
		raw = raw[3:]
	} else if strings.HasPrefix(strings.ToLower(raw), "https://huggingface.co/") {
		raw = raw[len("https://huggingface.co/"):]
	} else if strings.HasPrefix(strings.ToLower(raw), "http://huggingface.co/") {
		raw = raw[len("http://huggingface.co/"):]
	} else if strings.HasPrefix(strings.ToLower(raw), "https://hf-mirror.com/") {
		raw = raw[len("https://hf-mirror.com/"):]
	} else if strings.HasPrefix(strings.ToLower(raw), "http://hf-mirror.com/") {
		raw = raw[len("http://hf-mirror.com/"):]
	} else if strings.HasPrefix(strings.ToLower(raw), "hf.co/") {
		raw = raw[len("hf.co/"):]
	}

	raw = strings.TrimPrefix(raw, "/")

	// Format might be: repo/name:tag, repo/name/file, or repo/name:file
	// If it contains "resolve/main/...", parse accordingly
	if strings.Contains(raw, "/resolve/") {
		parts := strings.SplitN(raw, "/resolve/", 2)
		repo := parts[0]
		sub := parts[1]
		subParts := strings.SplitN(sub, "/", 2)
		rev := subParts[0]
		file := ""
		if len(subParts) > 1 {
			file = subParts[1]
		}
		return &HFSpec{
			Repo:     strings.Trim(repo, "/"),
			Target:   file,
			Revision: rev,
		}, nil
	}

	revision := "main"
	target := ""

	// Check if colon exists (e.g. repo:tag or repo:file)
	if idx := strings.Index(raw, ":"); idx != -1 {
		target = raw[idx+1:]
		raw = raw[:idx]
	}

	// raw is now the repo path, e.g. lmstudio-community/Qwen3.5-4B-GGUF or with subpaths
	repoParts := strings.Split(strings.Trim(raw, "/"), "/")
	if len(repoParts) < 2 {
		return nil, fmt.Errorf("invalid HuggingFace model repo '%s', expected 'owner/model'", raw)
	}

	// Hugging Face standard repos are owner/model
	repo := repoParts[0] + "/" + repoParts[1]
	if len(repoParts) > 2 && target == "" {
		target = strings.Join(repoParts[2:], "/")
	}

	return &HFSpec{
		Repo:     repo,
		Target:   target,
		Revision: revision,
	}, nil
}

// GetHFEndpoint determines HF endpoint from config, env or default
func GetHFEndpoint(config *Config) string {
	if config != nil && config.HFMirror != "" {
		endpoint := strings.TrimRight(config.HFMirror, "/")
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
		return endpoint
	}
	if env := os.Getenv("HF_ENDPOINT"); env != "" {
		endpoint := strings.TrimRight(env, "/")
		if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
			endpoint = "https://" + endpoint
		}
		return endpoint
	}
	return "https://huggingface.co"
}

// GetHFToken retrieves token from config or env
func GetHFToken(config *Config) string {
	if config != nil && config.HFToken != "" {
		return config.HFToken
	}
	if token := os.Getenv("HF_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("HUGGING_FACE_HUB_TOKEN")
}

// ResolveHFModelFiles fetches the repo tree and returns direct download URLs matching the target
func ResolveHFModelFiles(ctx context.Context, spec *HFSpec, config *Config) ([]string, error) {
	endpoint := GetHFEndpoint(config)
	token := GetHFToken(config)

	apiURL := fmt.Sprintf("%s/api/models/%s/tree/%s?recursive=true", endpoint, spec.Repo, spec.Revision)
	
	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	if config != nil && config.Timeout > 0 {
		client.Timeout = time.Duration(config.Timeout) * time.Second
	}
	if config != nil && config.ProxyURL != "" {
		if pURL, err := url.Parse(config.ProxyURL); err == nil {
			client.Transport = &http.Transport{
				Proxy: http.ProxyURL(pURL),
			}
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch Hugging Face repo tree: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Hugging Face API returned %s: %s", resp.Status, string(body))
	}

	var files []HFFileItem
	if err := json.NewDecoder(resp.Body).Decode(&files); err != nil {
		return nil, fmt.Errorf("failed to decode repo tree: %w", err)
	}

	matchedFiles := matchHFFiles(files, spec.Target)
	if len(matchedFiles) == 0 {
		var available []string
		for _, f := range files {
			lower := strings.ToLower(f.Path)
			if strings.HasSuffix(lower, ".gguf") || strings.HasSuffix(lower, ".onnx") || strings.HasSuffix(lower, ".safetensors") {
				available = append(available, f.Path)
			}
		}
		if len(available) > 0 {
			return nil, fmt.Errorf("no file matched target '%s' in %s. Available model files:\n  - %s",
				spec.Target, spec.Repo, strings.Join(available, "\n  - "))
		}
		return nil, fmt.Errorf("no file matching '%s' found in repository %s", spec.Target, spec.Repo)
	}

	urls := make([]string, 0, len(matchedFiles))
	for _, f := range matchedFiles {
		downloadURL := fmt.Sprintf("%s/%s/resolve/%s/%s", endpoint, spec.Repo, spec.Revision, f.Path)
		urls = append(urls, downloadURL)
	}

	return urls, nil
}

// matchHFFiles filters tree files based on target (quant tag or filename)
func matchHFFiles(files []HFFileItem, target string) []HFFileItem {
	if target == "all" || target == "*" {
		var all []HFFileItem
		for _, f := range files {
			if f.Type == "file" {
				all = append(all, f)
			}
		}
		return all
	}

	if target == "" {
		// 1. If GGUF files exist, prioritize them
		var ggufs []HFFileItem
		for _, f := range files {
			if f.Type == "file" && strings.HasSuffix(strings.ToLower(f.Path), ".gguf") {
				ggufs = append(ggufs, f)
			}
		}
		if len(ggufs) > 0 {
			return ggufs
		}

		// 2. If ONNX files exist, prioritize model.onnx (+ data) or all onnx
		var onnxFiles []HFFileItem
		var defaultONNX []HFFileItem
		for _, f := range files {
			if f.Type == "file" && strings.HasSuffix(strings.ToLower(f.Path), ".onnx") {
				onnxFiles = append(onnxFiles, f)
				if strings.ToLower(path.Base(f.Path)) == "model.onnx" {
					defaultONNX = append(defaultONNX, f)
				}
			}
		}
		if len(defaultONNX) > 0 {
			for _, f := range files {
				if f.Type == "file" && strings.HasPrefix(strings.ToLower(path.Base(f.Path)), "model.onnx_data") {
					defaultONNX = append(defaultONNX, f)
				}
			}
			return defaultONNX
		}
		if len(onnxFiles) > 0 {
			return onnxFiles
		}

		// 3. Fallback to all files
		var nonDirs []HFFileItem
		for _, f := range files {
			if f.Type == "file" {
				nonDirs = append(nonDirs, f)
			}
		}
		return nonDirs
	}

	targetLower := strings.ToLower(target)
	var matched []HFFileItem

	// 1. Exact path match
	for _, f := range files {
		if f.Type == "file" && (f.Path == target || strings.ToLower(f.Path) == targetLower) {
			return []HFFileItem{f}
		}
	}

	// 2. Base filename exact match
	for _, f := range files {
		if f.Type == "file" && strings.ToLower(path.Base(f.Path)) == targetLower {
			return []HFFileItem{f}
		}
	}

	// 2.5 Directory prefix match (e.g. target="onnx" -> all files in onnx/ directory)
	var dirMatched []HFFileItem
	cleanDir := strings.Trim(targetLower, "/")
	for _, f := range files {
		if f.Type == "file" {
			dir := strings.ToLower(path.Dir(f.Path))
			if dir == cleanDir || strings.HasPrefix(dir, cleanDir+"/") {
				dirMatched = append(dirMatched, f)
			}
		}
	}
	if len(dirMatched) > 0 {
		return dirMatched
	}

	// 3. Quantization tag match (e.g. Q4_K_M)
	// Match pattern: (?i)([\._\-]|^)Q4_K_M([\._\-]|\.gguf|$)
	cleanTarget := strings.TrimSuffix(targetLower, ".gguf")
	rePattern := fmt.Sprintf(`(?i)([\._\-]|^)%s([\._\-]|\.gguf|$)`, regexp.QuoteMeta(cleanTarget))
	re, err := regexp.Compile(rePattern)
	if err == nil {
		for _, f := range files {
			if f.Type == "file" && re.MatchString(f.Path) {
				matched = append(matched, f)
			}
		}
	}

	// 4. Substring fallback if still empty
	if len(matched) == 0 {
		for _, f := range files {
			if f.Type == "file" && strings.Contains(strings.ToLower(f.Path), cleanTarget) {
				matched = append(matched, f)
			}
		}
	}

	// Sort files naturally (especially helpful for split ggufs like 00001-of-00004)
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Path < matched[j].Path
	})

	return matched
}
