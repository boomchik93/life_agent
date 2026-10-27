package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const backupPrefix = "life-agent-"

// Backup writes a consistent copy of the database into dir (mode 600) with
// VACUUM INTO and removes copies older than keep. It returns the new file path.
func (s *Store) Backup(dir string, keep time.Duration, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, backupPrefix+now.UTC().Format("20060102T150405Z")+".db")
	if _, err := s.DB.Exec(`VACUUM INTO ?`, dst); err != nil {
		return "", fmt.Errorf("vacuum into: %w", err)
	}
	if err := os.Chmod(dst, 0o600); err != nil {
		return "", err
	}
	return dst, pruneBackups(dir, keep, now)
}

func pruneBackups(dir string, keep time.Duration, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, ".db") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) > keep {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}
