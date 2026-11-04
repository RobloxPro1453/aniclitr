# aniclitr

**Terminalde, Türkçe altyazılı anime.** Ara, fansubunu seç, izle. Kaldığın yer, sıradaki bölüm, açılış ve bitiş atlama kendiliğinden. İnternetsiz izlemek için indir, MyAnimeList ile listeni eşitle.

Go ile yazıldı. Arayüz Bubble Tea, oynatıcı mpv. Her şey klavyeyle yapılır.

---

## İçindekiler

- [Kurulum](#kurulum)
- [İlk adımlar](#i̇lk-adımlar)
- [Ekranlar](#ekranlar)
- [Kısayollar](#kısayollar)
- [Özellikler](#özellikler)
- [Kaynaklar](#kaynaklar)
- [MyAnimeList](#myanimelist)
- [İndirme ve çevrimdışı](#i̇ndirme-ve-çevrimdışı)
- [Görünüm](#görünüm)
- [Ayarlar ve dosyalar](#ayarlar-ve-dosyalar)
- [Sorun giderme](#sorun-giderme)
- [Geliştirme](#geliştirme)

---

## Kurulum

**Gerekenler**

| Araç | Ne için | Kurulum (Windows) |
|---|---|---|
| **mpv** | oynatıcı (zorunlu) | `winget install mpv-player.mpv-CI.MSVC` |
| yt-dlp | bazı kaynaklar, fragmanlar, HLS indirme | `winget install yt-dlp.yt-dlp` |
| aria2c | hızlı, kaldığı yerden devam eden indirme | `winget install aria2.aria2` |
| ffmpeg | HLS indirmelerini mp4'e çevirme | `winget install Gyan.FFmpeg` |

mpv dışındakiler isteğe bağlıdır. Eksik olanı ilk açılış rehberi gösterir.

**Kurulum ve güncelleme**

- [Releases](https://github.com/RobloxPro1453/aniclitr/releases/latest) sayfasından sistemine uygun dosyayı indirip çalıştır (Windows: `aniclitr_windows_amd64.exe`). Ayrıntılar: [UPDATE.md](UPDATE.md).
- İlk açılıştaki turda **terminal kısayolu** adımı aniclitr'i `%LOCALAPPDATA%\Programs\aniclitr\` altına kurar ve PATH'e ekler. Sonradan da yapılabilir: Ayarlar → Terminalden aç.
- O andan sonra her terminalde `aniclitr` yazman yeter.
- **Kurulu kopya kendini günceller.** Açılışta GitHub'daki son sürüme bakar; varsa indirir, SHA-256 ile doğrular ve bir sonraki açılışta yeni sürüm açılır.
  - Kapatmak için: Ayarlar → Otomatik güncelle.
  - Elle denetlemek için: Ayarlar → Güncellemeleri denetle, ya da komut satırında `aniclitr update`.

**Kaynaktan derleme**

```
go build -o bin\aniclitr.exe ./cmd/aniclitr
```

Kaynaktan derlenen kopya (`bin\`) bir geliştirme kopyasıdır; kendini güncellemez, yalnızca yeni sürüm olduğunu söyler. Terminal kısayolu her zaman kurulu kopyayı açar.

**Çalıştırma**

```
aniclitr                # arayüz
aniclitr doctor         # bütün kaynakları canlı test et
aniclitr update         # son sürümü kur
aniclitr --version
```

Önerilen terminal: **Windows Terminal** (truecolor ve Sixel destekler). En az 60×18 boyut gerekir.

---

## İlk adımlar

1. İlk açılışta bir tanıtım turu gelir: tema, araç kontrolü, nasıl çalışır, izlerken, kitaplık ve keşif, MyAnimeList (isteğe bağlı), terminal kısayolu ve tuşlar. Tur her zaman Ayarlar → Tanıtım turu ile yeniden açılır.
2. Turda **terminal kısayolu** adımı aniclitr'in klasörünü kullanıcı PATH'ine eklemeyi önerir (yönetici izni gerekmez). Sonra her yeni terminalde `aniclitr` yazman yeter. Sonradan: Ayarlar → Terminalden aç.
3. Arama kutusuna bir isim yaz. Yazarken öneriler gelir; `enter` ile aç.
4. Bölüm listesinde `enter` fansub seçtirir, `p` en iyi kaynakla hemen oynatır.
5. mpv kapanınca kaldığın saniye kaydedilir. Bir sonraki açılışta boş arama kutusunda `enter` son izlediğinin devamını açar.

Her ekranda `?` o ekranın bütün tuşlarını gösterir. Her güncellemeden sonra bir kez "yenilikler" kartı çıkar.

---

## Ekranlar

| Ekran | Açılış | İçerik |
|---|---|---|
| **Ana sayfa** | — | Arama, "devam et" rafı, yeni bölümler, bu haftanın takvimi, istatistik |
| **Anime** | aramadan / listelerden | Sezonlar, bölümler, ilerleme, notlar, ayrıntı ve kapak |
| **Fansub seçici** | bölümde `enter` | Fansub → kaynak → kalite; izle ya da indir |
| **Oynatılıyor** | izlerken | Sade kart; mpv penceresi öndedir, terminal boşta bekler |
| **Kitaplık** | `ctrl+b` | Raflar (izliyor, tamamlandı…), kendi listelerin, puanlar; liste ya da poster duvarı |
| **Keşfet** | `ctrl+e` | Takvim · bu akşam · bu sezon · türler · öneriler · rastgele |
| **İndirilenler** | `ctrl+d` | Kapaklı çevrimdışı raf; `v` ile indirme sırası |
| **Ayarlar** | `ctrl+o` | Hesap · görünüm · oynatma · kaynaklar |

Sağ üstteki iki düğme her yerde durur:
- **`◆ n` bildirimler** (`alt+n`): yeni bölümler, biten indirmeler, uyarılar.
- **`≡` menü** (`alt+m`): bütün ekranlar.

---

## Kısayollar

**Her yerde**

| Tuş | İş |
|---|---|
| `alt+m` · `F10` | menü |
| `alt+n` · `F9` | bildirimler |
| `ctrl+k` | komut paleti (her şey için bulanık arama) |
| `ctrl+b` · `F2` | kitaplık |
| `ctrl+e` · `F4` | keşfet |
| `ctrl+d` · `F3` | indirilenler |
| `ctrl+o` | ayarlar |
| `ctrl+t` | tema seç (canlı önizleme) |
| `ctrl+z` | son silmeyi geri al |
| `alt+a` | arşiv modu |
| `ctrl+l` | ekranı baştan çiz |
| `?` · `F1` | bu ekranın tuşları |
| `esc` | geri |
| `ctrl+c` | çık |

**Ana sayfa:** Yazmak aramadır. `esc` önce yazıyı siler, ikinci `esc` kutudan çıkar; dışarıdayken `m` menü, `n` bildirimler. `↓` listelere iner. Boş kutuda `enter` son izlediğine devam eder.

**Anime ekranı**

| Tuş | İş |
|---|---|
| `enter` | fansub seç |
| `p` · `space` | hemen oynat (indirilmişse internetsiz) |
| `tab` `[` `]` | sezon değiştir |
| `w` | izlendi işaretle |
| `N` | bölüme not yaz |
| `d` / `D` | bu bölümü / bütün sezonu indir (fansub seçtirir) |
| `a` | kitaplık: durum, puan, listeler |
| `t` | fragman |
| `y` | benzer animeler |
| `s` | başka kaynağa geç: geçmişteki kaynaklar hemen gelir, diğerleri (kapalı olanlar dahil) aranır; kaldığın bölüm korunur |

**İzlerken** (mpv penceresinde de çalışır): `n` sonraki, `b` önceki, `space` duraklat, `←` `→` 10 sn sar, `u` internetten altyazı ara (terminalde), `q` çık.

**Kitaplık:** `1`–`9`, `0` puan (0 = 10) · `s` durum değiştir · `f` favori · `v` liste / poster duvarı · `o` sırala · `/` ara · `n` yeni liste · `d` çıkar.

---

## Özellikler

**İzleme**
- **Kaldığın yer:** saniyesi saniyesine. "Devam et" rafında durduğun kare görünür.
- **Açılış ve bitiş atlama:** AniSkip verisiyle, bölüm süresine göre doğru zaman seçilir.
- **Bitiş kartı:** Ending başlayınca mpv'de "Sıradaki: … · n hemen geç" çıkar.
- **Otomatik sonraki bölüm:** aynı fansubla devam eder, sezon sınırlarını da geçer.
- **İzlendi sayılma:** Bölümün %75'i izlenince izlendi sayılır; geçmiş, MAL ve "sıradaki" buna göre güncellenir.
- **Akıllı kurtarma:** Bir kaynak açılmazsa sıradaki kaynak, o da olmazsa sıradaki fansub kendiliğinden denenir.
- **Hazırlık:** İmleç bir bölümde yarım saniye durunca link önceden hazırlanır; `enter` beklemeden açar.

**Keşif**
- **Takvim:** bu haftanın yayınları, yerel saatle ve geri sayımla. Takip ettiklerin işaretlidir.
- **Bu akşam:** yarım kalanlar, yeni çıkanlar, devam ettiklerin ve planlananlar, vaktine göre (`+` / `-`).
- **Türler:** `[` `]` tür, `m` en iyi / popüler / kült, `space` o türde zar.
- **Öneriler:** puanladığın ve bitirdiğin animelere göre. **Rastgele:** slot makinesi.
- **Yeni bölüm uyarısı:** Takip ettiğin seriye yeni bölüm düşünce bildirim gelir.

**Kitaplık:** durumlar (izliyor · tamamlandı · planlanan · beklemede · bırakıldı), 1–10 puan, kendi izleme listelerin, favoriler. İzlediğin her anime kendiliğinden "izliyor"a girer; serinin son bölümü onu "tamamlandı"ya taşır.

**Geri alma:** Kitaplıktan ya da geçmişten silme, liste silme ve bildirim temizleme 2 dakika içinde `ctrl+z` ile geri alınır.

**Harici altyazı:** İzlerken `u` OpenSubtitles'ta arar ve seçtiğini çalan mpv'ye anında ekler. Ücretsiz API anahtarı gerekir, bkz. [Ayarlar ve dosyalar](#ayarlar-ve-dosyalar).

---

## Kaynaklar

| Kaynak | Rol | Nasıl |
|---|---|---|
| **AnimeciX** | ana kaynak, Türkçe, canlı | JSON API → tau-video → doğrudan MP4 |
| **AnimeTR** | ek, Türkçe (anime + donghua) | arama JSON'u + bölüm sayfasındaki fansub listesi → OK.ru / Google Drive (yt-dlp) |
| **HiAnime** | ek, İngilizce (altyazı / dublaj) | HTML + gömülü oynatıcı → HLS + altyazı |
| **TürkAnime arşivi** | eski seriler, `alt+a` ile açılır | AnimeDepo GitLab dizini → 3. parti sunucular (çoğu link ölü) |

Arama açık bütün kaynaklarda paralel yapılır. Aynı anime hangi kaynakta izlenirse izlensin rafta tek kart olarak durur. Aynı anime farklı sitelerdeyse tek satırda birleşir; renkli noktalar hangi sitelerde olduğunu gösterir. Başlıktaki noktalar kaynakların canlı sağlığını gösterir: renkli çalışıyor, sarı yavaş, ✗ yanıt yok. Ayrıntı için `ctrl+k` → "Kaynak durumu".

Keşif verisi (takvim, sezon, öneriler, fragman) **AniList**'ten gelir. Giriş gerektirmez.

---

## MyAnimeList

Bağlanınca kitaplığın MAL listenin aynası olur:
- Durum, puan, izlenen bölüm sayısı ve listelerin iki yönlü eşitlenir. Listeler MAL'de etiket olarak durur.
- MAL'deki ilerlemen bölüm listesinde görünür: izlediğin sayıya kadar ✓, anime açılınca imleç sıradaki bölümde. `w` ile bir bölümü geri almak MAL'deki sayıyı da düşürür.
- Kitaplıkta durum değişikliği `ctrl+z` ile geri alınır; geri alma MAL'e de gönderilir.
- İnternet yokken yaptıkların sonra gönderilir.

**Bir kez yapılır:**

1. [myanimelist.net/apiconfig](https://myanimelist.net/apiconfig) → **Create ID**
2. App Type: **other** · App Redirect URL: **`http://localhost:47615/callback`** · diğer alanlar serbest
3. Çıkan **Client ID**'yi kopyala.
4. aniclitr'de kitaplıkta `m` (ya da Ayarlar → MyAnimeList) → yapıştır → `enter` → tarayıcıda **Allow**.

Şifren aniclitr'e hiç gelmez (OAuth PKCE). Oturum kendiliğinden yenilenir.

> Bilinen sınır: AnimeciX sezonları tek başlıkta toplar, MAL her sezona ayrı kayıt açar. İzlenen bölüm sayısı şimdilik ilk sezon için gönderilir.

---

## İndirme ve çevrimdışı

- Bölüm listesinde **`d`** o bölümü, **`D`** bütün sezonu indirir. İkisi de önce **fansub seçtirir**; `→` ile kaynak ve kalite de seçilebilir.
- **Seçtiğin fansub bağlayıcıdır.** O bölümde yoksa başka fansuba geçilmez; hata olarak görünür, başka fansubla tekrar denersin.
- **Araçlar:**
  - Doğrudan dosyalar **aria2c** ile çok bağlantılı ve kaldığı yerden devam ederek iner.
  - HLS akışları **yt-dlp** ile mp4 olur.
  - İkisi de yoksa dahili indirici çalışır.
- **Link süresi:** Link, iş sırası geldiğinde yeniden alınır; sitelerin linkleri süreli olduğu için sırada bekleyenler bozulmaz.
- **Dosyalar:** `~/Videos/aniclitr/<Anime>/S01E05 [fansub] [1080p].mp4`. Altyazı varsa aynı adla, kapak `kapak.jpg` olarak yanına iner.
- **İnternetsiz izleme:** İnen bölümler listede `↓` ile işaretlenir; `p` internetsiz oynatır.
- **Açılışta internet yoksa** arama ve akış yerine yalnızca indirdiklerinin kapaklı rafı açılır. `r` bağlantıyı yeniden dener. Bağlantı gelince ana sayfa kendiliğinden döner.

> Hız notu: İlk kez indirilen bölümler sitenin kendi sunucusundan gelir ve yavaş olabilir. Aynı bölüm Cloudflare önbelleğine girdikten sonra hat hızında iner. İndirme satırında açık bağlantı sayısı görünür.

---

## Görünüm

- **Temalar:** gece · catppuccin · tokyo night · rosé pine · gruvbox · nord · matcha · şafak (açık). `ctrl+t` ile gezerken canlı uygulanır.
- **Kendi temanı** `%APPDATA%\aniclitr\themes\<ad>.json` olarak ekleyebilirsin. Alanlar: `name bg text sub muted faint band surface accent accent2 sky ok warn err`, isteğe bağlı `light`.
- **Kapaktan tema:** Bir anime açılınca arayüz kapağının renklerine bürünür, çıkınca döner.
- **Kapaklar:** Terminal destekliyorsa Sixel ile tam çözünürlükte, değilse renkli blok karakterlerle çizilir.
- **Hareket:** ambiyans (sakura, kar, yağmur, yıldız, ateşböceği; otomatikte mevsime ve saate göre), logo girişi, kayan seçim çubuğu, konfeti. İstemezsen Ayarlar → Animasyonlar ya da `ANICLITR_REDUCED_MOTION=1`.
- **Arayüz sesleri:** gezinirken tık, seçimde çın, kutlamada arpej. Ayarlar → Arayüz sesleri (0 = kapalı).
- **İzlerken terminal boşta durur:** mpv öndedir, arayüz kare harcamaz.

---

## Ayarlar ve dosyalar

Ayarların çoğu uygulama içinden değişir (`ctrl+o`). Hepsi `%APPDATA%\aniclitr\config.json` dosyasındadır:

| Anahtar | Açıklama |
|---|---|
| `theme`, `theme_bg`, `cover_theme` | tema, terminal zeminini boyama, kapaktan tema |
| `ambient`, `intro`, `motion`, `sounds` | ambiyans, logo efekti, animasyonlar, ses seviyesi |
| `posters`, `graphics` | kapaklar; `auto` / `sixel` / `blocks` |
| `quality` | `best`, `1080p`, `720p`, `480p` |
| `auto_skip`, `auto_next`, `fullscreen` | oynatma davranışı |
| `sources`, `legacy` | açık kaynaklar (`animecix`, `animetr`, `hianime`, `animedepo`), arşiv modu |
| `mal_client_id` | MyAnimeList uygulaması |
| `opensubtitles_api_key` | harici altyazı (opensubtitles.com → profil → API consumers) |
| `download_dir` | indirme klasörü |
| `renderer` | `hücre` (varsayılan) ya da `klasik` (yedek çizici) |
| `mpv_path`, `extra_mpv_args` | mpv yolu ve ek argümanlar |

**Veri dosyaları** (`%APPDATA%\aniclitr\`): `history.json` (geçmiş, notlar, izleme süresi), `library.json` (kitaplık), `downloads.json` (indirmeler), `mal.json` (MAL oturumu), `aniclitr.log` (günlük).

Bozuk bir veri dosyası asla üzerine yazılmaz; `.bozuk` uzantısıyla yedeklenir. Eski adla (`%APPDATA%\kaze`) kalan veriler ilk açılışta otomatik taşınır.

---

## Sorun giderme

| Durum | Çözüm |
|---|---|
| Ekranda artık / bozukluk | `ctrl+l` ekranı baştan çizer. Sürerse Ayarlar → Çizici → `klasik` |
| Oynatma başlamıyor | Akıllı kurtarma başka kaynak dener. Olmazsa fansub seçicide başka kaynak seç. Ayrıntı günlükte |
| Kapaklar bulanık | Windows Terminal 1.22+ kullan. Ayarlar → Görsel kalitesi → `sixel` |
| Bir kaynak hiç yanıt vermiyor | `aniclitr doctor` · `ctrl+k` → "Kaynak durumu" |
| Ayrıntılı hata | `%APPDATA%\aniclitr\aniclitr.log` (iç hatalar yığın iziyle) |

Gereksinim: TTY, UTF-8, en az 60×18. `NO_COLOR` ya da `TERM=dumb` ile kapaklar ve animasyonlar kapanır.

---

## Geliştirme

Mimari, akışlar, kurallar ve tuzaklar için **[ARCHITECTURE.md](ARCHITECTURE.md)**. Yapılacaklar **[TODO.md](TODO.md)** dosyasında.

Güncelleme sisteminin ayrıntıları ve adım adım sürüm çıkarma: **[UPDATE.md](UPDATE.md)**.

**Sürüm çıkarmak:** `internal/ui/onboard.go` içine `whatsNew["X.Y.Z"]` satırlarını yaz, her şeyi commit et, sonra çalıştır:

```
.\scripts\release.ps1 X.Y.Z
```

Betik şunları yapar:
1. Testleri çalıştırır.
2. Sürüm numarasını yükseltir.
3. Commit ve `vX.Y.Z` etiketini oluşturur (etiket notu = "yenilikler" satırları).
4. GitHub'a gönderir.

GitHub Actions ardından 6 platform için exe'leri ve `checksums.txt` dosyasını derleyip sürümü yayınlar.

```
go vet ./...
go test ./internal/...                       # çevrimdışı testler
ANICLITR_LIVE=1 go test ./internal/...       # gerçek kaynaklara karşı
ANICLITR_FUZZ=150 go test ./internal/tty     # uzun çizici zorlama testi
```
