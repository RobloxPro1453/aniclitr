// Package backup exports and restores aniclitr user data as a single portable
// JSON file (config, history, library, mappings). Tokens (mal.json) are never
// included so backups can be shared or synced safely without leaking OAuth keys.
package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"aniclitr/internal/config"
)

const CurrentVersion = 1

// Allowed lists the exact data files included in a portable backup.
var Allowed = []string{
	"config.json",
	"history.json",
	"library.json",
	"mappings.json",
}

// Bundle is the versioned single-file backup envelope.
type Bundle struct {
	Version    int                        `json:"version"`
	AppVersion string                     `json:"app_version,omitempty"`
	CreatedAt  time.Time                  `json:"created_at"`
	Files      map[string]json.RawMessage `json:"files"`
}

// Summary reports what Export or Import touched.
type Summary struct {
	Path       string
	Files      []string
	BackupDir  string // pre-restore snapshot directory (Import only)
	CreatedAt  time.Time
	AppVersion string
}

// DefaultPath returns a timestamped backup file path inside config.Dir().
func DefaultPath(now time.Time) string {
	return filepath.Join(config.Dir(), fmt.Sprintf("aniclitr-yedek-%s.json", now.Format("20060102-150405")))
}

// Export packs existing user data files from config.Dir() into dst atomically.
func Export(dst, appVersion string) (Summary, error) {
	if dst == "" {
		dst = DefaultPath(time.Now())
	}
	dir := config.Dir()
	b := Bundle{
		Version:    CurrentVersion,
		AppVersion: appVersion,
		CreatedAt:  time.Now().UTC(),
		Files:      map[string]json.RawMessage{},
	}
	var included []string
	for _, name := range Allowed {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Summary{}, fmt.Errorf("%s okunamadı: %w", name, err)
		}
		if !json.Valid(raw) {
			return Summary{}, fmt.Errorf("%s geçerli JSON değil", name)
		}
		b.Files[name] = json.RawMessage(raw)
		included = append(included, name)
	}
	out, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return Summary{}, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Summary{}, err
	}
	if err := config.WriteAtomic(dst, out); err != nil {
		return Summary{}, err
	}
	return Summary{
		Path:       dst,
		Files:      included,
		CreatedAt:  b.CreatedAt,
		AppVersion: appVersion,
	}, nil
}

// Import validates a backup file, snapshots any existing data into a
// pre-restore safety folder, and atomically restores all allowed files.
func Import(src string) (Summary, error) {
	raw, err := os.ReadFile(src)
	if err != nil {
		return Summary{}, fmt.Errorf("yedek dosyası okunamadı: %w", err)
	}
	var b Bundle
	if err := json.Unmarshal(raw, &b); err != nil {
		return Summary{}, fmt.Errorf("yedek dosyası bozuk JSON: %w", err)
	}
	if b.Version != CurrentVersion {
		return Summary{}, fmt.Errorf("desteklenmeyen yedek sürümü: %d", b.Version)
	}
	if len(b.Files) == 0 {
		return Summary{}, errors.New("yedek dosyasında geri yüklenecek veri yok")
	}

	allowedSet := map[string]bool{}
	for _, name := range Allowed {
		allowedSet[name] = true
	}
	var toWrite []string
	for name, payload := range b.Files {
		if !allowedSet[name] {
			return Summary{}, fmt.Errorf("izin verilmeyen dosya adı: %q", name)
		}
		if !json.Valid(payload) {
			return Summary{}, fmt.Errorf("yedek içindeki %s bozuk JSON", name)
		}
		toWrite = append(toWrite, name)
	}
	sort.Strings(toWrite)

	dir := config.Dir()
	// Snapshot existing files first so restore can never cause data loss.
	snapDir := filepath.Join(dir, "pre-restore-"+time.Now().Format("20060102-150405"))
	snapped := false
	for _, name := range toWrite {
		existing, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		if !snapped {
			if err := os.MkdirAll(snapDir, 0o755); err != nil {
				return Summary{}, fmt.Errorf("ön-yedek klasörü oluşturulamadı: %w", err)
			}
			snapped = true
		}
		if err := config.WriteAtomic(filepath.Join(snapDir, name), existing); err != nil {
			return Summary{}, fmt.Errorf("ön-yedek yazılamadı (%s): %w", name, err)
		}
	}

	for _, name := range toWrite {
		if err := config.WriteAtomic(filepath.Join(dir, name), b.Files[name]); err != nil {
			return Summary{}, fmt.Errorf("%s geri yüklenemedi: %w", name, err)
		}
	}
	if !snapped {
		snapDir = ""
	}
	return Summary{
		Path:       src,
		Files:      toWrite,
		BackupDir:  snapDir,
		CreatedAt:  b.CreatedAt,
		AppVersion: b.AppVersion,
	}, nil
}
