package ui

import (
	"runtime/debug"
	"sync/atomic"
	"time"
)

// Memory hygiene. The Go runtime keeps freed memory around for reuse and
// hands it back to the OS only slowly; at the moments the UI has nothing to
// draw (mpv took over, a poster-heavy screen was left) we give it back at
// once, on a background goroutine so no frame waits.

// TuneGC is called once at start: collect at half the default growth and
// keep a soft ceiling. The UI is idle most of the time, so the extra GC work
// costs nothing visible (a frame takes ~1 ms of a 16 ms budget).
func TuneGC() {
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(128 << 20)
}

var lastTrim atomic.Int64

// trimMemory returns free memory to the OS (at most every 5 s).
func trimMemory() {
	now := time.Now().UnixNano()
	if prev := lastTrim.Load(); now-prev < int64(5*time.Second) || !lastTrim.CompareAndSwap(prev, now) {
		return
	}
	go debug.FreeOSMemory()
}
