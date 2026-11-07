package animecix

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"aniclitr/internal/config"
	"aniclitr/internal/logx"
	"aniclitr/internal/match"
	"aniclitr/internal/source"
)

// A video's "extra" field is free text typed by the uploader: sometimes the
// group name ("AnimeOU"), sometimes a whole credit line
// ("https://discord.gg/x Çevirmen: Ali, Encoder: Veli, Uploader: …").
// /secure/translators lists every registered group (~360, ~200 KB), so it is
// fetched at most once a day, kept on disk, and used to turn the credit line
// into the group's real name. Without it (offline, API change) a heuristic
// cleaner is used instead.

const translatorsTTL = 24 * time.Hour

type translator struct {
	Name      string `json:"name"`
	Alt       string `json:"translator"`
	URL       string `json:"translator_url"`
	Discord   string `json:"discord"`
	IsDubbing bool   `json:"isDubbing"`
}

// trEntry is one searchable name or address of a group.
type trEntry struct {
	key  string // normalized name (" word word ") or lower-case host/path
	addr bool   // key is an address, matched as a plain substring
	name string // display name
	dub  bool
}

var (
	trMu      sync.Mutex
	trEntries []trEntry // longest key first
	trAt      time.Time // when trEntries was loaded (zero: never)
	trTried   time.Time // last fetch attempt, so a dead endpoint is not hammered
)

func translatorsPath() string {
	return filepath.Join(config.CacheDir(), "animecix-cevirmenler.json")
}

// translators returns the group index, loading it from disk or the API when
// missing or older than a day. It never fails: on error it returns whatever
// it has (possibly nothing), and the caller falls back to the heuristic.
func translators(ctx context.Context) []trEntry {
	trMu.Lock()
	defer trMu.Unlock()
	if trEntries != nil && time.Since(trAt) < translatorsTTL {
		return trEntries
	}
	if trEntries == nil { // first use: the disk copy, if fresh enough
		if fi, err := os.Stat(translatorsPath()); err == nil {
			if b, err := os.ReadFile(translatorsPath()); err == nil {
				var list []translator
				if json.Unmarshal(b, &list) == nil && len(list) > 0 {
					trEntries, trAt = buildIndex(list), fi.ModTime()
					if time.Since(trAt) < translatorsTTL {
						return trEntries
					}
				}
			}
		}
	}
	if time.Since(trTried) < 10*time.Minute {
		return trEntries
	}
	trTried = time.Now()
	fctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	var list []translator
	if err := get(fctx, "/secure/translators", &list); err != nil || len(list) == 0 {
		if err != nil {
			logx.Infof("animecix çevirmen listesi alınamadı: %v", err)
		}
		return trEntries
	}
	if b, err := json.Marshal(list); err == nil {
		_ = config.WriteAtomic(translatorsPath(), b)
	}
	trEntries, trAt = buildIndex(list), time.Now()
	return trEntries
}

func buildIndex(list []translator) []trEntry {
	var out []trEntry
	seen := map[string]bool{}
	add := func(e trEntry) {
		if e.key == "" || seen[e.key] {
			return
		}
		seen[e.key] = true
		out = append(out, e)
	}
	for _, t := range list {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = strings.TrimSpace(t.Alt)
		}
		if name == "" {
			continue
		}
		for _, n := range []string{t.Name, t.Alt} {
			// very short names ("ab") would match inside unrelated words
			if k := match.Normalize(n); utf8.RuneCountInString(k) >= 3 {
				add(trEntry{key: " " + k + " ", name: name, dub: t.IsDubbing})
			}
		}
		for _, u := range []string{t.URL, t.Discord} {
			if k := addrKey(u); k != "" {
				add(trEntry{key: k, addr: true, name: name, dub: t.IsDubbing})
			}
		}
	}
	// longest first: "Anime Ou Fansub" wins over "Anime Ou"
	sort.SliceStable(out, func(i, j int) bool { return len(out[i].key) > len(out[j].key) })
	return out
}

// addrKey reduces a group's site or invite link to a matchable key:
// "https://www.x-fansub.com/" → "x-fansub.com", "discord.gg/abc" → "discord.gg/abc".
// A bare discord.gg host (no invite code) is useless and returns "".
func addrKey(raw string) string {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	host := strings.TrimPrefix(u.Host, "www.")
	path := strings.Trim(u.Path, "/")
	switch host {
	case "discord.gg", "discord.com", "discordapp.com":
		if path == "" || path == "invite" {
			return ""
		}
		return host + "/" + path
	}
	if !strings.Contains(host, ".") {
		return ""
	}
	return host
}

var (
	reURL    = regexp.MustCompile(`(?i)(?:https?://|www\.|discord\.gg/)\S+`)
	reRole   = regexp.MustCompile(`(?i)(?:çevirmen|çeviri|ceviri|cevirmen|translator|translation|fansub)\s*[:：-]\s*([^,/|;\n]+)`)
	reCredit = regexp.MustCompile(`(?i)\b(?:encoder|redakt[öo]r|uploader|edit[öo]r|timer|kontrol|typesetter|zamanlama)\s*[:：-]`)
)

// groupName turns a video's free-text "extra" into a fansub label, using the
// translators index when it can (idx may be empty).
func groupName(extra string, idx []trEntry) string {
	name, _ := groupInfo(extra, idx)
	return name
}

// groupInfo is groupName plus whether the registered group dubs.
func groupInfo(extra string, idx []trEntry) (string, bool) {
	s := strings.TrimSpace(extra)
	if s == "" {
		return "Bilinmeyen", false
	}
	if e, ok := lookupGroup(s, idx); ok {
		if e.dub && !strings.Contains(strings.ToLower(e.name), "dub") {
			return e.name + " (Dublaj)", true
		}
		return e.name, e.dub
	}
	return cleanCredit(s), false
}

func lookupGroup(s string, idx []trEntry) (trEntry, bool) {
	if len(idx) == 0 {
		return trEntry{}, false
	}
	low := strings.ToLower(s)
	norm := " " + match.Normalize(s) + " "
	for _, e := range idx {
		if e.addr {
			if strings.Contains(low, e.key) {
				return e, true
			}
		} else if strings.Contains(norm, e.key) {
			return e, true
		}
	}
	return trEntry{}, false
}

// cleanCredit is the fallback: drop links, prefer the name after
// "Çevirmen:", cut other credits ("Encoder: …"), trim and cap the length.
func cleanCredit(s string) string {
	raw := s
	s = reURL.ReplaceAllString(s, " ")
	if m := reRole.FindStringSubmatch(s); m != nil {
		s = m[1]
	} else if loc := reCredit.FindStringIndex(s); loc != nil && loc[0] > 0 {
		s = s[:loc[0]]
	}
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, " '\"`.,;:-–—|/")
	if s == "" {
		// nothing but links: name it after the first link's site
		if m := reURL.FindString(raw); m != "" {
			if k := addrKey(m); k != "" {
				return k
			}
		}
		return "Bilinmeyen"
	}
	if r := []rune(s); len(r) > 40 {
		s = strings.TrimSpace(string(r[:39])) + "…"
	}
	return s
}

// compile-time check that the provider still satisfies the contract
var _ source.Provider = Provider{}
