//go:build !windows

package pathx

import (
	"errors"
	"os"
	"path/filepath"
)

// Elsewhere a link in ~/.local/bin (on PATH in most distros and macOS
// setups) is less intrusive than editing shell rc files.
func link() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "aniclitr"), nil
}

func has(dir string) bool {
	l, err := link()
	if err != nil {
		return false
	}
	t, err := os.Readlink(l)
	return err == nil && filepath.Dir(t) == dir
}

func add(dir string) error {
	l, err := link()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(l), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(l); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			return errors.New(l + " zaten var (aniclitr'in bağlantısı değil)")
		}
		_ = os.Remove(l)
	}
	return os.Symlink(filepath.Join(dir, "aniclitr"), l)
}

func remove(dir string) error {
	if !has(dir) {
		return nil
	}
	l, _ := link()
	return os.Remove(l)
}
