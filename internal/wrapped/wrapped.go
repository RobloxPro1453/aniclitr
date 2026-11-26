// Package wrapped analyses the user's real watch history and library to build
// an interactive Spotify-Wrapped-style story with a lightweight deterministic
// Turkish persona & commentary engine (zero ML models) plus a shareable PNG card.
package wrapped

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"aniclitr/internal/store"
)

// GenreStat is one genre's weight in the user's library.
type GenreStat struct {
	Name  string
	Count int
	Share float64 // 0..1
}

// ShowStat is one of the user's top anime.
type ShowStat struct {
	Title   string
	Watched int
	Score   int
	Status  string
}

// Report holds the computed Wrapped numbers and personalized Turkish sentences.
type Report struct {
	Year           int
	TotalSeconds   float64
	TotalHours     float64
	TotalEpisodes  int
	TotalSeries    int
	CompletedCount int
	CompletionRate int // 0..100
	StreakDays     int
	AvgScore       float64
	RatedCount     int
	PeakPeriod     string // "Gece Kuşu", "Akşam Sefası", "Öğle Molası", "Sabahçı"
	PeakHourRange  string // "00:00 – 05:00"
	DayMinutes     [7]int // Mon..Sun estimated minutes
	TopGenres      []GenreStat
	TopShows       []ShowStat

	// Personalized narrative engine outputs
	Archetype      string
	ArchetypeBadge string
	Tagline        string
	TimeComparison string
	HabitSentence  string
	GenreSentence  string
	ShowSentence   string
	PersonaStory   []string
}

// Build analyses hist and lib and constructs a rich personalized Wrapped report.
func Build(hist *store.History, lib *store.Library, now time.Time) Report {
	r := Report{Year: now.Year()}

	var eps int
	var secs float64
	if hist != nil {
		eps, secs = hist.Totals()
		r.StreakDays = hist.Streak()
	}
	var entries []store.Entry
	if hist != nil {
		entries = hist.Recent()
	}
	var items []store.Item
	if lib != nil {
		items = lib.All()
	}

	// Supplement episode count / hours from library when local watch timer is small.
	libEps := 0
	for _, it := range items {
		libEps += it.Watched
		if it.Status == store.Completed {
			r.CompletedCount++
		}
		if it.Score > 0 {
			r.AvgScore += float64(it.Score)
			r.RatedCount++
		}
	}
	if libEps > eps {
		eps = libEps
	}
	if secs < float64(eps)*600 {
		// Estimate 23 minutes per watched episode when historical timer wasn't running.
		secs = float64(eps) * 23 * 60
	}
	r.TotalSeconds = secs
	r.TotalHours = secs / 3600.0
	r.TotalEpisodes = eps
	r.TotalSeries = max(len(items), len(entries))
	if r.TotalSeries > 0 {
		r.CompletionRate = r.CompletedCount * 100 / r.TotalSeries
	}
	if r.RatedCount > 0 {
		r.AvgScore /= float64(r.RatedCount)
	}

	// Peak time-of-day & weekday activity from recent updates.
	var buckets [4]int // 0: night(0-5), 1: morning(6-11), 2: afternoon(12-17), 3: evening(18-23)
	for _, e := range entries {
		if e.UpdatedAt.IsZero() {
			continue
		}
		lt := e.UpdatedAt.Local()
		hr := lt.Hour()
		buckets[hr/6]++
		wd := (int(lt.Weekday()) + 6) % 7 // Mon=0..Sun=6
		mins := max(23, int(e.Position/60))
		r.DayMinutes[wd] += mins
	}
	bestB := 3 // default evening
	bestV := -1
	for i, v := range buckets {
		if v > bestV {
			bestV = v
			bestB = i
		}
	}
	switch bestB {
	case 0:
		r.PeakPeriod, r.PeakHourRange = "Gece Kuşu", "00:00 – 05:59"
	case 1:
		r.PeakPeriod, r.PeakHourRange = "Sabah Enerjisi", "06:00 – 11:59"
	case 2:
		r.PeakPeriod, r.PeakHourRange = "Öğle Molası", "12:00 – 17:59"
	default:
		r.PeakPeriod, r.PeakHourRange = "Akşam Sefası", "18:00 – 23:59"
	}

	// Genres from library.
	gCounts := map[string]int{}
	gTotal := 0
	for _, it := range items {
		for _, g := range it.Genres {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			gCounts[g]++
			gTotal++
		}
	}
	for name, cnt := range gCounts {
		share := 0.0
		if gTotal > 0 {
			share = float64(cnt) / float64(gTotal)
		}
		r.TopGenres = append(r.TopGenres, GenreStat{Name: name, Count: cnt, Share: share})
	}
	sort.Slice(r.TopGenres, func(i, j int) bool {
		if r.TopGenres[i].Count != r.TopGenres[j].Count {
			return r.TopGenres[i].Count > r.TopGenres[j].Count
		}
		return r.TopGenres[i].Name < r.TopGenres[j].Name
	})
	if len(r.TopGenres) > 5 {
		r.TopGenres = r.TopGenres[:5]
	}

	// Top shows combining library score + watched episodes + history presence.
	seen := map[string]ShowStat{}
	for _, it := range items {
		k := strings.ToLower(strings.TrimSpace(it.Title))
		if k == "" {
			continue
		}
		seen[k] = ShowStat{Title: it.Title, Watched: it.Watched, Score: it.Score, Status: it.Status}
	}
	for _, e := range entries {
		k := strings.ToLower(strings.TrimSpace(e.Title))
		if k == "" {
			continue
		}
		st := seen[k]
		if st.Title == "" {
			st.Title = e.Title
		}
		w := max(len(e.Watched), e.Episode)
		if w > st.Watched {
			st.Watched = w
		}
		seen[k] = st
	}
	for _, s := range seen {
		r.TopShows = append(r.TopShows, s)
	}
	sort.Slice(r.TopShows, func(i, j int) bool {
		// Favor high score *and* episodes watched.
		si := r.TopShows[i].Score*15 + r.TopShows[i].Watched*2
		sj := r.TopShows[j].Score*15 + r.TopShows[j].Watched*2
		if si != sj {
			return si > sj
		}
		return r.TopShows[i].Title < r.TopShows[j].Title
	})
	if len(r.TopShows) > 5 {
		r.TopShows = r.TopShows[:5]
	}

	composeNarrative(&r)
	return r
}

func composeNarrative(r *Report) {
	// 1. Fun real-world time comparison.
	switch {
	case r.TotalHours >= 120:
		flights := max(1, int(r.TotalHours/11.5))
		r.TimeComparison = fmt.Sprintf("Bu sürede İstanbul'dan Tokyo'ya tam %d kez uçabilirdin — ama ekran başı çok daha keyifliydi.", flights)
	case r.TotalHours >= 40:
		lotr := max(1, int(r.TotalHours/11.4))
		r.TimeComparison = fmt.Sprintf("Yüzüklerin Efendisi üçlemesini baştan sona %d kez bitirecek kadar anime dünyasında yaşadın.", lotr)
	case r.TotalHours >= 10:
		r.TimeComparison = fmt.Sprintf("Tam %.0f saat: hafta sonu kaçamağı değil, ciddi bir anime yolculuğu.", r.TotalHours)
	case r.TotalEpisodes > 0:
		r.TimeComparison = fmt.Sprintf("%d bölümle ısınma turlarını attın — asıl maraton şimdi başlıyor.", r.TotalEpisodes)
	default:
		r.TimeComparison = "Henüz ilk bölümünü açmadın; ilk seriyi başlattığında hikâyen burada canlanacak."
	}

	// 2. Habit sentence.
	switch r.PeakPeriod {
	case "Gece Kuşu":
		r.HabitSentence = "Herkes uyurken sen 'bir bölüm daha' diyerek gece yarısını devirmişsin. En verimli saatin " + r.PeakHourRange + "."
	case "Sabah Enerjisi":
		r.HabitSentence = "Güne kahveyle değil opening şarkısıyla başlıyorsun (" + r.PeakHourRange + ")."
	case "Öğle Molası":
		r.HabitSentence = "Günün ortasında kendine nefes alanı açıp bölümleri eritiyorsun (" + r.PeakHourRange + ")."
	default:
		r.HabitSentence = "Günün yorgunluğunu akşamları (" + r.PeakHourRange + ") ekrana kurulup atıyorsun."
	}
	if r.StreakDays >= 5 {
		r.HabitSentence += fmt.Sprintf(" Üst üste %d gün ara vermeden izleme serisi yakaladın!", r.StreakDays)
	}

	// 3. Genre sentence.
	if len(r.TopGenres) >= 2 {
		r.GenreSentence = fmt.Sprintf("%s ve %s senin güvenli limanın; kitaplığının kalbi bu ikilide atıyor.", r.TopGenres[0].Name, r.TopGenres[1].Name)
	} else if len(r.TopGenres) == 1 {
		r.GenreSentence = fmt.Sprintf("Gözün %s türünden başkasını görmüyor; ne aradığını iyi biliyorsun.", r.TopGenres[0].Name)
	} else {
		r.GenreSentence = "Henüz tür profili oluşacak kadar seri birikmedi; Keşfet'ten birkaç seri ekleyince burası renklenir."
	}

	// 4. Show sentence.
	if len(r.TopShows) > 0 {
		top := r.TopShows[0]
		if top.Score >= 9 {
			r.ShowSentence = fmt.Sprintf("Zirvede tartışmasız %s var — %d/10 puanla gönlüne taht kurmuş.", top.Title, top.Score)
		} else if top.Watched >= 12 {
			r.ShowSentence = fmt.Sprintf("%s ile uzun bir yol yürüdün (%d bölüm); bırakması zor bir seri olmuş.", top.Title, top.Watched)
		} else {
			r.ShowSentence = fmt.Sprintf("En çok vakit ayırdığın yapım %s oldu.", top.Title)
		}
	} else {
		r.ShowSentence = "İlk favori serin seçilmeyi bekliyor."
	}

	// 5. Archetype & multi-sentence persona story.
	topG := ""
	if len(r.TopGenres) > 0 {
		topG = strings.ToLower(r.TopGenres[0].Name)
	}
	switch {
	case r.PeakPeriod == "Gece Kuşu" && (strings.Contains(topG, "dram") || strings.Contains(topG, "gizem") || strings.Contains(topG, "psikoloj") || strings.Contains(topG, "mystery")):
		r.Archetype, r.ArchetypeBadge = "Gece Yarısı Filozofu", "☾ FİLOZOF"
		r.Tagline = "Karanlıkta derin hikâyeler çözen sessiz izleyici"
	case strings.Contains(topG, "aksiyon") || strings.Contains(topG, "action") || strings.Contains(topG, "macera") || strings.Contains(topG, "adventure"):
		r.Archetype, r.ArchetypeBadge = "Shounen Maratoncusu", "⚡ MARATONCU"
		r.Tagline = "Tempo düşmeden bölümleri peş peşe deviren adrenalin avcısı"
	case strings.Contains(topG, "komedi") || strings.Contains(topG, "comedy") || strings.Contains(topG, "slice") || strings.Contains(topG, "yaşam"):
		r.Archetype, r.ArchetypeBadge = "Huzur Gezgini", "☕ HUZUR"
		r.Tagline = "Stresi kapıda bırakıp sıcak hikâyelerde dinlenen izleyici"
	case r.RatedCount >= 5 && r.AvgScore <= 6.8:
		r.Archetype, r.ArchetypeBadge = "Keskin Eleştirmen", "◈ ELEŞTİRMEN"
		r.Tagline = "Kolay beğenmeyen, 9 puanı gerçekten hak edene veren seçici göz"
	case r.CompletionRate >= 65 && r.CompletedCount >= 4:
		r.Archetype, r.ArchetypeBadge = "Kusursuz Bitirici", "★ BİTİRİCİ"
		r.Tagline = "Başladığı hikâyeyi final jeneriğine kadar bırakmayan disiplinli takipçi"
	case r.TotalSeries >= 15 && r.CompletionRate < 35:
		r.Archetype, r.ArchetypeBadge = "Meraklı Kaşif", "✦ KAŞİF"
		r.Tagline = "Her sezondan tat almayı seven, listesi hep canlı duran kâşif"
	default:
		r.Archetype, r.ArchetypeBadge = "Sezon Yolcusu", "◆ YOLCU"
		r.Tagline = "Kendi ritminde, reklam ve gürültüden uzak saf anime keyfi"
	}

	var story []string
	story = append(story, r.TimeComparison)
	if r.RatedCount >= 3 {
		switch {
		case r.AvgScore >= 8.3:
			story = append(story, fmt.Sprintf("Puan ortalaman %.1f/10 — sevdiğin serileri seçmekte nokta atışı yapıyorsun.", r.AvgScore))
		case r.AvgScore <= 6.5:
			story = append(story, fmt.Sprintf("Puan ortalaman %.1f/10 — çıtan yüksek, sıradan yapımlara kolay kolay geçit vermiyorsun.", r.AvgScore))
		default:
			story = append(story, fmt.Sprintf("Puan ortalaman %.1f/10 ile dengeli ve adil bir değerlendirme çizgin var.", r.AvgScore))
		}
	}
	if r.TotalSeries >= 3 {
		if r.CompletionRate >= 60 {
			story = append(story, fmt.Sprintf("Başladığın serilerin %%%d'ini bitirmişsin — yarım bırakmak sana göre değil.", r.CompletionRate))
		} else {
			story = append(story, fmt.Sprintf("Kitaplığında %d seri birikmiş; %d tanesini tamamlamış, geri kalanlarda keşif halindesin.", r.TotalSeries, r.CompletedCount))
		}
	}
	if len(r.TopShows) > 0 {
		story = append(story, fmt.Sprintf("Eğer bu yılı tek bir seri özetleseydi, o kesinlikle '%s' olurdu.", r.TopShows[0].Title))
	}
	r.PersonaStory = story
}
