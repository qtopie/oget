package oget

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// CleanBT cleans up BitTorrent session database and orphaned task directories.
func CleanBT(targetDir string) error {
	if targetDir == "" {
		targetDir = "."
	}

	cleanedCount := 0

	// 1. Clean session database in ~/.oget/bt/session.db
	home, err := os.UserHomeDir()
	if err == nil {
		metaDir := filepath.Join(home, ".oget", "bt")
		sessionDB := filepath.Join(metaDir, "session.db")
		if _, err := os.Stat(sessionDB); err == nil {
			if err := os.Remove(sessionDB); err == nil {
				fmt.Printf("[Clean] Removed BT session database: %s\n", sessionDB)
				cleanedCount++
			}
		}
	}

	// Also check fallback .oget_bt
	if _, err := os.Stat(".oget_bt"); err == nil {
		_ = os.RemoveAll(".oget_bt")
		fmt.Println("[Clean] Removed local metadata directory: .oget_bt")
		cleanedCount++
	}

	// 2. Scan target directory for orphaned random base64 hash directories
	// Rain generates 22-char base64 IDs (e.g. "-cInN4qSEfGRKcz55JzbjA", "AE6364qTEfGRKcz55JzbjA")
	base64DirRegex := regexp.MustCompile(`^[A-Za-z0-9_-]{22}$`)

	entries, err := os.ReadDir(targetDir)
	if err == nil {
		for _, entry := range entries {
			if entry.IsDir() && base64DirRegex.MatchString(entry.Name()) {
				fullPath := filepath.Join(targetDir, entry.Name())
				if err := os.RemoveAll(fullPath); err == nil {
					fmt.Printf("[Clean] Removed orphaned task directory: %s\n", fullPath)
					cleanedCount++
				}
			}
		}
	}

	// 3. Clean up orphaned torrent state files (.B16.oget etc.)
	if err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && (filepath.Ext(name) == ".oget" || filepath.Ext(name) == ".bits" || (len(name) > 6 && name[len(name)-5:] == ".oget")) {
				fullPath := filepath.Join(targetDir, name)
				if err := os.Remove(fullPath); err == nil {
					fmt.Printf("[Clean] Removed leftover state file: %s\n", fullPath)
					cleanedCount++
				}
			}
		}
	}

	if cleanedCount == 0 {
		fmt.Println("[Clean] Everything is clean. No orphan BT tasks or directories found.")
	} else {
		fmt.Printf("[Clean] Successfully cleaned %d item(s).\n", cleanedCount)
	}

	return nil
}
