package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/pathx"
	"aniclitr/internal/update"
)

// pathOps is the "type aniclitr in any terminal" switch and the installed
// copy behind it. Tests swap it for a fake (pathui_test.go), so no test
// ever touches the user's real PATH or installs anything.
type pathOps interface {
	Has() bool
	Add() error
	Remove() error
	Dir() (string, error)
	NeedsMigration() bool
	RunningInstalled() bool
	Installed() string // path of the installed copy
}

type realPath struct{}

func (realPath) Has() bool              { return pathx.Has() }
func (realPath) Add() error             { return pathx.Add() }
func (realPath) Remove() error          { return pathx.Remove() }
func (realPath) Dir() (string, error)   { return pathx.Dir() }
func (realPath) NeedsMigration() bool   { return pathx.NeedsMigration() }
func (realPath) RunningInstalled() bool { return pathx.RunningInstalled() }
func (realPath) Installed() string      { return pathx.Installed() }

var newPathOps = func() pathOps { return realPath{} }

// togglePath installs aniclitr and puts it on PATH (or takes it off).
func (a *App) togglePath(on bool) tea.Cmd {
	var err error
	if on {
		err = a.path.Add()
	} else {
		err = a.path.Remove()
	}
	a.cfg.PathAsked = true
	_ = a.cfg.Save()
	switch {
	case err != nil:
		a.notifyErr("PATH", err)
	case on:
		a.notify("kuruldu — yeni bir terminal açıp aniclitr yazman yeter", "ok")
		a.flash()
		return tea.Batch(a.tick(), a.officialInstall())
	default:
		a.notify("PATH'ten çıkarıldı", "ok")
	}
	return a.tick()
}

// officialInstall puts the published release over the copy that was just
// installed. Installing copies whatever build is running, and a local build
// may not know where updates come from; the release always does, so the
// installed copy can keep itself current from then on. Offline, or with no
// release yet, the copied build simply stays.
func (a *App) officialInstall() tea.Cmd {
	if update.Repo == "" || a.path.RunningInstalled() {
		return nil
	}
	target, cur := a.path.Installed(), Version
	return guard("resmi sürüm", func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		rel, err := update.Latest(ctx)
		if err != nil || !rel.Ready() || update.Newer(cur, rel.Version) {
			return nil // nothing published, or this build is newer than the release
		}
		if err := update.Install(ctx, rel, target); err != nil {
			return toastMsg{"kurulu kopya resmi sürüme geçirilemedi: " + err.Error(), "warn"}
		}
		return toastMsg{"kurulu kopya resmi sürümde (" + rel.Version + ") · güncellemeleri kendisi alır", "ok"}
	})
}

// migratePath: an earlier version put a build folder (clipro\bin) on PATH;
// move that to an installed copy, which is the one that updates itself.
func (a *App) migratePath() tea.Cmd {
	if !a.path.NeedsMigration() {
		return nil
	}
	return tea.Sequence(a.movePath(), a.officialInstall())
}

// movePath installs the running build and switches PATH to it.
func (a *App) movePath() tea.Cmd {
	p := a.path
	return guard("PATH taşıma", func() tea.Msg {
		if err := p.Add(); err != nil {
			return toastMsg{"terminal kısayolu taşınamadı: " + err.Error(), "warn"}
		}
		dir, _ := p.Dir()
		return toastMsg{"terminal kısayolu artık kurulu kopyayı açıyor (" + dir + ")", "ok"}
	})
}

// ---- updates (GitHub releases) ----

type updateMsg struct {
	rel       update.Release
	newer     bool
	installed bool // the new version is in place for the next start
	manual    bool // asked from settings: say "up to date" too
	err       error
}

var defaultExePath = pathx.Exe

var exePath = defaultExePath

// checkUpdate looks for a newer release; the installed copy also fetches
// and swaps it in (it applies on the next start). A development build only
// says that one exists.
func (a *App) checkUpdate(manual bool) tea.Cmd {
	if update.Repo == "" || Version == "dev" || (!manual && !a.cfg.AutoUpdate) {
		if manual {
			a.notify("bu derlemede güncelleme denetimi yok (geliştirme sürümü)", "warn")
		}
		return nil
	}
	if manual {
		a.notify("güncellemeler denetleniyor…", "")
	}
	installed := a.path.RunningInstalled()
	cur := Version
	return guard("güncelleme", func() tea.Msg {
		if !manual {
			time.Sleep(3 * time.Second) // let the first screens load first
		}
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		rel, err := update.Latest(ctx)
		m := updateMsg{rel: rel, manual: manual, err: err}
		if err != nil || !update.Newer(rel.Version, cur) {
			return m
		}
		m.newer = true
		if installed && rel.Ready() {
			exe, err := exePath()
			if err == nil {
				err = update.Install(ctx, rel, exe)
			}
			m.err, m.installed = err, err == nil
		}
		return m
	})
}

func (a *App) onUpdate(m updateMsg) {
	switch {
	case m.installed:
		a.addNotice(notice{key: "update:" + m.rel.Version, kind: "new", text: "aniclitr " + m.rel.Version + " kuruldu — yeniden açınca geçerli"})
		a.notify("aniclitr "+m.rel.Version+" indirildi · yeniden açınca geçerli", "ok")
	case m.newer && m.err != nil:
		a.notifyErr("güncelleme "+m.rel.Version, m.err)
	case m.newer:
		a.addNotice(notice{key: "update:" + m.rel.Version, kind: "new", text: "yeni sürüm: aniclitr " + m.rel.Version + " · kurulu kopya kendini günceller"})
		if m.manual {
			a.notify("yeni sürüm var: "+m.rel.Version, "ok")
		}
	case m.manual && m.err != nil:
		a.notifyErr("güncelleme", m.err)
	case m.manual:
		a.notify("güncelsin · aniclitr "+Version, "ok")
	}
}
