// Package cli implements fast non-TUI subcommands (search, continue list,
// portable backup/restore) and parses startup intents for the TUI.
package cli

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"aniclitr/internal/backup"
	"aniclitr/internal/config"
	"aniclitr/internal/source"
	"aniclitr/internal/store"
)

// SearchHit groups matching anime found across one or more enabled sources.
type SearchHit struct {
	Title   string
	Year    int
	Kind    string
	Sources []string
	Sample  source.Anime
}

// Search queries all enabled sources in parallel and merges duplicate titles.
func Search(ctx context.Context, cfg config.Config, query string) []SearchHit {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil
	}
	var provs []source.Provider
	for _, p := range source.All() {
		in := p.Info()
		if cfg.Enabled(in.ID, in.Legacy) {
			provs = append(provs, p)
		}
	}
	type res struct {
		src   string
		items []source.Anime
	}
	ch := make(chan res, len(provs))
	var wg sync.WaitGroup
	for _, p := range provs {
		wg.Add(1)
		go func(prov source.Provider) {
			defer wg.Done()
			c, cancel := context.WithTimeout(ctx, 12*time.Second)
			defer cancel()
			items, err := prov.Search(c, query)
			if err != nil {
				return
			}
			ch <- res{src: prov.Info().Name, items: items}
		}(p)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()

	byKey := map[string]*SearchHit{}
	var order []string
	for r := range ch {
		for _, an := range r.items {
			k := strings.ToLower(strings.TrimSpace(an.Title))
			if an.MalID > 0 {
				k = fmt.Sprintf("mal:%d", an.MalID)
			}
			h, ok := byKey[k]
			if !ok {
				h = &SearchHit{Title: an.Title, Year: an.Year, Kind: an.Kind, Sample: an}
				byKey[k] = h
				order = append(order, k)
			}
			dup := false
			for _, s := range h.Sources {
				if s == r.src {
					dup = true
					break
				}
			}
			if !dup {
				h.Sources = append(h.Sources, r.src)
			}
			if h.Year == 0 && an.Year > 0 {
				h.Year = an.Year
			}
		}
	}
	out := make([]SearchHit, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if len(out[i].Sources) != len(out[j].Sources) {
			return len(out[i].Sources) > len(out[j].Sources)
		}
		return out[i].Year > out[j].Year
	})
	return out
}

// PrintSearch formats quick CLI search results cleanly to w.
func PrintSearch(w io.Writer, query string, hits []SearchHit) {
	if len(hits) == 0 {
		fmt.Fprintf(w, "Sonuç bulunamadı: %q\n", query)
		return
	}
	limit := min(20, len(hits))
	fmt.Fprintf(w, "Arama: %q (%d sonuç)\n\n", query, len(hits))
	for i := 0; i < limit; i++ {
		h := hits[i]
		meta := h.Kind
		if h.Year > 0 {
			if meta != "" {
				meta += " · "
			}
			meta += strconv.Itoa(h.Year)
		}
		fmt.Fprintf(w, "  %2d. %-42s  %-12s  [%s]\n", i+1, cutRunes(h.Title, 42), meta, strings.Join(h.Sources, ", "))
	}
	fmt.Fprintf(w, "\nİzlemek için: aniclitr izle %q [bölüm]\n", query)
}

// PrintContinue prints recent watch history entries to w without launching TUI.
func PrintContinue(w io.Writer, hist *store.History) {
	recent := hist.Recent()
	if len(recent) > 10 {
		recent = recent[:10]
	}
	if len(recent) == 0 {
		fmt.Fprintln(w, "Henüz izleme geçmişi yok. Başlamak için: aniclitr ara <anime>")
		return
	}
	fmt.Fprintln(w, "Kaldığın yerden devam et:")
	fmt.Fprintln(w)
	for i, e := range recent {
		pct := e.Percent()
		qual := ""
		if e.Quality != "" {
			qual = " · " + e.Quality
		}
		fmt.Fprintf(w, "  %2d. %-36s  S%d·B%s  (%%%d%s)\n", i+1, cutRunes(e.Title, 36), e.Season, source.EpNum(e.Episode), pct, qual)
	}
	fmt.Fprintln(w, "\nHemen devam etmek için: aniclitr devam")
}

// RunBackup executes `aniclitr yedekle [dosya.json]`.
func RunBackup(w io.Writer, dst, appVersion string) error {
	sum, err := backup.Export(dst, appVersion)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Yedek oluşturuldu: %s\n", sum.Path)
	fmt.Fprintf(w, "Dahil edilen dosyalar (%d): %s\n", len(sum.Files), strings.Join(sum.Files, ", "))
	return nil
}

// RunRestore executes `aniclitr geri-yukle <dosya.json>`.
func RunRestore(w io.Writer, src string) error {
	if strings.TrimSpace(src) == "" {
		return fmt.Errorf("kullanım: aniclitr geri-yukle <yedek-dosyası.json>")
	}
	sum, err := backup.Import(src)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Yedek geri yüklendi: %s\n", strings.Join(sum.Files, ", "))
	if sum.BackupDir != "" {
		fmt.Fprintf(w, "Önceki veriler güvenlik için yedeklendi: %s\n", sum.BackupDir)
	}
	return nil
}

// ParseWatchArgs parses `aniclitr izle <anime adı...> [bölüm]`.
// If the last token is a positive integer, it is extracted as the episode number.
func ParseWatchArgs(args []string) (query string, ep int) {
	if len(args) == 0 {
		return "", 0
	}
	if len(args) >= 2 {
		if n, err := strconv.Atoi(args[len(args)-1]); err == nil && n > 0 {
			return strings.TrimSpace(strings.Join(args[:len(args)-1], " ")), n
		}
	}
	return strings.TrimSpace(strings.Join(args, " ")), 0
}

func cutRunes(s string, maxR int) string {
	rs := []rune(s)
	if len(rs) <= maxR {
		return s
	}
	return string(rs[:max(1, maxR-1)]) + "…"
}
