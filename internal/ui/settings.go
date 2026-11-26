package ui

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/backup"
	"aniclitr/internal/shaders"
)

type setting struct {
	label string
	desc  string
	value func(a *App) string
	on    func(a *App) bool
	act   func(a *App, dir int)
	run   func(a *App) tea.Cmd // an action (opens something) instead of a value
}

type settings struct {
	cur   int
	off   int
	items []setting
	gl    glide
}

var qualities = []string{"best", "1080p", "720p", "480p"}
var primaryHosts = []string{"Tau Video", "AniHub HLS", "Google Drive", "OK.ru", "Sibnet", "Voe", "Vidmoly"}

func newSettings() *settings {
	s := &settings{}
	toggle := func(label, desc string, get func(a *App) *bool) setting {
		return setting{
			label: label, desc: desc,
			on:  func(a *App) bool { return *get(a) },
			act: func(a *App, _ int) { v := get(a); *v = !*v },
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
			act: func(a *App, dir int) {
				v := get(a)
				n := len(opts)
				*v = opts[(indexOf(opts, *v)+dir+n)%n]
				if after != nil {
					after(a)
				}
			},
		}
	}
	s.items = append(s.items,
		setting{
			label: "Tanıtım turu", desc: "Uygulamanın temel kullanımını adım adım anlatan tanıtım turunu yeniden başlatır.",
			value: func(a *App) string { return "başlat" },
			run:   func(a *App) tea.Cmd { return a.startTour() },
		},
		setting{
			label: "Terminalden aç", desc: "aniclitr'i %LOCALAPPDATA%\\Programs\\aniclitr klasörüne kurar ve kullanıcı PATH'ine ekler; böylece her terminalde aniclitr komutuyla açılır. Yönetici izni gerektirmez.",
			on:  func(a *App) bool { return a.hasPath() },
			run: func(a *App) tea.Cmd { return a.togglePath(!a.hasPath()) },
		},
		setting{
			label: "Otomatik güncelle", desc: "Kurulu kopya açılışta yeni sürüm denetler; bulunan güncelleme SHA-256 ile doğrulanır ve sonraki açılışta etkinleşir.",
			on:  func(a *App) bool { return a.cfg.AutoUpdate },
			act: func(a *App, _ int) { a.cfg.AutoUpdate = !a.cfg.AutoUpdate },
		},
		setting{
			label: "Güncellemeleri denetle", desc: "Yeni sürüm olup olmadığını şimdi denetler.",
			value: func(a *App) string { return Version },
			run:   func(a *App) tea.Cmd { return a.checkUpdate(true) },
		},
		setting{
			label: "Verileri yedekle", desc: "Ayarlar, izleme geçmişi, kitaplık ve eşlemeleri taşınabilir bir JSON dosyasına dışa aktarır (OAuth jetonları hariç). Geri yüklemek için: aniclitr geri-yukle <dosya.json>.",
			value: func(a *App) string { return "dışa aktar" },
			run: func(a *App) tea.Cmd {
				ver := Version
				return tea.Batch(a.tick(), run(15*time.Second, func(context.Context) tea.Msg {
					sum, err := backup.Export("", ver)
					if err != nil {
						return toastMsg{text: "yedeklenemedi: " + err.Error(), kind: "err"}
					}
					return toastMsg{text: "yedeklendi: " + sum.Path, kind: "ok"}
				}))
			},
		},
		setting{
			label: "Eksik araçları kur", desc: "Eksik yardımcı araçları (mpv, yt-dlp, aria2c, ffmpeg) otomatik olarak kurar. Terminalden aniclitr kur komutuyla da çalıştırılabilir.",
			value: func(a *App) string {
				if a.toolsBusy {
					return "kuruluyor…"
				}
				if !a.toolsProbed {
					return "denetleniyor…"
				}
				if a.toolsMissing > 0 {
					return fmt.Sprintf("%d eksik", a.toolsMissing)
				}
				return "hepsi kurulu ✓"
			},
			run: func(a *App) tea.Cmd { return a.installMissingTools() },
		},
		setting{
			label: "MyAnimeList", desc: "Kitaplığı MyAnimeList hesabınızla iki yönlü eşitler: durum, puan, izlenen bölüm sayısı ve listeler.",
			value: func(a *App) string {
				if a.mal.Connected() {
					return "bağlı · " + a.mal.User()
				}
				return "bağlı değil"
			},
			run: func(a *App) tea.Cmd { return a.push(newMalScreen(a)) },
		},
		setting{
			label: "AniList", desc: "Kitaplığı AniList hesabınızla OAuth üzerinden iki yönlü eşitler: durum, puan, bölüm ilerlemesi ve özel listeler.",
			value: func(a *App) string {
				if a.al != nil && a.al.Connected() {
					return "bağlı · " + a.al.User()
				}
				return "bağlı değil"
			},
			run: func(a *App) tea.Cmd { return a.push(newAniListScreen(a)) },
		},
		setting{
			label: "Discord Rich Presence", desc: "İzlediğin animeyi, bölümü, canlı ilerleme çubuğunu ve kapak görselini Discord'da gösterir (ekstra ayar gerekmez; özel başlık için config.json → discord_app_id).",
			on:    func(a *App) bool { return a.cfg.DiscordRPC },
			act: func(a *App, _ int) {
				a.cfg.DiscordRPC = !a.cfg.DiscordRPC
				if a.rpc != nil {
					a.rpc.SetEnabled(a.cfg.DiscordRPC)
					if a.cfg.DiscordRPC {
						a.lastRPCKey = ""
						a.syncBrowseDiscord()
					}
				}
			},
		},
		toggle("Masaüstü bildirimleri", "Takip ettiğin serilerin yeni bölümü çıktığında veya indirme tamamlandığında işletim sistemi bildirimi gönderir.", func(a *App) *bool { return &a.cfg.DesktopNotify }),
		setting{
			label: "Kapaktan tema", desc: "Bir anime açıldığında arayüz kapak görselinin renklerini alır; çıkınca seçili temaya döner.",
			on:  func(a *App) bool { return a.cfg.CoverTheme },
			act: func(a *App, _ int) { a.cfg.CoverTheme = !a.cfg.CoverTheme },
		},
		setting{
			label: "Arayüz sesleri", desc: "Gezinme, seçim ve bildirimler için kısa arayüz sesleri. 0 kapalı demektir.",
			value: func(a *App) string {
				if a.cfg.Sounds <= 0 {
					return "kapalı"
				}
				return fmt.Sprintf("%%%d", a.cfg.Sounds)
			},
			act: func(a *App, dir int) {
				levels := []int{0, 20, 35, 60, 100}
				idx := 0
				for i, l := range levels {
					if l == a.cfg.Sounds {
						idx = i
						break
					}
				}
				a.cfg.Sounds = levels[(idx+dir+len(levels))%len(levels)]
				playSound(a, sndSelect)
			},
		},
		cycle("Arayüz", "yeni (varsayılan): Nova arayüzü; üst çubukta indirme dalgası, 3 sütunlu Keşfet, tuş kapakları ve renkli bildirimler. klasik: sade metin tabanlı yedek görünüm.",
			[]string{"yeni", "klasik"}, func(a *App) *string {
				if a.cfg.UI == "" {
					a.cfg.UI = "yeni"
				}
				return &a.cfg.UI
			}, func(a *App) {
				if a.cfg.UI == "yeni" {
					a.cfg.UI = "" // the default stays out of config.json
				}
			}),
		setting{
			label: "Tema", desc: "ctrl+t canlı önizlemeli tema seçiciyi açar. Özel temalar %APPDATA%\\aniclitr\\themes\\*.json dosyası olarak eklenebilir.",
			value: func(a *App) string { return a.themeName },
			act:   func(a *App, _ int) { a.openPalette("tema ") },
		},
		cycle("Ambiyans", "Ana sayfadaki arka plan efekti. otomatik: mevsime ve saate göre seçilir (gece yıldızlar, kışın kar…).", ambientKinds, func(a *App) *string { return &a.cfg.Ambient }, nil),
		cycle("Logo efekti", "Açılışta logonun beliriş efekti. Komut paletinden (ctrl+k) yeniden oynatılabilir.", introKinds, func(a *App) *string { return &a.cfg.Intro }, nil),
		setting{
			label: "Terminal zeminini boya", desc: "Terminal arka planını temanın rengine ayarlar (OSC 11); çıkışta eski rengi geri yükler.",
			on:  func(a *App) bool { return a.cfg.ThemeBg },
			act: func(a *App, _ int) { a.cfg.ThemeBg = !a.cfg.ThemeBg; a.oscUntil = time.Now().Add(300 * time.Millisecond) },
		},
		setting{
			label: "Nerd Font ikonları", desc: "Terminal yazı tipi bir Nerd Font ise ayrıntılı ikonlar kullanılır.",
			on: func(a *App) bool { return nerd },
			act: func(a *App, _ int) {
				nerd = !nerd
				a.cfg.Icons = map[bool]string{true: "nerd", false: "unicode"}[nerd]
			},
		},
		setting{
			label: "Çizici", desc: "hücre (önerilen): yalnızca değişen hücreleri yazar, görselleri titretmez ve kendini onarır. klasik: Bubble Tea'nın satır tabanlı çizicisi; uyumluluk sorunlarında yedek olarak kullanılır. Değişiklik yeniden başlatınca geçerli olur. Ekran bozulursa ctrl+l ekranı yeniden çizer.",
			value: func(a *App) string {
				if a.cfg.Renderer == "klasik" {
					return "klasik"
				}
				return "hücre"
			},
			act: func(a *App, _ int) {
				if a.cfg.Renderer == "klasik" {
					a.cfg.Renderer = "hücre"
				} else {
					a.cfg.Renderer = "klasik"
				}
				a.notify("çizici değişikliği yeniden başlatınca geçerli olur", "warn")
			},
		},
		toggle("Sıkı düzen", "Küçük pencereler için kenar boşluklarını daraltır.", func(a *App) *bool { return &a.cfg.Compact }),
		toggle("İndirme hız grafiği", "İndirilenler ekranında anlık indirme hızının canlı mini grafiğini (▁▂▃▅▇) gösterir.", func(a *App) *bool { return &a.cfg.SpeedGraph }),
		setting{
			label: "Etkileşimli OP/ED kartı",
			desc:  "Açıkken oynatıcıda açılış ve bitiş jeneriği için atlama kartı gösterilir; kapalıyken maraton modunda otomatik atlanır.",
			on:    func(a *App) bool { return !a.cfg.AutoSkipBinge },
			act:   func(a *App, _ int) { a.cfg.AutoSkipBinge = !a.cfg.AutoSkipBinge },
		},
		cycle("Ses Stüdyosu profili", "Oynatıcı ses profili: referans (doğal), gece (dengeli dinamik aralık ve net diyalog), kulaklık (geniş stereo sahne).", []string{"referans", "gece", "kulaklık"}, func(a *App) *string { return &a.cfg.AudioProfile }, nil),
		toggle("Görüntü profili önerisi", "Açılan videonun çözünürlük ve bit hızına göre uygun FSR ve Anime4K profili önerir.", func(a *App) *bool { return &a.cfg.CinemaSuggest }),
		toggle("Öngörülü ön-yükleme", "Açılışta sıradaki bölüm için arka planda bağlantı ve akış bilgilerini önceden hazırlar.", func(a *App) *bool { return &a.cfg.PreloadNext }),
		setting{
			label: "Yerel disk arşivini tara",
			desc:  "İndirme klasöründeki ve config.json → local_dirs dizinlerindeki .mkv/.mp4 anime dosyalarını tarar.",
			value: func(a *App) string {
				if a.local == nil {
					return "tara"
				}
				return fmt.Sprintf("%d dosya", a.local.Count())
			},
			run: func(a *App) tea.Cmd {
				a.notify("yerel disk klasörleri taranıyor…", "ok")
				return tea.Batch(a.scanLocalMediaAsync(), a.tick())
			},
		},
	)
	s.items = append(s.items,
		toggle("OP/ED otomatik atla", "AniSkip ve kaynak verisini kullanarak açılış ve bitiş jeneriklerini atlar.", func(a *App) *bool { return &a.cfg.AutoSkip }),
		toggle("Filler bölümleri atla", "Otomatik geçişte ve n tuşunda filler işaretli bölümleri atlayıp sonraki ana hikâye bölümüne geçer (destekleyen kaynaklarda).", func(a *App) *bool { return &a.cfg.SkipFiller }),
		toggle("Otomatik sonraki bölüm", "Bölüm bitince 6 saniyelik geri sayımın ardından aynı çeviri grubuyla sonraki bölüme geçer.", func(a *App) *bool { return &a.cfg.AutoNext }),
		toggle("Kaynaklar arası yedek", "Bir bölümün tüm sunucuları çalışmazsa ya da bölüm o sitede yoksa diğer açık kaynaklardaki (AnimeciX, AnimeTR, AniHub) karşılığını bulup geçiş önerir.", func(a *App) *bool { return &a.cfg.AutoFallback }),
		toggle("Tam ekran başlat", "Oynatıcı tam ekran açılır.", func(a *App) *bool { return &a.cfg.Fullscreen }),
		setting{
			label: "Çözünürlük yükseltme (FSR)",
			desc:  "AMD FidelityFX FSR 1.0 ile monitör çözünürlüğüne uygun ölçekleme (kapalı · otomatik · 1080p · 2K · 4K). Oynatıcıda m menüsünden değiştirilebilir.",
			value: func(a *App) string { return shaders.ShortResLabel(a.cfg.UpscaleRes) },
			act: func(a *App, dir int) {
				if dir < 0 {
					a.cfg.UpscaleRes = shaders.PrevRes(a.cfg.UpscaleRes)
				} else {
					a.cfg.UpscaleRes = shaders.NextRes(a.cfg.UpscaleRes)
				}
			},
		},
		setting{
			label: "Kalite iyileştirme (Anime4K)",
			desc:  "Anime4K v4 CNN gölgelendiricileri ile çizgi onarımı ve gürültü azaltma (kapalı · hızlı · cam · sinema).",
			value: func(a *App) string { return shaders.ShortLabel(a.cfg.Anime4K) },
			act: func(a *App, dir int) {
				if dir < 0 {
					a.cfg.Anime4K = shaders.Prev(a.cfg.Anime4K)
				} else {
					a.cfg.Anime4K = shaders.Next(a.cfg.Anime4K)
				}
			},
		},
		setting{
			label: "Akıcılık (Frame-Gen)",
			desc:  "mpv GPU zamansal ara kare üretimi ile kamera kaydırmalarında titreşimsiz akıcılık (kapalı · akıcı · 60+ FPS).",
			value: func(a *App) string { return shaders.ShortFGLabel(a.cfg.FrameGen) },
			act: func(a *App, dir int) {
				if dir < 0 {
					a.cfg.FrameGen = shaders.PrevFG(a.cfg.FrameGen)
				} else {
					a.cfg.FrameGen = shaders.NextFG(a.cfg.FrameGen)
				}
			},
		},
		setting{
			label: "Kapak görselleri", desc: "Anime kapaklarını terminalde gösterir.",
			on:  func(a *App) bool { return a.cfg.Posters },
			act: func(a *App, _ int) { a.cfg.Posters = !a.cfg.Posters; a.applyGraphics() },
		},
		setting{
			label: "Görsel kalitesi",
			desc:  "otomatik: terminal Sixel destekliyorsa tam çözünürlük kullanılır (Windows Terminal destekler). sixel: Sixel her zaman kullanılır. blok: renkli karakterlerle çizilir ve her terminalde çalışır.",
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
			act: func(a *App, dir int) {
				opts := []string{"auto", "sixel", "blocks"}
				cur := a.cfg.Graphics
				if cur == "" {
					cur = "auto"
				}
				a.cfg.Graphics = opts[(indexOf(opts, cur)+dir+len(opts))%len(opts)]
				a.applyGraphics()
			},
		},
		toggle("Animasyonlar", "Logo, arka plan efektleri ve geçiş animasyonları. Kapatıldığında arayüz sabit ve anında çizilir.", func(a *App) *bool { return &a.cfg.Motion }),
		setting{
			label: "Tercih edilen kalite", desc: "Seçilen kalite yoksa bir alt kalite kullanılır.",
			value: func(a *App) string {
				if a.cfg.Quality == "" || a.cfg.Quality == "best" {
					return "en iyi"
				}
				return a.cfg.Quality
			},
			act: func(a *App, dir int) {
				cur := a.cfg.Quality
				if cur == "" {
					cur = "best"
				}
				n := len(qualities)
				a.cfg.Quality = qualities[(indexOf(qualities, cur)+dir+n)%n]
			},
		},
		setting{
			label: "Tercih edilen sunucu", desc: "Otomatik oynatmada öncelik verilecek birinci oynatıcı sunucusu. Diğer sunucular yedek olarak sıradadır.",
			value: func(a *App) string {
				if len(a.cfg.HostOrder) > 0 && a.cfg.HostOrder[0] != "" {
					return a.cfg.HostOrder[0]
				}
				return primaryHosts[0]
			},
			act: func(a *App, dir int) {
				cur := primaryHosts[0]
				if len(a.cfg.HostOrder) > 0 && a.cfg.HostOrder[0] != "" {
					cur = a.cfg.HostOrder[0]
				}
				n := len(primaryHosts)
				next := primaryHosts[(indexOf(primaryHosts, cur)+dir+n)%n]
				order := []string{next}
				for _, h := range a.cfg.HostOrder {
					if !strings.EqualFold(h, next) {
						order = append(order, h)
					}
				}
				a.cfg.HostOrder = order
			},
		},
		setting{
			label: "Sunucu sırası", desc: "Hızlı oynatmanın sunucuları deneme sırası; istemediğin sunucuyu \"asla\", yalnızca kullanmak istediklerini \"yalnızca\" olarak işaretle.",
			value: func(a *App) string {
				var tags []string
				if n := len(a.cfg.NeverHosts); n > 0 {
					tags = append(tags, fmt.Sprintf("%d asla", n))
				}
				if n := len(a.cfg.OnlyHosts); n > 0 {
					tags = append(tags, fmt.Sprintf("%d yalnızca", n))
				}
				if len(tags) == 0 {
					return "düzenle"
				}
				return strings.Join(tags, " · ")
			},
			run: func(a *App) tea.Cmd { return a.push(newHostOrder(a)) },
		},
	)
	// keep the list in display order (grouped), so ↑↓ moves visually
	sort.SliceStable(s.items, func(i, j int) bool { return settingGroup(s.items[i].label) < settingGroup(s.items[j].label) })
	return s
}

func (s *settings) crumb() string { return "ayarlar" }

func (s *settings) hints() []hint {
	return []hint{{"←→/enter", "değiştir"}, {"↑↓", "gez"}, {"esc", "kaydet ve dön"}}
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
		return a.tick()
	case "down", "j":
		s.cur = clamp(s.cur+1, 0, len(s.items)-1)
		return a.tick()
	case "pgup":
		s.cur = max(0, s.cur-8)
		return a.tick()
	case "pgdown":
		s.cur = clamp(s.cur+8, 0, len(s.items)-1)
		return a.tick()
	case "home":
		s.cur = 0
		return a.tick()
	case "end":
		s.cur = max(0, len(s.items)-1)
		return a.tick()
	case "enter", " ", "right", "l", "left", "h":
		dir := 1
		if k.String() == "left" || k.String() == "h" {
			dir = -1
		}
		if run := s.items[s.cur].run; run != nil {
			if dir < 0 {
				return nil
			}
			a.flash()
			return run(a)
		}
		before := a.toastAt
		s.items[s.cur].act(a, dir)
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
	case label == "MyAnimeList" || label == "AniList" || label == "Discord Rich Presence" || label == "Masaüstü bildirimleri" || label == "Tanıtım turu" || label == "Terminalden aç" || label == "Otomatik güncelle" || label == "Güncellemeleri denetle" || label == "Verileri yedekle" || label == "Eksik araçları kur" || label == "Yerel disk arşivini tara":
		return 0
	case label == "OP/ED otomatik atla" || label == "Etkileşimli OP/ED kartı" || label == "Ses Stüdyosu profili" || label == "Görüntü profili önerisi" || label == "Öngörülü ön-yükleme" || label == "Filler bölümleri atla" || label == "Otomatik sonraki bölüm" || label == "Kaynaklar arası yedek" || label == "Tam ekran başlat" || label == "Çözünürlük yükseltme (FSR)" || label == "Kalite iyileştirme (Anime4K)" || label == "Akıcılık (Frame-Gen)" || label == "Tercih edilen kalite" || label == "Tercih edilen sunucu" || label == "Sunucu sırası":
		return 2
	}
	return 1
}

var settingGroups = []string{"genel", "görünüm", "oynatma"}

func (s *settings) view(a *App, w, h int) string {
	cw := min(80, w-4)
	head := pageHeadCol("Ayarlar", "Görünüm, oynatma ve hesap tercihleri", "", cw, w, h)
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
