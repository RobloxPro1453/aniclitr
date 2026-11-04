// Package pathx lets the user type `aniclitr` in any new terminal. It keeps
// an *installed* copy apart from wherever aniclitr was built or downloaded:
//
//   - Windows: %LOCALAPPDATA%\Programs\aniclitr\aniclitr.exe, its folder on
//     the per-user PATH (HKCU\Environment, no admin rights);
//   - elsewhere: ~/.local/share/aniclitr/aniclitr, linked from ~/.local/bin.
//
// The installed copy is the one that updates itself (package update); a
// development build never replaces itself. Nothing here runs unless the
// user asks (or had asked before: Migrate).
package pathx

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Exe is the running executable (symlinks resolved).
func Exe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	return exe, nil
}

func exeName() string {
	if runtime.GOOS == "windows" {
		return "aniclitr.exe"
	}
	return "aniclitr"
}

// InstallDir is where the installed copy lives.
func InstallDir() string {
	if runtime.GOOS == "windows" {
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			base, _ = os.UserConfigDir()
		}
		return filepath.Join(base, "Programs", "aniclitr")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "aniclitr")
}

// Installed is the installed copy's path.
func Installed() string { return filepath.Join(InstallDir(), exeName()) }

// Dir is the folder new terminals run aniclitr from.
func Dir() (string, error) { return InstallDir(), nil }

// RunningInstalled reports whether this process is the installed copy (the
// only one allowed to update itself).
func RunningInstalled() bool {
	exe, err := Exe()
	if err != nil {
		return false
	}
	return sameDir(filepath.Dir(exe), InstallDir())
}

// Has reports whether new terminals will find the installed aniclitr.
func Has() bool {
	if _, err := os.Stat(Installed()); err != nil {
		return false
	}
	return has(InstallDir())
}

// NeedsMigration: an earlier version put the folder of a build (for example
// the source checkout's bin) on PATH instead of an installed copy.
func NeedsMigration() bool {
	exe, err := Exe()
	if err != nil || Has() {
		return false
	}
	dir := filepath.Dir(exe)
	return !sameDir(dir, InstallDir()) && has(dir)
}

// Add installs a copy of the running aniclitr (if none is installed yet,
// or this one is a newer build) and puts its folder on PATH. A build folder
// an earlier version put on PATH is taken off it.
func Add() error {
	exe, err := Exe()
	if err != nil {
		return err
	}
	if !sameDir(filepath.Dir(exe), InstallDir()) {
		if err := install(exe); err != nil {
			return err
		}
		if has(filepath.Dir(exe)) {
			_ = remove(filepath.Dir(exe))
		}
	}
	return add(InstallDir())
}

// Remove takes the installed copy off PATH (its files stay, harmless).
func Remove() error {
	if exe, err := Exe(); err == nil && !sameDir(filepath.Dir(exe), InstallDir()) && has(filepath.Dir(exe)) {
		_ = remove(filepath.Dir(exe))
	}
	return remove(InstallDir())
}

// install copies exe into InstallDir via a temp file, so a half-written
// copy never sits under the real name.
func install(exe string) error {
	if err := os.MkdirAll(InstallDir(), 0o755); err != nil {
		return err
	}
	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(InstallDir(), ".aniclitr-kur-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	_ = os.Chmod(tmp.Name(), 0o755)
	dst := Installed()
	if _, err := os.Stat(dst); err == nil {
		// the installed copy may be running: step it aside like an update
		_ = os.Remove(dst + ".old")
		if err := os.Rename(dst, dst+".old"); err != nil {
			return err
		}
	}
	return os.Rename(tmp.Name(), dst)
}

func sameDir(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(clean(a), clean(b))
	}
	return clean(a) == clean(b)
}

// split and clean are shared by the platform files: PATH entries compare
// case-insensitively on Windows, without trailing separators, with
// %VARS% / $VARS expanded.
func split(p string) []string {
	var out []string
	for _, e := range filepath.SplitList(p) {
		if strings.TrimSpace(e) != "" {
			out = append(out, e)
		}
	}
	return out
}

func clean(p string) string {
	p = os.ExpandEnv(expandWin(strings.TrimSpace(p)))
	p = strings.TrimRight(filepath.Clean(p), `\/`)
	return p
}

// expandWin expands %VAR% references (Windows PATH entries use them).
func expandWin(s string) string {
	for {
		i := strings.Index(s, "%")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+1:], "%")
		if j < 0 {
			return s
		}
		name := s[i+1 : i+1+j]
		val, ok := os.LookupEnv(name)
		if !ok {
			return s
		}
		s = s[:i] + val + s[i+2+j:]
	}
}
