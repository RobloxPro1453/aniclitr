// Package logx is a tiny append-only log in the app data dir. The TUI owns
// the terminal, so anything worth knowing after the fact goes here.
package logx

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"
)

var (
	mu   sync.Mutex
	f    *os.File
	path string
)

// Init opens (and if large, rotates) the log file.
func Init(dir string) {
	mu.Lock()
	defer mu.Unlock()
	path = filepath.Join(dir, "aniclitr.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 2<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, _ = os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	write("INFO", "---- aniclitr başladı ----")
}

// Path is where the log lives (for error messages).
func Path() string { return path }

func write(level, msg string) {
	if f == nil {
		return
	}
	fmt.Fprintf(f, "%s %-5s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), level, msg)
}

// Infof logs an informational line.
func Infof(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	write("INFO", fmt.Sprintf(format, args...))
}

// Errorf logs an error line.
func Errorf(format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	write("ERROR", fmt.Sprintf(format, args...))
}

// Panic records a recovered panic with its stack.
func Panic(where string, r any) {
	mu.Lock()
	defer mu.Unlock()
	write("PANIC", fmt.Sprintf("%s: %v\n%s", where, r, debug.Stack()))
}

// Close flushes the log.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if f != nil {
		write("INFO", "---- aniclitr kapandı ----")
		_ = f.Close()
		f = nil
	}
}
