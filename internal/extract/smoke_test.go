package extract

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func loadSamples(t *testing.T) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "samples.json"))
	if err != nil {
		t.Fatalf("samples.json okunamadı: %v", err)
	}
	var m map[string][]string
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("samples.json geçersiz JSON: %v", err)
	}
	return m
}

func TestSmokeCatalogValid(t *testing.T) {
	samples := loadSamples(t)
	if len(samples) == 0 {
		t.Fatal("smoke kataloğu boş")
	}
	byName := map[string]Extractor{}
	for _, e := range All() {
		byName[e.Name()] = e
	}
	for name, urls := range samples {
		e, ok := byName[name]
		if !ok {
			t.Errorf("samples.json içindeki %q için kayıtlı çözümleyici yok", name)
			continue
		}
		if len(urls) == 0 {
			t.Errorf("%q için örnek bağlantı listesi boş", name)
		}
		for _, raw := range urls {
			u, err := url.Parse(NormalizeURL(raw))
			if err != nil {
				t.Errorf("%s: geçersiz URL %q: %v", name, raw, err)
				continue
			}
			if !e.CanHandle(u) {
				t.Errorf("%s çözümleyicisi kendi örnek adresini tanımadı: %q", name, raw)
			}
		}
	}
}

func TestSmoke(t *testing.T) {
	if os.Getenv("ANICLITR_SMOKE") == "" {
		t.Skip("canlı smoke testi için ANICLITR_SMOKE=1 ayarlayın")
	}
	samples := loadSamples(t)
	names := make([]string, 0, len(samples))
	for k := range samples {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, name := range names {
		urls := samples[name]
		t.Run(name, func(t *testing.T) {
			var fails []string
			for _, raw := range urls {
				ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
				st, f := ResolveDetailed(ctx, raw, nil)
				cancel()
				if st != nil && len(st.Qualities) > 0 {
					t.Logf("✓ %s → %d kalite (ilk: %s)", raw, len(st.Qualities), st.Qualities[0].Label)
					return
				}
				fails = append(fails, fmt.Sprintf("%s: %v", raw, f))
			}
			t.Fatalf("%s: %d örnek bağlantının tamamı başarısız oldu:\n  %s",
				name, len(urls), strings.Join(fails, "\n  "))
		})
	}
}
