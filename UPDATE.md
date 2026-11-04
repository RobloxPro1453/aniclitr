# Güncellemeler ve sürümler

Bu belge iki şeyi anlatır:
- aniclitr'in kendini nasıl güncel tuttuğu (herkes için);
- yeni bir sürümün nasıl yayınlandığı (projeye katkı verenler için).

---

## Kullanıcılar için

### Kurulum

1. [Releases](https://github.com/RobloxPro1453/aniclitr/releases/latest) sayfasından kendi sisteminin dosyasını indir:

   | Sistem | Dosya |
   |---|---|
   | Windows (çoğu bilgisayar) | `aniclitr_windows_amd64.exe` |
   | Windows ARM | `aniclitr_windows_arm64.exe` |
   | Linux | `aniclitr_linux_amd64` / `aniclitr_linux_arm64` |
   | macOS (Apple Silicon / Intel) | `aniclitr_darwin_arm64` / `aniclitr_darwin_amd64` |

2. Dosyayı çalıştır. İlk açılıştaki tanıtım turunun **terminal kısayolu** adımında `y`'ye basarsan aniclitr kurulur ve PATH'e eklenir. Yönetici izni gerekmez.
3. O andan sonra her yeni terminalde `aniclitr` yazman yeter.

Kurulum yeri:
- Windows: `%LOCALAPPDATA%\Programs\aniclitr\`
- Linux / macOS: `~/.local/share/aniclitr/`, bağlantısı `~/.local/bin/aniclitr`

Sonradan kurmak ya da kaldırmak için: **Ayarlar → Terminalden aç**.

### Güncellemeler kendiliğinden gelir

1. aniclitr açıldıktan birkaç saniye sonra arka planda GitHub'daki son sürüme bakar.
2. Yeni sürüm varsa indirir ve `checksums.txt` içindeki **SHA-256** özetiyle doğrular. Özet tutmazsa hiçbir şey değişmez.
3. Yeni sürüm bir sonraki açılışta çalışır ve "yenilikler" kartı bir kez gösterilir.

| Ne istiyorsun | Nasıl |
|---|---|
| Otomatik güncellemeyi kapat / aç | Ayarlar → Otomatik güncelle |
| Şimdi denetle | Ayarlar → Güncellemeleri denetle, ya da `aniclitr update` |
| Sürümü öğren | `aniclitr --version` |

Kişisel veriler güncellemeden etkilenmez:
- Ayarlar, izleme geçmişi, kitaplık, MyAnimeList oturumu: `%APPDATA%\aniclitr\`.
- Kapak önbelleği: `%LOCALAPPDATA%\aniclitr\`.

### Sorun giderme

| Durum | Çözüm |
|---|---|
| `aniclitr update` "güncelleme deposu yok" diyor | Bu, kaynaktan derlenmiş bir kopyadır. Ayarlar → Terminalden aç'ı kapatıp yeniden aç: resmi sürüm kurulur. |
| "SHA-256 özeti tutmuyor" | İndirme bozulmuştur; hiçbir şey kurulmadı. Biraz sonra tekrar dene. |
| Bir önceki sürüme dönmek | Releases'tan o sürümün dosyasını indir, aniclitr kapalıyken kurulum klasöründeki exe'nin yerine koy ve Ayarlar → Otomatik güncelle'yi kapat. |

---

## Katkı verenler için: sürüm yayınlamak

Bir sürüm, `vX.Y.Z` etiketiyle yayınlanır. Etiket GitHub'a gelince [`release.yml`](.github/workflows/release.yml) iş akışı şunları yapar:
1. Windows'ta testleri çalıştırır.
2. 6 platform için derler.
3. `checksums.txt` dosyasını üretir.
4. Sürümü yayınlar.

### Adımlar

1. **Testler geçsin:**
   ```
   go vet ./cmd/... ./internal/...
   go test ./internal/...
   ```
2. **Sürüm numarasını seç** (X.Y.Z). Şimdiki sürüm `cmd/aniclitr/main.go` içindeki `var version` değeridir.
   - Hata düzeltmesi → Z
   - Yeni özellik → Y
   - Uyumsuz değişiklik → X

   Yeni numara her zaman öncekinden büyük olmalı.
3. **Yenilikleri yaz:** `internal/ui/onboard.go` → `whatsNew` haritasının en üstüne `"X.Y.Z": { … }` ekle. Bu satırlar hem uygulamadaki "yenilikler" kartında hem de GitHub sürüm notunda görünür.
   - 2–5 madde, Türkçe, kullanıcının anlayacağı dilde, en önemlisi önce.
   - Örnek: "Kapaklar daha az bellek harcıyor" (✓) · "poster cache LRU eklendi" (✗).
4. **Belgeleri güncelle** (gerekiyorsa): `README.md`, `TODO.md`, `ARCHITECTURE.md`.
5. **Commit et ve gönder:**
   ```
   git add -A
   git commit -m "…"
   git push
   ```
6. **Yayınla:**
   ```
   .\scripts\release.ps1 X.Y.Z
   ```
   Betik sırasıyla şunları yapar:
   - çalışma alanının temiz olduğunu ve numaranın geçerli olduğunu denetler;
   - testleri çalıştırır;
   - `var version`'ı yükseltir;
   - "yenilikler" satırlarını not olarak alıp commit ve etiket oluşturur;
   - ikisini de gönderir.
7. **İzle ve doğrula** (~5 dk):
   ```
   gh run watch -R RobloxPro1453/aniclitr
   aniclitr update
   ```
   Sürümde 7 dosya olmalı: 6 exe ve `checksums.txt`.

### Bir şey ters giderse

| Durum | Ne yapılır |
|---|---|
| Betik "commit edilmemiş değişiklikler var" diyor | 5. adımı yap, tekrar çalıştır |
| Betik "whatsNew yok" diyor | 3. adımı yap, commit et, tekrar çalıştır |
| Betik "etiket zaten var" diyor | Bir sonraki numarayı kullan |
| Actions'ta testler kırmızı | Sürüm yayınlanmaz. Düzelt, yeni numarayla tekrar yayınla. |
| Hatalı bir sürüm yayınlandı | Düzeltmeyi bir üst numarayla yayınla; güncelleyici yalnızca ileri gider. Sayfadan kaldırmak için: `gh release delete vX.Y.Z --yes` ve `git push --delete origin vX.Y.Z` |

### Teknik ayrıntılar

- **Güncelleyici** (`internal/update`): `api.github.com/repos/<repo>/releases/latest` adresine tokensız sorar, dosyayı indirip SHA-256 ile doğrular. Çalışan exe'yi `.old` olarak kenara alıp yenisini yerine koyar; Windows çalışan dosyanın yeniden adlandırılmasına izin verir. `.old` bir sonraki açılışta silinir.
- **Kurulum** (`internal/pathx`): exe'yi kurulum klasörüne kopyalar ve kullanıcı PATH'ine ekler. Ardından resmi sürümü indirip kopyanın yerine koyar; çalışan derleme sürümden yeniyse dokunmaz.
- Yalnızca **kurulu kopya** kendini günceller. Kaynaktan derlenen kopya (`bin\`) yalnızca yeni sürüm olduğunu bildirir.
- **Damgalar:** depo adı (`update.Repo`) ve sürüm (`main.version`) derleme sırasında `-ldflags -X` ile damgalanır. Depo adının kaynaktaki varsayılan değeri `RobloxPro1453/aniclitr`; depo taşınırsa bu değer de değiştirilmeli.
