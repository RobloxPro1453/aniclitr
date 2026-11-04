package ui

import (
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"aniclitr/internal/player"
)

// Version is set by main (shown in "what's new").
var Version = "dev"

// whatsNew: shown once after updating to that version. Keep it short.
var whatsNew = map[string][]string{
	"0.11.0": {
		"Kendini güncelleyen kurulum: aniclitr artık %LOCALAPPDATA%\\Programs\\aniclitr altında; yeni sürümleri GitHub'dan kendisi alır",
		"Terminal kısayolu kurulu kopyayı açar; geliştirme klasörüyle karışıklık yok",
		"Ayarlar → Otomatik güncelle · Güncellemeleri denetle; komut satırında aniclitr update",
		"Animasyonlar kare başına yarıdan az bellek harcıyor",
	},
	"0.10.1": {
		"Bellek kullanımı ~250 MB'tan ~50 MB'a indi: bir kapak artık 1,5 MB değil 56 KB tutuyor",
		"Kapaklar aynı görünüyor; uzun oturumlarda bellek artık büyümüyor",
	},
	"0.10.0": {
		"Yeni tanıtım turu: nasıl çalışır, izlerken, kitaplık ve keşif — Ayarlar → Tanıtım turu ile istediğin zaman",
		"Terminalden aç: Ayarlar → Terminalden aç ile her terminalde aniclitr yazman yeter",
		"Anime her zaman ana kaynakta açılır; MAL ilerlemen bölümlerde görünür",
	},
	"0.9.4": {
		"MAL listen bölümlerde görünüyor: izlediğin sayıya kadar ✓, açınca sıradaki bölümdesin",
		"MAL'den gelen durum ve puan, animeyi hangi kaynaktan açarsan aç görünür",
		"w ile bir bölümü geri almak MAL'deki sayıyı da geri çeker",
		"Kitaplıkta durum değişikliği ctrl+z ile geri alınır (MAL'e de gider)",
		"Anime her zaman ana kaynakta açılır; kaldığın bölüm en son izlediğin kaynaktan gelir",
	},
	"0.9.3": {
		"Aynı anime farklı kaynaklarda izlense de rafta tek kart; silince hepsi gider (ctrl+z geri getirir)",
		"s: geçmişteki kaynaklar hemen gelir, kapalı olanlar dahil diğer kaynaklar da aranır",
		"Kaynak değişince kaldığın bölüm taşınır; yeni bölüm uyarısı da kaynaktan bağımsız",
		"Altyazılı sürüm dublajdan önce gelir; boş sezon artık hata gibi görünmez",
		"Ana kaynağı ya da bütün kaynakları kapatınca uyarı çıkar",
	},
	"0.9.2": {
		"Yeni kaynak: AnimeTR — Türkçe altyazılı anime ve donghua, her sezon ayrı seri",
		"Google Drive kaynakları artık 1080p oynuyor ve iniyor",
		"Seride s: arşivden ya da raftan açtığın seri de başka kaynağa geçebiliyor",
	},
	"0.9.1": {
		"İndirirken fansubu sen seçiyorsun: d (bölüm) ve D (sezon) önce fansub seçtirir",
		"Seçtiğin fansub bağlayıcı: kaynağı yoksa başka fansuba geçilmez, açıkça söylenir",
		"İnternet yoksa açılışta sadece indirdiklerin gösterilir; bağlantı gelince ana sayfa döner",
		"Çevrimdışı rafta kapaklar artık görünüyor",
		"İndirme sırasında açık bağlantı sayısı görünür; kopan bağlantılar yeniden denenir",
	},
	"0.9.0": {
		"Cila turu: her ekran aynı dili konuşuyor, dar pencerelerde taşma yok",
		"Ana sayfa sadeleşti; yan panel ne gösterdiğini söylüyor (kaldığın yer · seçili bölüm · öne çıkan)",
		"Alt satırdaki kısayollar sığmazsa yarım kesilmez: önemliler + “? hepsi”",
		"Ayarlar bölümlere ayrıldı (hesap · görünüm · oynatma · kaynaklar) ve küçük pencerede kayar",
		"Arşiv modu artık alt+a (ctrl+l ekranı baştan çizer)",
		"Tarihler Türkçe, yardım penceresi iki sütun, düzinelerce küçük düzeltme",
	},
	"0.8.0": {
		"Kaldığın yer önizlemesi: “devam et”te seçili seride durduğun kare görünür",
		"Bitiş kartı: ending başlayınca mpv'de sıradaki bölüm hatırlatılır (n ile geç)",
		"Keşfet'e “bu akşam” (vaktine göre öneri) ve “türler” (zar · kült klasikler) geldi",
		"Anime sayfasında y: benzer animeler",
		"İndirilenler artık kapaklı bir çevrimdışı kitaplık (v: indirme sırası)",
		"ctrl+z geri al · bozuk kaynakta otomatik olarak sıradakine geçiş",
		"Bölümde bekleyince link önceden hazırlanır; açılış önbellekten anında",
		"Arama kutusunda esc: önce yazıyı siler, sonra kutudan çıkar (m, n kısayolları için)",
	},
}

type onboard struct {
	step     int
	isUpdate bool // only "what's new"
	tour     bool // started again from settings: nothing to mark as seen
	tools    map[string]bool
}

// newOnboard checks the helper tools once; the guide shows what is missing.
func newOnboard(a *App, isUpdate bool) *onboard {
	o := &onboard{isUpdate: isUpdate, tools: map[string]bool{}}
	_, err := player.FindMpv(a.cfg.MpvPath)
	o.tools["mpv"] = err == nil
	for _, t := range []string{"yt-dlp", "aria2c", "ffmpeg"} {
		_, err := exec.LookPath(t)
		o.tools[t] = err == nil
	}
	return o
}

// maybeOnboard puts the guide (first run) or "what's new" (after an
// update) on top of the home screen.
func (a *App) maybeOnboard() {
	if a.cfg.SeenVersion == Version || Version == "dev" {
		return
	}
	first := a.cfg.SeenVersion == "" && len(a.hist.Recent()) == 0
	if !first && len(whatsNew[Version]) == 0 {
		a.markSeen()
		return
	}
	a.stack = append(a.stack, newOnboard(a, !first))
}

// startTour runs the guide again (settings → "Tanıtım turu").
func (a *App) startTour() tea.Cmd {
	o := newOnboard(a, false)
	o.tour = true
	return a.push(o)
}

func (a *App) markSeen() {
	a.cfg.SeenVersion = Version
	_ = a.cfg.Save()
}

// the tour, one idea per step
const (
	obWelcome = iota
	obTools
	obHow
	obWatch
	obShelves
	obMal
	obPath
	obKeys
)

var onboardSteps = []string{"hoş geldin", "araçlar", "nasıl çalışır", "izlerken", "kitaplık ve keşif", "MyAnimeList", "terminal kısayolu", "tuşlar"}

func (o *onboard) crumb() string {
	if o.isUpdate {
		return "yenilikler"
	}
	if o.tour {
		return "tanıtım turu"
	}
	return "başlarken"
}

func (o *onboard) hints() []hint {
	if o.isUpdate {
		return []hint{{"enter", "devam"}, {"esc", "kapat"}}
	}
	var h []hint
	switch o.step {
	case obWelcome:
		h = append(h, hint{"t", "tema seç"})
	case obMal:
		h = append(h, hint{"b", "şimdi bağlan"})
	case obPath:
		h = append(h, hint{"y", "ekle"}, hint{"n", "şimdi değil"})
	}
	return append(h, hint{"→ enter", "ileri"}, hint{"←", "geri"}, hint{"esc", "turu bitir"})
}

func (o *onboard) finish(a *App) tea.Cmd {
	if !o.tour {
		a.markSeen()
	}
	a.pop()
	a.anchorOK = false
	a.celebrate(60)
	if !o.isUpdate {
		a.notify("hazırsın — aramaya bir isim yaz ya da alt+m ile menüyü aç", "ok")
	}
	return a.tick()
}

func (o *onboard) next(a *App) tea.Cmd {
	if o.isUpdate || o.step >= len(onboardSteps)-1 {
		return o.finish(a)
	}
	o.step++
	a.flash()
	return a.tick()
}

func (o *onboard) update(a *App, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "esc", "q":
		return o.finish(a)
	case "enter", "right", "l", " ":
		return o.next(a)
	case "left", "h":
		o.step = max(0, o.step-1)
	case "t":
		if o.step == obWelcome {
			a.openPalette("tema ")
		}
	case "b":
		if o.step == obMal {
			return a.push(newMalScreen(a))
		}
	case "y", "e":
		if o.step == obPath && !o.isUpdate {
			if !a.path.Has() {
				cmd := a.togglePath(true)
				return tea.Batch(cmd, o.next(a))
			}
			return o.next(a)
		}
	case "n":
		if o.step == obPath && !o.isUpdate {
			a.cfg.PathAsked = true
			_ = a.cfg.Save()
			return o.next(a)
		}
	}
	return a.tick()
}

// keyRows lays out "key  meaning" pairs in two aligned columns.
func keyRows(rows [][2]string, kw int) []string {
	var out []string
	for _, r := range rows {
		out = append(out, fit(" "+stText.Bold(true).Render(r[0]), kw)+stSub.Render(r[1]))
	}
	return out
}

func (o *onboard) view(a *App, w, h int) string {
	cw := min(84, w-6)
	var body []string
	title := ""
	if o.isUpdate {
		title = "aniclitr " + Version + " · yenilikler"
		for _, l := range whatsNew[Version] {
			for i, wl := range wrap(l, cw-8) {
				prefix := "   "
				if i == 0 {
					prefix = stAccent.Render(" ◆ ")
				}
				body = append(body, prefix+stText.Render(wl))
			}
		}
		body = append(body, "", stMuted.Render("Hepsi her zaman ? ve alt+m menüsünde."))
	} else {
		title = onboardSteps[o.step]
		para := func(s string) []string {
			var out []string
			for _, l := range wrap(s, cw-6) {
				out = append(out, stSub.Render(l))
			}
			return out
		}
		switch o.step {
		case obWelcome:
			body = []string{stText.Render("aniclitr: terminalde, Türkçe altyazılı anime."), ""}
			body = append(body, para("Ara, fansubunu seç, izle. Kaldığın yer, sıradaki bölüm, açılış ve bitiş atlama kendiliğinden. İnternetsiz izlemek için indir, MyAnimeList ile listeni eşitle.")...)
			body = append(body, "", stMuted.Render("Bu kısa tur her şeyi bir dakikada gösterir. Önce bir görünüm seç: ")+stText.Bold(true).Render("t"))
		case obTools:
			check := func(name, why, install string) []string {
				if o.tools[name] {
					return []string{stOK.Render(" ✓ ") + stBold.Render(name) + stMuted.Render("  "+why)}
				}
				return []string{stErr.Render(" ✗ ") + stBold.Render(name) + stMuted.Render("  "+why), "     " + stAccent.Render(install)}
			}
			body = append(body, check("mpv", "oynatıcı (gerekli)", "winget install mpv-player.mpv-CI.MSVC")...)
			body = append(body, check("yt-dlp", "bazı kaynaklar ve fragmanlar için", "winget install yt-dlp.yt-dlp")...)
			body = append(body, check("aria2c", "hızlı indirme (yoksa dahili indirici)", "winget install aria2.aria2")...)
			body = append(body, check("ffmpeg", "HLS indirmeleri mp4'e çevirmek için", "winget install Gyan.FFmpeg")...)
			if !o.tools["mpv"] {
				body = append(body, "", stWarn.Render("mpv olmadan izleyemezsin — kurup aniclitr'i yeniden aç."))
			}
		case obHow:
			type stp struct{ n, head, sub string }
			cols := []stp{{"1", "ara", "bir isim yaz"}, {"2", "bölüm", "enter ya da p"}, {"3", "fansub", "çeviri ekibi"}, {"4", "izle", "mpv açılır"}}
			cell := max(12, (cw-6)/4)
			var top, bot string
			for i, c := range cols {
				t := fit(stAccent.Render(c.n)+" "+stBold.Render(c.head), cell-2)
				u := fit("  "+stMuted.Render(c.sub), cell-2)
				if i < len(cols)-1 {
					t += stFaint.Render("→ ")
					u += "  "
				}
				top += t
				bot += u
			}
			body = append(body, " "+top, " "+bot, "")
			body = append(body, keyRows([][2]string{
				{"p", "fansub sormadan en iyi kaynakla hemen oynat"},
				{"s", "başka kaynakta aç: " + turkishSources()},
				{"d · D", "bölümü · sezonu indir, internetsiz izle"},
			}, 10)...)
			body = append(body, "")
			body = append(body, para("Bir kaynak açılmazsa sıradaki kendiliğinden denenir; seçtiğin fansub hatırlanır.")...)
		case obWatch:
			body = append(body, para("mpv penceresi öndedir; terminal boşta bekler. Bu tuşlar mpv'de de çalışır:")...)
			body = append(body, "")
			body = append(body, keyRows([][2]string{
				{"n · b", "sonraki · önceki bölüm"},
				{"space", "duraklat"},
				{"← →", "10 saniye sar"},
				{"u", "internetten altyazı ara (terminalde)"},
				{"q", "çık; kaldığın saniye ve kare kaydedilir"},
			}, 10)...)
			body = append(body, "")
			body = append(body, para("Açılış ve bitiş kendiliğinden atlanır. Bölümün %75'i izlenince izlendi sayılır, sıradaki aynı fansubla başlar.")...)
		case obShelves:
			body = append(body, keyRows([][2]string{
				{"ctrl+b", "kitaplık: izliyor, tamamlandı, puanlar, listeler"},
				{"ctrl+e", "keşfet: takvim, bu akşam, türler, öneriler"},
				{"ctrl+d", "indirilenler: kapaklı çevrimdışı raf"},
				{"ctrl+k", "komut paleti: her şeye yazarak ulaş"},
			}, 10)...)
			body = append(body, "")
			body = append(body, para("İzlediğin her anime kitaplığa kendiliğinden girer. Ana sayfadaki “devam et” rafı kaldığın yerden başlatır; boş arama kutusunda enter da aynısını yapar.")...)
		case obMal:
			if a.mal.Connected() {
				body = []string{stOK.Render("● bağlı: ") + stBold.Render(a.mal.User()), "", stSub.Render("Kitaplığın MAL listenle iki yönlü eşitleniyor.")}
			} else {
				body = append(body, para("İsteğe bağlı: MAL hesabını bağlarsan izlediğin bölümler, puanların ve listelerin MyAnimeList'e kendiliğinden gider; MAL'deki listen de buraya gelir.")...)
				body = append(body, "", stText.Bold(true).Render("b")+stMuted.Render(" şimdi bağlan   ·   ")+stText.Bold(true).Render("→")+stMuted.Render(" sonra (ayarlar → MyAnimeList)"))
			}
		case obPath:
			dir, _ := a.path.Dir()
			body = append(body, para("Her terminalde yalnızca aniclitr yazarak açabilmen için aniclitr'i şuraya kurup PATH'e ekleyebilirim:")...)
			body = append(body, "", "  "+stAccent.Render(truncate(dir, cw-8)), "")
			if a.path.Has() {
				body = append(body, stOK.Render(" ✓ ")+stSub.Render("zaten ekli — yeni bir terminalde aniclitr yaz."))
			} else {
				body = append(body, " "+stText.Bold(true).Render("y")+stMuted.Render(" ekle   ·   ")+stText.Bold(true).Render("n")+stMuted.Render(" şimdi değil"), "")
				body = append(body, para("Yönetici izni gerekmez, yalnızca senin kullanıcı PATH'in değişir. Kurulu kopya yeni sürümleri kendisi alır. Ayarlar → Terminalden aç ile istediğin zaman geri alırsın.")...)
			}
		case obKeys:
			body = append(body, keyRows([][2]string{
				{"yaz + enter", "ara (boşken enter: son izlediğine devam)"},
				{"alt+m  ·  alt+n", "menü  ·  bildirimler"},
				{"ctrl+o", "ayarlar (bu tur da orada)"},
				{"ctrl+z", "son silmeyi ya da değişikliği geri al"},
				{"?", "bulunduğun ekranın bütün tuşları"},
			}, 22)...)
			body = append(body, "", stMuted.Render("enter ile bitir. İyi seyirler."))
		}
	}
	// step dots
	dots := ""
	if !o.isUpdate {
		var ds []string
		for i := range onboardSteps {
			if i == o.step {
				ds = append(ds, stAccent.Render("━━"))
			} else {
				ds = append(ds, stFaint.Render("──"))
			}
		}
		dots = strings.Join(ds, " ")
	}
	box := panel(title, append(append([]string{""}, body...), ""), cw, len(body)+4, true, cAccent, dots)
	out := make([]string, 0, h)
	top := max(1, (h-len(box))/2-2)
	for i := 0; i < top; i++ {
		out = append(out, "")
	}
	out = append(out, center(gradient("aniclitr", brand, true), w), "")
	for _, l := range box {
		out = append(out, center(l, w))
	}
	if !o.isUpdate {
		out = append(out, "", center(stMuted.Render(fmt.Sprintf("%d / %d", o.step+1, len(onboardSteps))), w))
	}
	return strings.Join(out, "\n")
}
