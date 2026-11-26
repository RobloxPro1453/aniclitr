//go:build windows

package download

import "golang.org/x/sys/windows"

// DiskSpace reports free (caller-available) and total bytes on the volume
// holding dir. If dir does not exist yet, the nearest existing ancestor is
// queried.
func DiskSpace(dir string) (free, total int64, ok bool) {
	p := existingDir(dir)
	if p == "" {
		return 0, 0, false
	}
	ptr, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return 0, 0, false
	}
	var avail, tot, totFree uint64
	if err := windows.GetDiskFreeSpaceEx(ptr, &avail, &tot, &totFree); err != nil || tot == 0 {
		return 0, 0, false
	}
	return int64(avail), int64(tot), true
}
