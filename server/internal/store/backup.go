package store

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

const backupPrefix = "life-agent-"

// Backup writes a consistent copy of the database into dir (mode 600) with
// VACUUM INTO and removes copies older than keep. It returns the new file path.
//
// With a recipient (an age public key) the copy is encrypted and only the
// ".db.age" file stays on disk: the plain copy exists for a moment in a
// temporary file and is removed. The private key lives on the Mac, so the
// server cannot read its backups. Without a recipient the copy is plain.
func (s *Store) Backup(dir string, keep time.Duration, now time.Time, recipient string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := filepath.Join(dir, backupPrefix+now.UTC().Format("20060102T150405Z")+".db")
	plain := base
	if recipient != "" {
		plain = base + ".tmp"
	}
	if _, err := s.DB.Exec(`VACUUM INTO ?`, plain); err != nil {
		return "", fmt.Errorf("vacuum into: %w", err)
	}
	if err := os.Chmod(plain, 0o600); err != nil {
		return "", err
	}
	dst := plain
	if recipient != "" {
		defer os.Remove(plain)
		dst = base + ".age"
		if err := encryptFile(plain, dst, recipient); err != nil {
			os.Remove(dst)
			return "", fmt.Errorf("encrypt backup: %w", err)
		}
	}
	return dst, pruneBackups(dir, keep, now)
}

func encryptFile(src, dst, recipient string) error {
	rcpt, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	w, err := age.Encrypt(out, rcpt)
	if err != nil {
		out.Close()
		return err
	}
	if _, err := io.Copy(w, in); err != nil {
		out.Close()
		return err
	}
	if err := w.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func isBackupName(name string) bool {
	return strings.HasPrefix(name, backupPrefix) &&
		(strings.HasSuffix(name, ".db") || strings.HasSuffix(name, ".db.age") || strings.HasSuffix(name, ".db.tmp"))
}

func pruneBackups(dir string, keep time.Duration, now time.Time) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !isBackupName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if now.Sub(info.ModTime()) > keep {
			if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
