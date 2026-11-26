package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"aniclitr/internal/config"
	"aniclitr/internal/extract"
)

// RunLab executes developer-only resolver lab subcommands (`aniclitr lab ...`).
// It is intentionally CLI/developer-only and never exposed in the end-user TUI.
func RunLab(ctx context.Context, w io.Writer, args []string) int {
	if len(args) == 0 {
		printLabUsage(w)
		return 0
	}
	switch args[0] {
	case "list", "liste":
		return LabList(w)
	case "validate", "dogrula", "doğrula":
		return LabValidate(w)
	case "test", "probe":
		if len(args) < 2 {
			fmt.Fprintln(w, "kullanım: aniclitr lab test <çözücü-id-veya-url> [url] [--fixture dosya.html]")
			return 2
		}
		var target, rawURL, fixture string
		for i := 1; i < len(args); i++ {
			if args[i] == "--fixture" && i+1 < len(args) {
				fixture = args[i+1]
				i++
				continue
			}
			if target == "" {
				target = args[i]
			} else if rawURL == "" {
				rawURL = args[i]
			}
		}
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			rawURL = target
			target = ""
		}
		return LabTest(ctx, w, target, rawURL, fixture)
	case "scaffold", "yeni":
		if len(args) < 3 {
			fmt.Fprintln(w, "kullanım: aniclitr lab scaffold <id> <domain>")
			return 2
		}
		return LabScaffold(w, args[1], args[2])
	default:
		printLabUsage(w)
		return 2
	}
}

func printLabUsage(w io.Writer) {
	fmt.Fprintln(w, "aniclitr lab — Geliştirici Çözücü & Eklenti Teşhis Laboratuvarı")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Komutlar:")
	fmt.Fprintln(w, "  aniclitr lab list                              Yüklü yerleşik + harici çözücüleri listele")
	fmt.Fprintln(w, "  aniclitr lab validate                          Tüm sıfır-derleme JSON eklenti paketlerini doğrula")
	fmt.Fprintln(w, "  aniclitr lab test <url> [--fixture f.html]     Tek bir çözücüyü izole test et ve kırılma teşhisi al")
	fmt.Fprintln(w, "  aniclitr lab test <id> <url> [--fixture ...]   Belirli eklenti kimliğiyle izole test koştur")
	fmt.Fprintln(w, "  aniclitr lab scaffold <id> <domain>            Harici eklenti klasöründe şablon JSON üret")
	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "Harici eklenti klasörü (derlemesiz güncelleme): %s\n", Dir())
}

// LabList prints all loaded zero-compile packs and native extractors.
func LabList(w io.Writer) int {
	packs, errs := LoadAll()
	fmt.Fprintf(w, "Sıfır-derleme çözücü paketleri (%d adet) · Harici klasör: %s\n\n", len(packs), Dir())
	for _, p := range packs {
		src := "yerleşik"
		if p.Manifest.External {
			src = "harici (" + filepath.Base(p.Manifest.SourcePath) + ")"
		}
		doms := strings.Join(p.Manifest.Domains, ", ")
		if len(doms) > 56 {
			doms = doms[:53] + "..."
		}
		fmt.Fprintf(w, "  %-14s %-16s v%-6s [%-10s] %s\n", p.Manifest.ID, p.Manifest.Name, p.Manifest.Version, src, doms)
	}
	if len(errs) > 0 {
		fmt.Fprintln(w, "\nYükleme uyarıları:")
		for _, err := range errs {
			fmt.Fprintln(w, "  !", err)
		}
	}
	fmt.Fprintf(w, "\nToplam kayıtlı extract.Extractor: %d\n", len(extract.All()))
	return 0
}

// LabValidate checks every built-in and external plugin manifest.
func LabValidate(w io.Writer) int {
	packs, errs := LoadAll()
	ok := 0
	for _, p := range packs {
		if err := p.Manifest.Validate(); err != nil {
			errs = append(errs, err)
			continue
		}
		ok++
	}
	fmt.Fprintf(w, "Doğrulanan eklenti: %d  ·  Hata: %d\n", ok, len(errs))
	for _, err := range errs {
		fmt.Fprintln(w, "  ✗", err)
	}
	if len(errs) > 0 {
		return 1
	}
	fmt.Fprintln(w, "  ✓ Tüm eklenti paketleri geçerli.")
	return 0
}

// LabTest runs one resolver in isolation and emits step-by-step diagnostics.
func LabTest(ctx context.Context, w io.Writer, pluginID, rawURL, fixturePath string) int {
	_, _ = LoadAll()
	if rawURL == "" {
		fmt.Fprintln(w, "✗ Test edilecek URL gerekli.")
		return 2
	}
	u, err := url.Parse(extract.NormalizeURL(rawURL))
	if err != nil || u.Host == "" {
		fmt.Fprintf(w, "✗ Geçersiz URL: %q\n", rawURL)
		return 2
	}
	var fixtureBody string
	if fixturePath != "" {
		b, err := os.ReadFile(fixturePath)
		if err != nil {
			fmt.Fprintf(w, "✗ Fixture okunamadı (%s): %v\n", fixturePath, err)
			return 2
		}
		fixtureBody = string(b)
	}

	var target *Pack
	if pluginID != "" {
		target = ByID(pluginID)
		if target == nil {
			fmt.Fprintf(w, "✗ Eklenti bulunamadı: %q\n", pluginID)
			return 1
		}
	} else {
		for _, p := range All() {
			if p.CanHandle(u) {
				target = p
				break
			}
		}
	}
	if target == nil {
		fmt.Fprintf(w, "✗ Bu URL için eşleşen sıfır-derleme eklenti yok: %s\n", u.Host)
		return 1
	}

	rep := target.ResolveTrace(ctx, u, nil, nil, fixtureBody)
	fmt.Fprintf(w, "Eklenti : %s (%s v%s)\n", target.Manifest.Name, target.Manifest.ID, target.Manifest.Version)
	fmt.Fprintf(w, "URL     : %s\n", rep.URL)
	fmt.Fprintf(w, "Süre    : %s\n", rep.Duration.Round(1e6))
	if rep.RedirectFrom != "" {
		fmt.Fprintf(w, "Yönlendirme: %s → %s\n", rep.RedirectFrom, rep.RedirectTo)
	}
	fmt.Fprintln(w, "\nAdım izi:")
	for _, tr := range rep.Traces {
		mark := "✓"
		if tr.Skipped {
			mark = "·"
		} else if !tr.OK {
			mark = "✗"
		}
		fmt.Fprintf(w, "  %s #%d %-14s (%4dms) %s\n", mark, tr.Index, tr.Op, tr.Duration.Milliseconds(), tr.Note)
	}
	if rep.Stream != nil && len(rep.Stream.Qualities) > 0 {
		fmt.Fprintf(w, "\n✓ ÇÖZÜLDÜ (%d kalite):\n", len(rep.Stream.Qualities))
		for _, q := range rep.Stream.Qualities {
			kind := "MP4"
			if strings.Contains(strings.ToLower(q.URL), ".m3u8") || q.HLSBandwidth > 0 {
				kind = "HLS"
			}
			fmt.Fprintf(w, "  - %-6s [%s] %s\n", q.Label, kind, q.URL)
		}
		if rep.Stream.Referer != "" {
			fmt.Fprintf(w, "  Referer: %s\n", rep.Stream.Referer)
		}
		return 0
	}
	fmt.Fprintf(w, "\n✗ KIRILMA TEŞHİSİ [%s]: %s\n", rep.Kind.String(), rep.Diagnosis)
	return 1
}

// LabScaffold writes a starter zero-compile plugin JSON into Dir().
func LabScaffold(w io.Writer, id, domain string) int {
	id = strings.ToLower(strings.TrimSpace(id))
	domain = strings.ToLower(strings.TrimSpace(domain))
	m := Manifest{
		Schema:  1,
		ID:      id,
		Name:    strings.ToUpper(id[:1]) + id[1:],
		Version: "1.0.0",
		Note:    "Özel sıfır-derleme çözücü paketi",
		Domains: []string{domain},
		Steps: []Step{
			{Op: "http_get", URL: "{{url}}", Headers: map[string]string{"Referer": "{{origin}}/"}},
			{Op: "js_unpack", Optional: true},
			{Op: "emit_regex_all", From: "body", Pattern: `(https?://[^"'\s]+\.(?:m3u8|mp4)[^"'\s]*)`, URLGroup: 1, Referer: "{{origin}}/"},
		},
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintln(w, "✗ Şablon oluşturulamadı:", err)
		return 1
	}
	dir := Dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(w, "✗ Klasör oluşturulamadı:", err)
		return 1
	}
	dst := filepath.Join(dir, id+".json")
	if err := config.WriteAtomic(dst, append(raw, '\n')); err != nil {
		fmt.Fprintln(w, "✗ Dosya yazılamadı:", err)
		return 1
	}
	fmt.Fprintf(w, "✓ Eklenti şablonu oluşturuldu: %s\n", dst)
	fmt.Fprintln(w, "  Derleme gerekmez — düzenleyip 'aniclitr lab validate' ile doğrulayabilirsin.")
	return 0
}
