package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/source"
)

type setting struct {
	label string
	desc  string
	value func(a *App) string
	on    func(a *App) bool
	act   func(a *App)
	run   func(a *App) tea.Cmd // an action (opens something) instead of a value
}

type settings struct {
	cur   int
	off   int
	items []setting
	gl    glide
}

var qualities = []string{"best", "1080p", "720p", "480p"}

func newSettings() *settings {
	s := &settings{}
	toggle := func(label, desc string, get func(a *App) *bool) setting {
		return setting{
			label: label, desc: desc,
			on:  func(a *App) bool { return *get(a) },
			act: func(a *App) { v := get(a); *v = !*v },
		}
	}
	cycle := func(label, desc string, opts []string, get func(a *App) *string, after func(a *App)) setting {
		return setting{
			label: label, desc: desc,
			value: func(a *App) string {
				if v := *get(a); v != "auto" {
					return v
				}
				return "otomatik"
			},
			act: func(a *App) {
				v := get(a)
				*v = opts[(indexOf(opts, *v)+1)%len(opts)]
				if after != nil {
					after(a)
				}
			},
		}
	}
	s.items = append(s.items,
		setting{
			label: "Tanıtım turu", desc: "Uygulamanın nasıl kullanıldığını adım adım gösteren turu yeniden başlatır.",
			value: func(a *App) string { return "başlat" },
			run:   func(a *App) tea.Cmd { return a.startTour() },
		},
		setting{
			label: "Terminalden aç", desc: "Her yeni terminalde yalnızca aniclitr yazarak açılır: aniclitr %LOCALAPPDATA%\\Programs\\aniclitr altına kurulur, o klasör kullanıcı PATH'ine eklenir (yönetici izni gerekmez). Kendini güncelleyen kopya budur.",
			on:  func(a *App) bool { return a.path.Has() },
			run: func(a *App) tea.Cmd { return a.togglePath(!a.path.Has()) },
		},
		setting{
			label: "Otomatik güncelle", desc: "Kurulu kopya açılışta GitHub'daki son sürüme bakar; varsa indirir, SHA-256 ile doğrular, bir sonraki açılışta geçerli olur.",
			on:  func(a *App) bool { return a.cfg.AutoUpdate },
			act: func(a *App) { a.cfg.AutoUpdate = !a.cfg.AutoUpdate },
		},
		setting{
			label: "Güncellemeleri denetle", desc: "Şimdi GitHub'a bak.",
			value: func(a *App) string { return Version },
			run:   func(a *App) tea.Cmd { return a.checkUpdate(true) },
		},
		setting{
			label: "MyAnimeList", desc: "Kitaplığı MAL listenle iki yönlü eşitler (durum, puan, izlenen bölüm, listeler).",
			value: func(a *App) string {
				if a.mal.Connected() {
					return "bağlı · " + a.mal.User()
				}
				return "bağlı değil"
			},
			act: func(a *App) { a.stack = append(a.stack, newMalScreen(a)) },
		},
		setting{
			label: "Kapaktan tema", desc: "Bir animeyi açınca arayüz kapağının renklerine bürünür; çıkınca temana döner.",
			on:  func(a *App) bool { return a.cfg.CoverTheme },
			act: func(a *App) { a.cfg.CoverTheme = !a.cfg.CoverTheme },
		},
		setting{
			label: "Arayüz sesleri", desc: "Gezinirken minik tık, seçerken çın, kutlamada arpej. 0 = kapalı.",
			value: func(a *App) string {
				if a.cfg.Sounds <= 0 {
					return "kapalı"
				}
				return fmt.Sprintf("%%%d", a.cfg.Sounds)
			},
			act: func(a *App) {
				levels := []int{0, 20, 35, 60, 100}
				next := levels[0]
				for i, l := range levels {
					if l == a.cfg.Sounds {
						next = levels[(i+1)%len(levels)]
					}
				}
				a.cfg.Sounds = next
				playSound(a, sndSelect)
			},
		},
		setting{
			label: "Tema", desc: "ctrl+t ile canlı önizlemeli seçici açılır. Kendi temanı %APPDATA%\\aniclitr\\themes\\*.json olarak ekleyebilirsin.",
			value: func(a *App) string { return a.themeName },
			act:   func(a *App) { a.openPalette("tema ") },
		},
		cycle("Ambiyans", "Ana ekrandaki hava: otomatik mevsime ve saate göre seçer (gece yıldız, kışın kar…).", ambientKinds, func(a *App) *string { return &a.cfg.Ambient }, nil),
		cycle("Logo efekti", "Açılışta logonun gelişi. Komut paletinden (ctrl+k) yeniden oynatabilirsin.", introKinds, func(a *App) *string { return &a.cfg.Intro }, nil),
		setting{
			label: "Terminal zeminini boya", desc: "Pencere arka planını temanın rengine boyar (OSC 11); çıkışta eski haline döner.",
			on:  func(a *App) bool { return a.cfg.ThemeBg },
			act: func(a *App) { a.cfg.ThemeBg = !a.cfg.ThemeBg; a.oscUntil = time.Now().Add(300 * time.Millisecond) },
		},
		setting{
			label: "Nerd Font ikonları", desc: "Terminal yazı tipin bir Nerd Font ise zengin ikonlar kullanılır.",
			on: func(a *App) bool { return nerd },
			act: func(a *App) {
				nerd = !nerd
				a.cfg.Icons = map[bool]string{true: "nerd", false: "unicode"}[nerd]
			},
		},
		setting{
			label: "Çizici", desc: "hücre: kendi çizicimiz — yalnızca değişen hücreleri yazar, kapaklar titremez, kendini onarır. klasik: Bubble Tea'nın satır çizicisi (yedek). Yeniden başlatınca geçerli olur. Ekran bozulursa ctrl+l.",
			value: func(a *App) string {
				if a.cfg.Renderer == "klasik" {
					return "klasik"
				}
				return "hücre"
			},
			act: func(a *App) {
				if a.cfg.Renderer == "klasik" {
					a.cfg.Renderer = "hücre"
				} else {
					a.cfg.Renderer = "klasik"
				}
				a.notify("çizici değişikliği yeniden başlatınca geçerli olur", "warn")
			},
		},
		toggle("Sıkı düzen", "Kenar boşluklarını daraltır (küçük pencereler için).", func(a *App) *bool { return &a.cfg.Compact }),
		toggle("Arşiv modu", legacyDesc(), func(a *App) *bool { return &a.cfg.Legacy }),
	)
	for _, p := range source.All() {
		in := p.Info()
		if in.Legacy {
			continue
		}
		desc := sourceRole(in) + " — " + in.Note
		id := in.ID
		s.items = append(s.items, setting{
			label: "Kaynak: " + in.Name, desc: desc,
			on: func(a *App) bool { return a.cfg.Enabled(id, false) },
			act: func(a *App) {
				a.cfg.Sources[id] = !a.cfg.Enabled(id, false)
				warnSources(a)
			},
		})
	}
	s.items = append(s.items,
		toggle("OP/ED otomatik atla", "AniSkip verisiyle opening ve ending'i atlar.", func(a *App) *bool { return &a.cfg.AutoSkip }),
		toggle("Otomatik sonraki bölüm", "Bölüm bitince aynı fansubla sıradakine geçer (6 sn geri sayım).", func(a *App) *bool { return &a.cfg.AutoNext }),
		toggle("Tam ekran başlat", "mpv tam ekran açılır.", func(a *App) *bool { return &a.cfg.Fullscreen }),
		setting{
			label: "Kapak görselleri", desc: "Posterleri terminalde çizer.",
			on:  func(a *App) bool { return a.cfg.Posters },
			act: func(a *App) { a.cfg.Posters = !a.cfg.Posters; a.applyGraphics() },
		},
		setting{
			label: "Görsel kalitesi",
			desc:  "otomatik: terminal Sixel destekliyorsa tam çözünürlük (Windows Terminal destekler). sixel: zorla. blok: renkli karakterlerle (her terminalde çalışır).",
			value: func(a *App) string {
				label := map[string]string{"auto": "otomatik", "sixel": "sixel", "blocks": "blok"}[a.cfg.Graphics]
				if label == "" {
					label = "otomatik"
				}
				if a.gfx.on {
					return label + " · tam çözünürlük"
				}
				return label + " · blok"
			},
			act: func(a *App) {
				next := map[string]string{"auto": "sixel", "sixel": "blocks", "blocks": "auto", "": "sixel"}
				a.cfg.Graphics = next[a.cfg.Graphics]
				a.applyGraphics()
			},
		},
		toggle("Animasyonlar", "Logo, sakura yaprakları, geçişler. Kapatınca her şey anında ve sabit çizilir.", func(a *App) *bool { return &a.cfg.Motion }),
		setting{
			label: "Tercih edilen kalite", desc: "Mevcut değilse bir altındaki seçilir.",
			value: func(a *App) string {
				if a.cfg.Quality == "" || a.cfg.Quality == "best" {
					return "en iyi"
				}
				return a.cfg.Quality
			},
			act: func(a *App) {
				for i, q := range qualities {
					if q == a.cfg.Quality {
						a.cfg.Quality = qualities[(i+1)%len(qualities)]
						return
					}
				}
				a.cfg.Quality = qualities[0]
			},
		},
	)
	// keep the list in display order (grouped), so ↑↓ moves visually
	sort.SliceStable(s.items, func(i, j int) bool { return settingGroup(s.items[i].label) < settingGroup(s.items[j].label) })
	return s
}

func (s *settings) crumb() string { return "ayarlar" }

func (s *settings) hints() []hint {
	return []hint{{"enter/space", "değiştir"}, {"↑↓", "gez"}, {"esc", "kaydet ve dön"}}
}

func (s *settings) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "q", "ctrl+o":
		_ = a.cfg.Save()
		a.pop()
	case "up", "k":
		s.cur = max(0, s.cur-1)
	case "down", "j":
		s.cur = clamp(s.cur+1, 0, len(s.items)-1)
	case "enter", " ", "right", "left":
		if run := s.items[s.cur].run; run != nil {
			a.flash()
			return run(a)
		}
		before := a.toastAt
		s.items[s.cur].act(a)
		_ = a.cfg.Save()
		a.flash()
		if a.pal == nil && a.toastAt == before { // a setting's own warning wins
			a.notify("kaydedildi", "ok")
		}
		return a.tick()
	}
	return nil
}

// settingGroup files a setting under a heading.
func settingGroup(label string) int {
	switch {
	case label == "MyAnimeList" || label == "Tanıtım turu" || label == "Terminalden aç" || label == "Otomatik güncelle" || label == "Güncellemeleri denetle":
		return 0
	case strings.HasPrefix(label, "Kaynak:") || label == "Arşiv modu":
		return 3
	case label == "OP/ED otomatik atla" || label == "Otomatik sonraki bölüm" || label == "Tam ekran başlat" || label == "Tercih edilen kalite":
		return 2
	}
	return 1
}

var settingGroups = []string{"genel", "görünüm", "oynatma", "kaynaklar"}

func (s *settings) view(a *App, w, h int) string {
	cw := min(80, w-4)
	head := []string{"", center(gradient("ayarlar", brand, true), w), ""}
	// rows grouped under headings; remember where the cursor's row lands
	var lines []string
	selLine := 0
	band := s.gl.row(a, s.cur)
	for g, name := range settingGroups {
		first := true
		for i, it := range s.items {
			if settingGroup(it.label) != g {
				continue
			}
			if first {
				if len(lines) > 0 {
					lines = append(lines, "")
				}
				lines = append(lines, center(sectionTitle(name, cw, false), w))
				first = false
			}
			sel := i == band
			bg, bar, tc := "", sp("  ", cText), cText
			if sel {
				bg, bar, tc = cBand, sp("▌ ", cAccent), cAccent
			}
			var right []span
			if it.on != nil {
				if it.on(a) {
					right = []span{sp("● açık ", cOK)}
				} else {
					right = []span{sp("○ kapalı ", cMuted)}
				}
			} else if it.run != nil && it.value != nil {
				right = []span{sp(it.value(a)+" ", cText), sp("› ", cAccent)}
			} else if it.value != nil {
				right = []span{sp("‹ ", cFaint), sp(it.value(a), cText), sp(" › ", cFaint)}
			}
			if i == s.cur {
				selLine = len(lines)
			}
			lines = append(lines, center(row(cw, bg, []span{bar, span{t: it.label, c: tc, bold: sel}}, right), w))
		}
	}
	// the description of the selected setting lives in a fixed strip at
	// the bottom, so the list never jumps while you move
	var desc []string
	if s.cur < len(s.items) {
		for _, l := range wrap(s.items[s.cur].desc, cw-4) {
			desc = append(desc, center(fit("  "+stSub.Render(l), cw), w))
		}
	}
	if len(desc) > 3 {
		desc = desc[:3]
	}
	foot := append([]string{center(stFaint.Render(strings.Repeat("─", cw)), w)}, desc...)
	foot = append(foot, center(fit(stMuted.Render("yapılandırma: %APPDATA%\\aniclitr\\config.json"), cw), w))
	listH := max(3, h-len(head)-len(foot))
	s.off = scroll(selLine, s.off, listH, len(lines))
	body := lines[s.off:min(len(lines), s.off+listH)]
	out := append(head, block(body, w, listH)...)
	out = append(out, foot...)
	return strings.Join(out, "\n")
}
