package download

import (
	"os"
	"path/filepath"
)

// existingDir returns dir if it exists as a directory, otherwise walks up to
// the nearest existing ancestor directory.
func existingDir(dir string) string {
	if dir == "" {
		return ""
	}
	p := filepath.Clean(dir)
	for {
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return ""
		}
		p = parent
	}
}
