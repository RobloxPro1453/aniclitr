# Geliştirme ve yayın akışı

Bu belge, projede çalışan herkes (insan ya da yapay zekâ asistanı) için başlangıç noktasıdır. Yeni bir oturumda önce bunu okuyun.

## Temel bilgiler

| | |
|---|---|
| Depo | https://github.com/RobloxPro1453/aniclitr (herkese açık) |
| Ana dal | `main` |
| Sürümler | https://github.com/RobloxPro1453/aniclitr/releases |
| Geliştirme klasörü | `clipro\`: kaynak kod. Buradaki derleme (`bin\aniclitr.exe`) **geliştirme kopyasıdır**. |
| Kurulu kopya | `%LOCALAPPDATA%\Programs\aniclitr\aniclitr.exe`: terminalde `aniclitr` yazınca açılan, kendini güncelleyen **resmi sürüm**. |
| Şimdiki sürüm | `cmd/aniclitr/main.go` → `var version` |
| GitHub CLI | `gh` (Windows: `C:\Program Files\GitHub CLI\gh.exe`), `gh auth login` ile giriş yapılmış olmalı |

## İki kopya kuralı

- `go build` yalnızca **geliştirme kopyasını** (`bin\`) değiştirir. Kurulu kopyaya dokunmaz.
- Kurulu kopyaya bir değişikliğin ulaşmasının **tek yolu bir sürüm yayınlamaktır.** Kurulu kopya açılışta GitHub'daki son sürümü bulur, indirir, doğrular ve bir sonraki açılışta onu çalıştırır.
- Kurulu kopyanın yerine elle bir geliştirme derlemesi kopyalanmaz. Kopyalanan derleme güncelleme deposunu bilmeyebilir; o durumda kopya kendini güncelleyemez.

## Günlük çalışma

```
go vet ./cmd/... ./internal/...
go test ./internal/...
go build -o bin\aniclitr.exe ./cmd/aniclitr
bin\aniclitr.exe
```

## GitHub'a gönderme

**Gönderilenler:** `cmd/`, `internal/`, `go.mod`, `go.sum`, `*.md`, `.github/`, `scripts/`.

**Gönderilmeyenler** (`.gitignore` ile dışarıda):

| Kalem | Neden |
|---|---|
| `bin/`, `dist/`, `*.exe` | Derleme çıktıları; sürüm exe'lerini GitHub Actions derler |
| `refs/` | Başka projelerin referans kodu |
| `.agents/`, `.taste-skill/`, `.claude/`, `skills-lock.json` | Araç klasörleri |
| `mpv-shot*.jpg`, `*.ans`, `*.log` | Geçici dosyalar |

Kullanıcı verileri (`%APPDATA%\aniclitr\`, `%LOCALAPPDATA%\aniclitr\`) proje klasörünün dışındadır ve hiçbir zaman depoya girmez.

**Kod değişikliğini göndermek** (sürüm yayınlamaz, kullanıcılara bir şey gitmez):
```
git add -A
git status            # yalnızca beklenen dosyalar olmalı
git commit -m "kısa açıklama"
git push
```

## Sürüm yayınlamak (kullanıcılara ulaştırmak)

1. Testler geçmeli.
2. `internal/ui/onboard.go` → `whatsNew` haritasının en üstüne `"X.Y.Z": { "…", "…" }` ekle. Bu satırlar hem uygulamadaki "yenilikler" kartı hem de sürüm notudur. Kullanıcıya yönelik, kısa ve Türkçe yazılır.
3. Değişiklikleri commit edip gönder (yukarıdaki adım).
4. Yayınla:
   ```
   .\scripts\release.ps1 X.Y.Z
   ```
   Betik testleri çalıştırır, sürüm numarasını yükseltir, commit ve `vX.Y.Z` etiketini oluşturup gönderir.
5. GitHub Actions ~5 dakikada 6 platform için derleyip sürümü yayınlar:
   ```
   gh run watch -R RobloxPro1453/aniclitr
   ```
6. Doğrula: `aniclitr update`. Yeni sürümü indirmeli; bir sonraki açılışta `aniclitr --version` yeni numarayı göstermeli.

Sürüm numarası kuralları, sorun giderme ve teknik ayrıntılar: [UPDATE.md](UPDATE.md).

## Diğer belgeler

| Belge | İçerik |
|---|---|
| [README.md](README.md) | Kullanıcı kılavuzu |
| [ARCHITECTURE.md](ARCHITECTURE.md) | Mimari, kurallar ve bilinen tuzaklar. Kod değiştirmeden önce okunmalı. |
| [UPDATE.md](UPDATE.md) | Güncelleme sistemi ve yayın ayrıntıları |
| [OPTIMIZATION.md](OPTIMIZATION.md) | Bellek ve kaynak kullanımı raporu |
| [TODO.md](TODO.md) | Yapılacaklar |
