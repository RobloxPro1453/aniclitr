// Package doctor runs a live end-to-end check of every source and dependency.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"aniclitr/internal/diag"
	"aniclitr/internal/source"
)

// Blame prints the accumulated provider/extractor blame report.
func Blame() {
	fmt.Println(diag.FormatReport())
}

// Run probes each provider with query: search → details → episodes → variants → resolve.
func Run(query string, legacy bool) {
	if query == "--blame" || query == "-blame" || query == "blame" {
		Blame()
		return
	}
	fmt.Println("aniclitr doctor —", time.Now().Format("2006-01-02 15:04"))
	for _, bin := range []string{"mpv", "yt-dlp"} {
		if p, err := exec.LookPath(bin); err == nil {
			fmt.Printf("  ✓ %-7s %s\n", bin, p)
		} else {
			fmt.Printf("  ✗ %-7s bulunamadı\n", bin)
		}
	}
	for _, p := range source.All() {
		info := p.Info()
		if info.Legacy && !legacy {
			continue
		}
		q := query
		if info.Lang == "en" && q == "frieren" {
			q = "frieren"
		}
		fmt.Printf("\n▌%s (%s)\n", info.Name, info.ID)
		probe(p, q)
	}
	diag.Flush()
}

func recordStep(provider, op string, start time.Time, err error, empty bool) {
	d := time.Since(start)
	out := diag.OK
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		out = diag.Timeout
	case errors.Is(err, source.ErrNotFound) || (err == nil && empty):
		out = diag.Empty
	case err != nil:
		out = diag.Error
	}
	diag.Record(provider, op, d, out)
}

func step(name string, start time.Time, err error, detail string) bool {
	d := time.Since(start).Round(time.Millisecond)
	if err != nil {
		fmt.Printf("  ✗ %-9s %6s  %v\n", name, d, err)
		return false
	}
	fmt.Printf("  ✓ %-9s %6s  %s\n", name, d, detail)
	return true
}

func probe(p source.Provider, q string) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	pid := p.Info().ID

	if f, ok := p.(source.Feeder); ok {
		t := time.Now()
		items, err := f.Latest(ctx)
		recordStep(pid, "feed", t, err, len(items) == 0)
		detail := ""
		if len(items) > 0 {
			detail = fmt.Sprintf("%d öğe, en yeni: %s %d. bölüm", len(items), items[0].Anime.Title, items[0].Episode)
		}
		step("akış", t, err, detail)
	}
	t := time.Now()
	res, err := p.Search(ctx, q)
	recordStep(pid, "search", t, err, len(res) == 0)
	if err == nil && len(res) == 0 {
		err = source.ErrNotFound
	}
	if !step("arama", t, err, fmt.Sprintf("%d sonuç, ilk: %s", len(res), first(res))) {
		return
	}
	t = time.Now()
	a, err := p.Details(ctx, res[0])
	recordStep(pid, "details", t, err, a.ID == "")
	if !step("detay", t, err, fmt.Sprintf("%s · %d sezon · MAL %d · %s", a.Kind, len(a.Seasons), a.MalID, strings.Join(a.Genres, ", "))) {
		return
	}
	season := 1
	if len(a.Seasons) > 0 {
		season = a.Seasons[0].Number
	}
	t = time.Now()
	eps, err := p.Episodes(ctx, a, season)
	recordStep(pid, "episodes", t, err, len(eps) == 0)
	if !step("bölümler", t, err, fmt.Sprintf("%d bölüm, ilk: %q", len(eps), epTitle(eps))) {
		return
	}
	t = time.Now()
	vs, err := p.Variants(ctx, a, eps[0])
	recordStep(pid, "variants", t, err, len(vs) == 0)
	fansubs := map[string]bool{}
	for _, v := range vs {
		fansubs[v.Fansub] = true
	}
	if !step("varyant", t, err, fmt.Sprintf("%d seçenek, %d fansub", len(vs), len(fansubs))) {
		return
	}
	for i, v := range vs {
		if i >= 4 {
			break
		}
		t = time.Now()
		st, err := p.Resolve(ctx, v)
		recordStep(pid, "resolve", t, err, st == nil || len(st.Qualities) == 0)
		detail := ""
		if st != nil && len(st.Qualities) > 0 {
			var qs []string
			for _, q := range st.Qualities {
				qs = append(qs, q.DisplayLabel())
			}
			detail = fmt.Sprintf("%s · %s → %s", v.Host, strings.Join(qs, "/"), trunc(diag.RedactURL(st.Qualities[0].URL), 60))
		} else {
			err = fmt.Errorf("%s: %v", v.Host, err)
		}
		step("çözüm", t, err, detail)
	}
}

func first(r []source.Anime) string {
	if len(r) == 0 {
		return "-"
	}
	return r[0].Title
}

func epTitle(e []source.Episode) string {
	if len(e) == 0 {
		return ""
	}
	return e[0].Title
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
