//go:build !windows

package download

import "golang.org/x/sys/unix"

// DiskSpace reports free (caller-available) and total bytes on the volume
// holding dir. If dir does not exist yet, the nearest existing ancestor is
// queried.
func DiskSpace(dir string) (free, total int64, ok bool) {
	p := existingDir(dir)
	if p == "" {
		return 0, 0, false
	}
	var st unix.Statfs_t
	if err := unix.Statfs(p, &st); err != nil || st.Blocks == 0 {
		return 0, 0, false
	}
	bsize := uint64(st.Bsize)
	return int64(uint64(st.Bavail) * bsize), int64(uint64(st.Blocks) * bsize), true
}
