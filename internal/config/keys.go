package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// KeyMap maps logical global actions to custom key strings (stored in
// %APPDATA%\aniclitr\keys.json). Users only need to specify the keys they want
// to override; unspecified actions keep their built-in shortcuts.
type KeyMap struct {
	Bindings map[string]string // action -> key string (lowercase)
	byKey    map[string]string // key string -> action
}

// DefaultKeyBindings lists the built-in global shortcuts.
var DefaultKeyBindings = map[string]string{
	"palette":   "ctrl+k",
	"themes":    "ctrl+t",
	"library":   "ctrl+b",
	"downloads": "ctrl+d",
	"discover":  "ctrl+e",
	"torrent":   "alt+t",
	"undo":      "ctrl+z",
	"menu":      "alt+m",
	"bell":      "alt+n",
	"back":      "alt+left",
	"forward":   "alt+right",
	"redraw":    "ctrl+l",
	"actions":   "f1",
}

func keysPath() string { return filepath.Join(Dir(), "keys.json") }

// LoadKeys reads optional keys.json overrides merged on top of DefaultKeyBindings.
func LoadKeys() KeyMap {
	km := KeyMap{
		Bindings: make(map[string]string, len(DefaultKeyBindings)),
		byKey:    make(map[string]string, len(DefaultKeyBindings)),
	}
	for act, k := range DefaultKeyBindings {
		km.Bindings[act] = k
	}
	if b, err := os.ReadFile(keysPath()); err == nil {
		var custom map[string]string
		if err := json.Unmarshal(b, &custom); err != nil {
			_ = os.WriteFile(keysPath()+".bozuk", b, 0o644)
		} else {
			for act, k := range custom {
				act = strings.ToLower(strings.TrimSpace(act))
				k = strings.ToLower(strings.TrimSpace(k))
				if _, valid := DefaultKeyBindings[act]; valid && k != "" {
					km.Bindings[act] = k
				}
			}
		}
	}
	km.rebuild()
	return km
}

func (km *KeyMap) rebuild() {
	km.byKey = make(map[string]string, len(km.Bindings))
	for act, k := range km.Bindings {
		km.byKey[strings.ToLower(k)] = act
	}
}

// SaveKeys writes the current keymap to keys.json atomically.
func (km KeyMap) SaveKeys() error {
	b, err := json.MarshalIndent(km.Bindings, "", "  ")
	if err != nil {
		return err
	}
	return WriteAtomic(keysPath(), b)
}

// Action returns the logical global action bound to pressed key (case-insensitive).
func (km KeyMap) Action(pressed string) (string, bool) {
	act, ok := km.byKey[strings.ToLower(pressed)]
	return act, ok
}
