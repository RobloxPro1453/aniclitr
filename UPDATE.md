# aniclitr — güncelleme ve sürüm rehberi

İki şeyi anlatır:
1. Kurulu aniclitr'in kendini nasıl güncel tuttuğunu.
2. Yeni bir sürümü nasıl yayınlayacağını: ne yazılır, ne gönderilir, hangi komut çalıştırılır.

Kısa yol için [§6 Hızlı başvuru](#6-hızlı-başvuru) bölümüne atla.

---

## 1. Büyük resim

```
 clipro (kaynak kod)            GitHub (RobloxPro1453/aniclitr)            senin bilgisayarın
 ───────────────────            ─────────────────────────────────          ──────────────────
 kodu yaz, test et
 whatsNew'i yaz, commit et
 .\scripts\release.ps1 X.Y.Z ─▶ vX.Y.Z etiketi gelir
                                GitHub Actions:
                                  1. Windows'ta testler
                                  2. 6 platform için derleme
                                  3. checksums.txt (SHA-256)
                                  4. Releases sayfasında yayın ─────▶ aniclitr açılır
                                                                       3 sn sonra GitHub'a bakar
                                                                       yeni sürüm → indir, doğrula
                                                                       bir sonraki açılışta yeni sürüm
```

**İki ayrı kopya var. Karıştırmamak için:**

| | Nerede | Kim kullanır | Kendini günceller mi |
|---|---|---|---|
| **Kurulu kopya** | `%LOCALAPPDATA%\Programs\aniclitr\aniclitr.exe` | Terminalde yazdığın `aniclitr`; PATH buraya bakar | **Evet**, her açılışta |
| **Geliştirme kopyası** | `clipro\bin\aniclitr.exe` | Kod yazarken denediğin derleme | Hayır, yalnızca "yeni sürüm var" der |

Yeni bir şey yazdığında kurulu kopyaya ulaşmasının tek yolu **yayınlamak**. `go build` yalnızca geliştirme kopyasını değiştirir. Bu bilerek böyle: elindeki yarım kod, her gün kullandığın uygulamaya karışmaz.

---

## 2. Kullanıcı tarafı: güncellemeler nasıl gelir

Her şey kendiliğinden olur:

1. aniclitr açılır. 3 saniye sonra (ilk ekran yüklensin diye) arka planda GitHub'a "son sürüm ne?" diye sorar.
2. Daha yeni bir sürüm varsa kendi platformunun exe'sini indirir (`aniclitr_windows_amd64.exe`).
3. `checksums.txt` içindeki SHA-256 özetiyle karşılaştırır. **Tutmazsa hiçbir şeye dokunmaz.**
4. Çalışan exe'yi `aniclitr.exe.old` olarak kenara alır, yenisini yerine koyar. Windows çalışan bir dosyanın yeniden adlandırılmasına izin verir, üzerine yazılmasına izin vermez; bu yüzden bu yöntem kullanılıyor.
5. Ekranda "aniclitr X.Y.Z indirildi · yeniden açınca geçerli" yazar, bildirimlere (`◆`) de düşer.
6. Bir sonraki açılışta yeni sürüm çalışır, `.old` silinir ve "yenilikler" kartı bir kez gösterilir.

**Ayarlar (`ctrl+o` → Genel):**
- **Otomatik güncelle:** açık / kapalı (varsayılan açık).
- **Güncellemeleri denetle:** şimdi bak.

**Komut satırı:**
```
aniclitr update      # şimdi denetle ve kur
aniclitr --version   # hangi sürümdesin
```

---

## 3. Yeni sürüm yayınlamak (adım adım)

### 3.1 Kodu bitir ve test et

```
go vet ./cmd/... ./internal/...
go test ./internal/...
go build -o bin\aniclitr.exe ./cmd/aniclitr     # geliştirme kopyasında dene
```

Testler kırmızıysa yayınlama. Betik de zaten durur.

### 3.2 Sürüm numarasını seç

Biçim **X.Y.Z** (semver). Şimdiki sürüm `cmd/aniclitr/main.go` içinde: `var version = "…"`.

| Ne değişti | Hangi hane | Örnek (0.11.0'dan) |
|---|---|---|
| Hata düzeltmesi, küçük cila | **Z** (yama) | 0.11.1 |
| Yeni özellik, yeni ekran, yeni kaynak | **Y** (küçük) | 0.12.0 |
| Ayar dosyası ya da veri biçimi uyumsuz değişti, büyük yeniden yazım | **X** (büyük) | 1.0.0 |

Yeni numara her zaman eskisinden büyük olmalı. Güncelleyici yalnızca büyüğe geçer.

### 3.3 "Yenilikler"i yaz: bu hem uygulamadaki kart hem de sürüm notu

`internal/ui/onboard.go` → `whatsNew` haritasının **en üstüne** ekle:

```go
var whatsNew = map[string][]string{
	"0.11.1": {
		"İndirmeler artık kaldığı yerden devam ediyor",
		"Kitaplık duvarında kapaklar daha hızlı açılıyor",
	},
	"0.11.0": {
		…
```

**Nasıl yazılır:**
- **Kullanıcıya** yaz, geliştiriciye değil. "poster cache LRU" değil, "kapaklar daha az bellek harcıyor".
- 2–5 madde. Her madde tek satır, Türkçe, nokta yok.
- Önce en çok fark edilecek şey.
- Tırnak gerekiyorsa `“ ”` kullan. Ters bölü yazacaksan Go dizesinde `\\` olarak yaz.

Bu girdi yoksa yayın betiği durur ve uyarır.

### 3.4 Belgeleri güncelle (gerekiyorsa)

- `README.md`: yeni tuş, ayar ya da özellik varsa.
- `TODO.md`: yapılanları işaretle.
- `ARCHITECTURE.md`: mimari bir kural ya da tuzak değiştiyse.

### 3.5 Commit et ve gönder

```
git add -A
git status            # neyin gideceğine bak (bkz. §4)
git commit -m "kısa açıklama"
git push
```

Yayın betiği **commit edilmemiş değişiklik varsa durur**; bu adım şart.

### 3.6 Yayınla

```
.\scripts\release.ps1 0.11.1
```

Betiğin yaptıkları, sırayla:
1. Sürümün geçerli ve şimdikinden büyük olduğunu, çalışma alanının temiz olduğunu ve etiketin daha önce kullanılmadığını denetler.
2. `whatsNew["0.11.1"]` satırlarını sürüm notu olarak alır.
3. `go vet` ve `go test` çalıştırır.
4. `main.go` içindeki `var version`'ı 0.11.1 yapar, geliştirme kopyasını derler.
5. "aniclitr 0.11.1" commit'ini ve `v0.11.1` etiketini (notlarla) oluşturur.
6. `git push` ve `git push origin v0.11.1` yapar.

### 3.7 GitHub'ın bitirmesini izle (~4–5 dk)

```
gh run watch -R RobloxPro1453/aniclitr
```

Ya da tarayıcıda: https://github.com/RobloxPro1453/aniclitr/actions

Bitince sürüm şurada olur: https://github.com/RobloxPro1453/aniclitr/releases

İçinde **7 dosya** olmalı: 6 exe (windows/linux/macOS × amd64/arm64) ve `checksums.txt`.

### 3.8 Doğrula

```
aniclitr update       # "indiriliyor: 0.11.1 … kuruldu" demeli
aniclitr --version    # yeniden aç: 0.11.1
```

Ya da aniclitr'i aç, birkaç saniye bekle, kapatıp yeniden aç.

---

## 4. Ne gönderilir, ne gönderilmez

`.gitignore` bunu zaten ayarlıyor; yine de `git status` ile göz at.

| Gider | Gitmez |
|---|---|
| `cmd/`, `internal/` (kaynak kod ve testler) | `bin/`, `dist/`, `*.exe` (derleme çıktıları; Actions kendisi derler) |
| `go.mod`, `go.sum` | `refs/` (başka projelerin kodu, yalnızca referans) |
| `README.md`, `ARCHITECTURE.md`, `UPDATE.md`, `TODO.md`, `OPTIMIZATION.md` | `.agents/`, `.taste-skill/`, `.claude/`, `skills-lock.json` (araçlar) |
| `.github/workflows/release.yml`, `scripts/release.ps1` | `mpv-shot*.jpg`, `*.ans`, `*.log` |

**Kişisel verin depoda hiç yok:**
- Ayarlar, geçmiş, kitaplık, MAL oturumu: `%APPDATA%\aniclitr\`.
- Kapak önbelleği: `%LOCALAPPDATA%\aniclitr\`.

İkisi de proje klasörünün dışında. MAL istemci kimliğin `config.json` içinde durur, kodda değil.

---

## 5. Bir şey ters giderse

| Durum | Ne yap |
|---|---|
| Betik "commit edilmemiş değişiklikler var" diyor | §3.5'i yap, sonra betiği tekrar çalıştır |
| Betik "whatsNew yok" diyor | §3.3'ü yap, commit et, tekrar çalıştır |
| Betik "etiket zaten var" diyor | O numara kullanılmış: bir sonrakini seç (0.11.2) |
| Actions'ta **test** adımı kırmızı | Sürüm yayınlanmaz, kullanıcılara bir şey gitmez. Hatayı düzelt, commit et, **yeni numarayla** tekrar yayınla. Bozuk etiketi silmek istersen aşağıya bak. |
| Yanlış ya da bozuk bir sürüm yayınladım | Hemen düzeltip bir üst numarayla yayınla (0.11.2). Güncelleyici yalnızca ileri gider; düzeltme herkese kendiliğinden ulaşır. |
| Bozuk sürümü sayfadan kaldırmak istiyorum | `gh release delete v0.11.1 -R RobloxPro1453/aniclitr --yes` ve `git push --delete origin v0.11.1` |
| Kurulu kopya güncellenmiyor | `aniclitr update` çalıştır, ne dediğine bak. "deposu yok" diyorsa eski bir derlemedir: Ayarlar → Terminalden aç'ı kapatıp aç; resmi sürüm yeniden kurulur. |
| Bir önceki sürüme dönmek istiyorum | Releases'tan o sürümün `aniclitr_windows_amd64.exe` dosyasını indir, uygulama kapalıyken `%LOCALAPPDATA%\Programs\aniclitr\aniclitr.exe` yerine koy. Ayarlar → Otomatik güncelle'yi kapat, yoksa bir sonraki açılışta tekrar yükselir. |
| "SHA-256 özeti tutmuyor" | İndirme bozulmuş ya da değiştirilmiş; hiçbir şey kurulmadı. Biraz sonra tekrar dene. Sürerse Releases sayfasındaki dosyaları kontrol et. |

---

## 6. Hızlı başvuru

```
# 1) kod + test
go test ./internal/...

# 2) internal/ui/onboard.go → whatsNew["X.Y.Z"] ekle

# 3) commit + push
git add -A
git commit -m "…"
git push

# 4) yayınla
.\scripts\release.ps1 X.Y.Z

# 5) izle + doğrula
gh run watch -R RobloxPro1453/aniclitr
aniclitr update
```

---

## 7. Arka plan (geliştirici için)

- **Güncelleyici:** `internal/update`.
  - `Latest`: `GET api.github.com/repos/<Repo>/releases/latest`; tokensız çalışır, bu yüzden depo herkese açık olmalı.
  - `Install`: indir, SHA-256 doğrula, `.old` ile yer değiştir.
  - `Cleanup`: açılışta `.old` dosyasını siler.
- **Kurulum:** `internal/pathx`.
  - `Add`: çalışan exe'yi kurulum klasörüne kopyalar, o klasörü kullanıcı PATH'ine (`HKCU\Environment`) yazar, eski derleme klasörünü PATH'ten çıkarır.
  - `ui.officialInstall`: ardından kopyalanan derlemeyi resmi sürümle değiştirir, böylece kurulu kopya her zaman güncellemeleri bilen bir derleme olur. Çalışan derleme sürümden yeniyse dokunmaz.
- **Depo adı:**
  - `update.Repo` kaynakta varsayılan olarak `RobloxPro1453/aniclitr`.
  - Actions da derlerken `-ldflags "-X aniclitr/internal/update.Repo=$GITHUB_REPOSITORY"` ile damgalar.
  - Depo taşınırsa ya da adı değişirse **varsayılanı da değiştir**.
- **Sürüm numarası:** `main.version`. Actions etiketten damgalar (`-X main.version=…`); betik de kaynakta yükseltir, ikisi hep aynıdır.
- **İş akışı:** `.github/workflows/release.yml`. Windows'ta test, Ubuntu'da 6 platforma çapraz derleme (`CGO_ENABLED=0`), `sha256sum`, `gh release create`.
