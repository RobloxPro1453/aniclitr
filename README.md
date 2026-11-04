<div align="center">

# aniclitr

**Türkçe altyazılı anime için klavyeyle kullanılan terminal oynatıcısı**

[![Sürüm](https://img.shields.io/github/v/release/RobloxPro1453/aniclitr?label=s%C3%BCr%C3%BCm)](https://github.com/RobloxPro1453/aniclitr/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platformlar](https://img.shields.io/badge/platform-Windows%20%C2%B7%20Linux%20%C2%B7%20macOS-informational)](#kurulum)
[![Lisans: GPL-3.0](https://img.shields.io/badge/lisans-GPL--3.0-blue)](LICENSE)

</div>

aniclitr; anime arama, çeviri grubu (fansub) seçimi, oynatma, indirme ve izleme takibini tek bir terminal uygulamasında toplar. Kalınan yer, sıradaki bölüm ve jenerik atlama otomatik olarak yönetilir. Kitaplık MyAnimeList ile iki yönlü eşitlenebilir ve bölümler çevrimdışı izlenmek üzere indirilebilir.

> **English summary.** aniclitr is a keyboard-driven terminal anime player for Turkish-subtitled content. It searches live sources, lets you pick the fansub, plays through mpv, resumes where you left off, downloads episodes for offline viewing and syncs your library with MyAnimeList. The interface and documentation are in Turkish.

---

## İçindekiler

- [Özellikler](#özellikler)
- [Kurulum](#kurulum)
- [Hızlı başlangıç](#hızlı-başlangıç)
- [Arayüz](#arayüz)
- [Klavye kısayolları](#klavye-kısayolları)
- [Kaynaklar](#kaynaklar)
- [MyAnimeList entegrasyonu](#myanimelist-entegrasyonu)
- [İndirme ve çevrimdışı kullanım](#i̇ndirme-ve-çevrimdışı-kullanım)
- [Görünüm](#görünüm)
- [Yapılandırma](#yapılandırma)
- [Sorun giderme](#sorun-giderme)
- [Geliştirme](#geliştirme)
- [Lisans](#lisans)
- [Sorumluluk reddi](#sorumluluk-reddi)

---

## Özellikler

| Alan | Açıklama |
|---|---|
| **Arama** | Etkin tüm kaynaklarda eşzamanlı arama; farklı kaynaklardaki aynı anime tek sonuçta birleştirilir. Yazarken öneri gösterilir. |
| **Oynatma** | mpv ile oynatma, saniye hassasiyetinde kalınan yerden devam, AniSkip ile açılış ve bitiş jeneriği atlama, otomatik sonraki bölüm. |
| **Dayanıklılık** | Bir kaynak açılmazsa sıradaki kaynak, ardından sıradaki çeviri grubu otomatik olarak denenir. |
| **Kitaplık** | İzliyor, tamamlandı, planlanan, beklemede ve bırakıldı durumları; 1–10 puan, özel listeler, favoriler ve izleme istatistikleri. |
| **Keşfet** | AniList verisiyle haftalık yayın takvimi, sezon listesi, türe göre listeler, kişisel öneriler ve rastgele öneri. |
| **MyAnimeList** | Durum, puan, izlenen bölüm sayısı ve listelerin iki yönlü eşitlenmesi (OAuth PKCE). |
| **İndirme** | Seçilen çeviri grubuyla bölüm ya da sezon indirme; aria2c, yt-dlp veya dahili indirici. Çevrimdışı kitaplık. |
| **Görünüm** | Hazır ve özel temalar, kapak görsellerine göre renklenen arayüz, Sixel destekli kapaklar, isteğe bağlı animasyonlar. |
| **Güncelleme** | Kurulu kopya GitHub Releases üzerinden kendini günceller; her dosya SHA-256 ile doğrulanır. |

---

## Kurulum

### Gereksinimler

| Araç | Amaç | Windows kurulumu |
|---|---|---|
| **mpv** | Video oynatıcı (zorunlu) | `winget install mpv-player.mpv-CI.MSVC` |
| yt-dlp | Bazı kaynaklar, fragmanlar ve HLS indirmeleri | `winget install yt-dlp.yt-dlp` |
| aria2c | Hızlı ve devam ettirilebilir indirme | `winget install aria2.aria2` |
| ffmpeg | HLS indirmelerini MP4'e dönüştürme | `winget install Gyan.FFmpeg` |

mpv dışındaki araçlar isteğe bağlıdır; eksik olanlar ilk açılıştaki tanıtım turunda listelenir.

**Önerilen terminal:** Windows Terminal (truecolor ve Sixel desteği). Pencere en az 60×18 karakter olmalıdır.

### Hazır sürüm (önerilen)

1. [Releases](https://github.com/RobloxPro1453/aniclitr/releases/latest) sayfasından sisteminize uygun dosyayı indirin:

   | Sistem | Dosya |
   |---|---|
   | Windows (x64) | `aniclitr_windows_amd64.exe` |
   | Windows (ARM) | `aniclitr_windows_arm64.exe` |
   | Linux | `aniclitr_linux_amd64` · `aniclitr_linux_arm64` |
   | macOS | `aniclitr_darwin_arm64` (Apple Silicon) · `aniclitr_darwin_amd64` (Intel) |

2. Dosyayı çalıştırın. Tanıtım turunun **Terminalden erişim** adımında `y` tuşuna basıldığında aniclitr kurulur ve kullanıcı PATH'ine eklenir. Yönetici izni gerekmez.
3. Bu adımdan sonra her terminalde `aniclitr` komutu yeterlidir.

Kurulum klasörü Windows'ta `%LOCALAPPDATA%\Programs\aniclitr\`, Linux ve macOS'ta `~/.local/share/aniclitr/` klasörüdür (`~/.local/bin/aniclitr` bağlantısıyla). Kurulum, **Ayarlar → Terminalden aç** üzerinden sonradan yapılabilir ya da kaldırılabilir.

### Güncellemeler

Kurulu kopya açılışta GitHub'daki son sürümü denetler. Yeni bir sürüm varsa indirilir, `checksums.txt` içindeki SHA-256 özetiyle doğrulanır ve bir sonraki açılışta etkinleşir. Ayrıntılar için [docs/UPDATE.md](docs/UPDATE.md) belgesine bakın.

| İşlem | Yöntem |
|---|---|
| Otomatik güncellemeyi açma / kapatma | Ayarlar → Otomatik güncelle |
| Elle denetleme | Ayarlar → Güncellemeleri denetle ya da `aniclitr update` |
| Sürümü görüntüleme | `aniclitr --version` |

### Kaynaktan derleme

Go 1.27 veya üstü gerekir.

```bash
git clone https://github.com/RobloxPro1453/aniclitr.git
cd aniclitr
go build -o bin/aniclitr ./cmd/aniclitr
```

Kaynaktan derlenen kopya kendini güncellemez; yalnızca yeni bir sürüm olduğunu bildirir.

### Komut satırı

```text
aniclitr            Arayüzü başlatır
aniclitr doctor     Tüm kaynakları canlı olarak test eder
aniclitr update     Son sürümü kurar
aniclitr --version  Sürümü gösterir
```

---

## Hızlı başlangıç

1. İlk açılışta tanıtım turu başlar: tema seçimi, gerekli araçların denetimi, temel kullanım, MyAnimeList bağlantısı (isteğe bağlı) ve terminalden erişim. Tur **Ayarlar → Tanıtım turu** üzerinden her zaman yeniden açılabilir.
2. Ana sayfadaki arama kutusuna bir anime adı yazın ve `enter` tuşuna basın.
3. Bölüm listesinde `enter` çeviri grubunu sorar; `p` en uygun kaynakla doğrudan oynatır.
4. Oynatıcı kapatıldığında kalınan an kaydedilir. Arama kutusu boşken `enter`, son izlenen animeye kalınan yerden devam eder.

Her ekranda `?` tuşu **İşlemler** menüsünü açar. Bu menü o ekrandaki tüm eylemleri kısayollarıyla listeler; bir eylem seçilip `enter` ile doğrudan çalıştırılabilir. Kısayolların ezberlenmesi gerekmez.

---

## Arayüz

Arayüz adım adım ilerler: **menü → liste → öğe → eylem**. `esc` her zaman bir adım geri döner. Her sayfanın başında başlığı ve amacı yazar; liste ile ayrıntı bölgeleri ayrı panellerde gösterilir.

| Ekran | Erişim | İçerik |
|---|---|---|
| **Ana sayfa** | — | Arama kutusu ve ana menü: Devam et, Yeni bölümler, Kitaplık, Keşfet, İndirilenler, Ayarlar, Çıkış. Seçilen öğenin önizlemesi yanda; geniş ekranlarda haftalık takvim ve istatistikler. |
| **Yeni bölümler** | Ana menü | Tüm kaynaklarda son yayınlanan bölümler; `f` yalnızca takip edilenleri gösterir. |
| **Anime** | Arama sonuçları, listeler | Sezonlar, bölümler, izleme durumu, notlar, ayrıntılar ve kapak. |
| **Çeviri seçici** | Bölümde `enter` | Çeviri grubu → kaynak → kalite; oynatma ya da indirme. |
| **Kitaplık** | `ctrl+b` | Durumlar, özel listeler ve puanlar; liste ya da kapak duvarı görünümü. |
| **Keşfet** | `ctrl+e` | Önce bölüm seçilir (takvim, bu akşam, bu sezon, türler, öneriler, rastgele), ardından içerik kendi sayfasında açılır. Türler bölümünde önce tür ve sıralama seçilir. |
| **İndirilenler** | `ctrl+d` | Kapaklı çevrimdışı kitaplık; `v` indirme kuyruğunu gösterir. |
| **Ayarlar** | `ctrl+o` | Genel, görünüm, oynatma ve kaynak ayarları. |

Başlık çubuğunun sağında indirme durumu, kaynak sağlığı, bildirimler (`◆`, `alt+n`) ve tüm ekranları içeren menü (`≡`, `alt+m`) bulunur.

---

## Klavye kısayolları

Tüm kısayollar `?` (İşlemler) menüsünde de listelenir.

### Genel

| Tuş | İşlev |
|---|---|
| `↑` `↓` · `enter` · `esc` | Seçim · aç / çalıştır · bir adım geri |
| `?` · `F1` | İşlemler: bu ekrandaki tüm eylemler |
| `alt+m` · `F10` | Menü |
| `alt+n` · `F9` | Bildirimler |
| `ctrl+k` | Komut paleti (bulanık arama) |
| `ctrl+b` · `F2` | Kitaplık |
| `ctrl+e` · `F4` | Keşfet |
| `ctrl+d` · `F3` | İndirilenler |
| `ctrl+o` | Ayarlar |
| `ctrl+t` | Tema seçici (canlı önizleme) |
| `ctrl+z` | Son silme ya da değişikliği geri al (2 dakika içinde) |
| `alt+a` | Arşiv modu |
| `ctrl+l` | Ekranı yeniden çiz |
| `ctrl+c` | Çıkış |

**Ana sayfa:** Yazılan her karakter arama kutusuna gider. `esc` önce metni temizler, ikinci kez basıldığında menüye geçer. `↓` menüye iner.

### Anime ekranı

| Tuş | İşlev |
|---|---|
| `enter` | Çeviri grubunu seç |
| `p` · `space` | Doğrudan oynat (indirilmişse çevrimdışı) |
| `tab` · `[` `]` | Sezon değiştir |
| `w` | İzlendi olarak işaretle |
| `N` | Bölüme not ekle |
| `d` · `D` | Bölümü · sezonu indir |
| `a` | Kitaplık: durum, puan, listeler |
| `t` | Fragman |
| `y` | Benzer animeler |
| `s` | Başka bir kaynakta aç (kalınan bölüm korunur) |

### Oynatma sırasında

mpv penceresinde de geçerlidir: `n` sonraki bölüm, `b` önceki bölüm, `space` duraklat, `←` `→` 10 saniye sar, `u` çevrimiçi altyazı ara (terminalde), `q` çık.

### Kitaplık

`1`–`9`, `0` puan (0 = 10) · `s` durum · `f` favori · `v` liste / kapak duvarı · `o` sıralama · `/` filtre · `n` yeni liste · `d` kaldır.

---

## Kaynaklar

| Kaynak | Rol | Yöntem |
|---|---|---|
| **AnimeciX** | Birincil kaynak, Türkçe | JSON API → tau-video → doğrudan MP4 |
| **AnimeTR** | Ek kaynak, Türkçe (anime ve donghua) | Arama JSON'u ve bölüm sayfası → OK.ru, Google Drive (yt-dlp) |
| **HiAnime** | Ek kaynak, İngilizce (altyazılı / dublajlı) | HTML ve gömülü oynatıcı → HLS ve altyazı |
| **TürkAnime arşivi** | Eski seriler, `alt+a` ile etkinleşir | AnimeDepo dizini → üçüncü taraf sunucular (bağlantıların çoğu artık çalışmıyor) |

- Arama, etkin tüm kaynaklarda paralel yapılır.
- Aynı anime hangi kaynaktan izlenirse izlensin kitaplıkta ve geçmişte tek kayıt olarak tutulur.
- Başlık çubuğundaki noktalar kaynakların anlık durumunu gösterir: renkli çalışıyor, sarı yavaş, ✗ yanıt vermiyor. Ayrıntılar **Menü → Kaynak durumu** ekranındadır.
- Keşif verileri (takvim, sezon, öneriler, fragmanlar) [AniList](https://anilist.co)'ten alınır ve hesap gerektirmez.

---

## MyAnimeList entegrasyonu

Hesap bağlandığında kitaplık, MyAnimeList listesinin yansıması olur:

- Durum, puan, izlenen bölüm sayısı ve listeler iki yönlü eşitlenir. Listeler MyAnimeList'te etiket olarak saklanır.
- MyAnimeList'teki ilerleme bölüm listesinde işaretlenir; anime açıldığında imleç sıradaki bölümde durur.
- Kitaplıktaki durum değişiklikleri `ctrl+z` ile geri alınabilir; geri alma işlemi MyAnimeList'e de gönderilir.
- Çevrimdışıyken yapılan değişiklikler bağlantı kurulduğunda gönderilir.

### Kurulum (bir kez)

1. [myanimelist.net/apiconfig](https://myanimelist.net/apiconfig) adresinde **Create ID** seçeneğini kullanın.
2. **App Type** olarak `other`, **App Redirect URL** olarak `http://localhost:47615/callback` girin. Diğer alanlar serbesttir.
3. Oluşturulan **Client ID** değerini kopyalayın.
4. aniclitr'de **Ayarlar → MyAnimeList** (ya da kitaplıkta `m`) ekranına yapıştırıp `enter` tuşuna basın ve tarayıcıda **Allow** seçeneğini onaylayın.

Parolanız aniclitr'e hiçbir zaman iletilmez (OAuth PKCE). Oturum otomatik olarak yenilenir.

> **Bilinen sınırlama:** AnimeciX sezonları tek başlık altında toplar, MyAnimeList ise her sezon için ayrı kayıt tutar. İzlenen bölüm sayısı şu anda yalnızca ilk sezon için eşitlenir.

---

## İndirme ve çevrimdışı kullanım

- Bölüm listesinde `d` seçili bölümü, `D` tüm sezonu indirir. Her ikisi de önce çeviri grubunu sorar; `→` ile kaynak ve kalite de seçilebilir.
- **Seçilen çeviri grubu bağlayıcıdır.** Grup o bölümde yoksa başka bir gruba geçilmez; hata olarak bildirilir.
- **İndirme araçları:** doğrudan dosyalar için aria2c (çok bağlantılı, devam ettirilebilir), HLS akışları için yt-dlp; ikisi de yoksa dahili indirici kullanılır.
- **Bağlantı süresi:** Kaynak bağlantıları süreli olduğundan her iş, sırası geldiğinde güncel bağlantıyla başlar.
- **Dosya düzeni:** `~/Videos/aniclitr/<Anime>/S01E05 [çeviri grubu] [1080p].mp4`. Varsa altyazı aynı adla, kapak `kapak.jpg` olarak aynı klasöre kaydedilir.
- **Çevrimdışı izleme:** İndirilen bölümler listede `↓` ile işaretlenir ve `p` ile internet bağlantısı olmadan oynatılır.
- Açılışta internet bağlantısı yoksa yalnızca çevrimdışı kitaplık gösterilir; bağlantı geldiğinde ana sayfa otomatik olarak açılır.

---

## Görünüm

- **Temalar:** gece, catppuccin, tokyo night, rosé pine, gruvbox, nord, matcha, şafak (açık tema). `ctrl+t` ile canlı önizlemeyle seçilir.
- **Özel temalar:** `%APPDATA%\aniclitr\themes\<ad>.json` dosyası olarak eklenebilir. Alanlar: `name`, `bg`, `text`, `sub`, `muted`, `faint`, `band`, `surface`, `accent`, `accent2`, `sky`, `ok`, `warn`, `err` ve isteğe bağlı `light`.
- **Kapaktan tema:** Bir anime açıldığında arayüz kapak görselinin renklerini alır.
- **Kapak görselleri:** Sixel destekleyen terminallerde tam çözünürlükte, diğerlerinde renkli blok karakterlerle çizilir.
- **Animasyonlar:** Logo girişi, arka plan efektleri, geçişler. **Ayarlar → Animasyonlar** ya da `ANICLITR_REDUCED_MOTION=1` ile kapatılabilir.
- Oynatma sırasında arayüz boşta bekler ve işlemci kullanmaz.

---

## Yapılandırma

Ayarların tamamı uygulama içinden (`ctrl+o`) değiştirilebilir ve `%APPDATA%\aniclitr\config.json` dosyasında saklanır.

| Anahtar | Açıklama |
|---|---|
| `theme`, `theme_bg`, `cover_theme` | Tema, terminal arka planını boyama, kapaktan tema |
| `ambient`, `intro`, `motion`, `sounds` | Arka plan efekti, logo efekti, animasyonlar, ses düzeyi |
| `posters`, `graphics` | Kapak görselleri; `auto`, `sixel` ya da `blocks` |
| `quality` | `best`, `1080p`, `720p`, `480p` |
| `auto_skip`, `auto_next`, `fullscreen` | Oynatma davranışı |
| `sources`, `legacy` | Etkin kaynaklar (`animecix`, `animetr`, `hianime`, `animedepo`), arşiv modu |
| `mal_client_id` | MyAnimeList uygulama kimliği |
| `opensubtitles_api_key` | Çevrimiçi altyazı (opensubtitles.com → profil → API consumers) |
| `download_dir` | İndirme klasörü |
| `renderer` | `hücre` (varsayılan) ya da `klasik` (yedek çizici) |
| `mpv_path`, `extra_mpv_args` | mpv yolu ve ek argümanlar |

### Veri dosyaları

`%APPDATA%\aniclitr\` klasöründe:

| Dosya | İçerik |
|---|---|
| `history.json` | İzleme geçmişi, notlar, izleme süresi |
| `library.json` | Kitaplık |
| `downloads.json` | İndirme işleri |
| `mal.json` | MyAnimeList oturumu |
| `aniclitr.log` | Uygulama günlüğü |

Bozuk bir veri dosyası hiçbir zaman üzerine yazılmaz; `.bozuk` uzantısıyla yedeklenir. Görsel önbelleği `%LOCALAPPDATA%\aniclitr\` altındadır.

### Ortam değişkenleri

| Değişken | Etki |
|---|---|
| `ANICLITR_HOME` | Veri ve önbellek için ayrı bir klasör (taşınabilir kullanım) |
| `ANICLITR_REDUCED_MOTION` | Animasyonları kapatır |
| `NO_COLOR`, `TERM=dumb` | Kapakları ve animasyonları kapatır |

---

## Sorun giderme

| Belirti | Çözüm |
|---|---|
| Ekranda artık ya da bozulma | `ctrl+l` ekranı yeniden çizer. Sorun sürerse **Ayarlar → Çizici → klasik**. |
| Oynatma başlamıyor | Başka bir kaynak otomatik olarak denenir. Sonuç alınamazsa çeviri seçicide farklı bir kaynak seçin. Ayrıntılar günlük dosyasındadır. |
| Kapaklar bulanık | Windows Terminal 1.22 veya üstünü kullanın; **Ayarlar → Görsel kalitesi → sixel**. |
| Bir kaynak yanıt vermiyor | `aniclitr doctor` komutunu çalıştırın ya da **Menü → Kaynak durumu** ekranına bakın. |
| Kaydırırken takılma | `aniclitr.log` dosyasındaki "yavaş kare" ve "yavaş terminal" kayıtlarını bir hata bildirimine ekleyin. |
| Ayrıntılı hata bilgisi | `%APPDATA%\aniclitr\aniclitr.log` (iç hatalar yığın iziyle kaydedilir) |

Hata bildirimleri ve öneriler için [Issues](https://github.com/RobloxPro1453/aniclitr/issues) sayfasını kullanabilirsiniz.

---

## Geliştirme

Katkı süreci, testler ve kod kuralları [CONTRIBUTING.md](CONTRIBUTING.md) belgesinde açıklanmıştır.

| Belge | İçerik |
|---|---|
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | Mimari, veri akışı, değişmezler ve bilinen tuzaklar |
| [docs/DESIGN.md](docs/DESIGN.md) | Arayüz tasarım sistemi |
| [docs/UPDATE.md](docs/UPDATE.md) | Kurulum ve güncelleme mekanizması |
| [docs/OPTIMIZATION.md](docs/OPTIMIZATION.md) | Bellek ve kaynak kullanımı raporu |
| [CHANGELOG.md](CHANGELOG.md) | Sürüm geçmişi |
| [ROADMAP.md](ROADMAP.md) | Planlanan çalışmalar |

```bash
go vet ./...
go test ./internal/...                    # çevrimdışı testler
ANICLITR_LIVE=1 go test ./internal/...    # gerçek kaynaklara karşı testler
```

---

## Lisans

Copyright © 2026 RobloxPro1453 ve katkıda bulunanlar.

aniclitr özgür bir yazılımdır: Free Software Foundation tarafından yayımlanan [GNU Genel Kamu Lisansı sürüm 3](LICENSE) (GPL-3.0) koşulları altında yeniden dağıtabilir ve/veya değiştirebilirsiniz. Değiştirilmiş sürümlerin dağıtımı da aynı lisansla ve kaynak koduyla birlikte yapılmalıdır.

Bu program yararlı olması umuduyla dağıtılmaktadır, ancak **hiçbir garanti verilmez**; satılabilirlik ya da belirli bir amaca uygunluk garantisi dahil. Ayrıntılar için [LICENSE](LICENSE) dosyasına bakın.

---

## Sorumluluk reddi

aniclitr herhangi bir içerik barındırmaz, yüklemez ya da dağıtmaz. Uygulama, herkese açık üçüncü taraf web sitelerinde zaten yayımlanmış içeriğe bağlantı verir ve bu içeriği kullanıcının kendi oynatıcısında açar. İçeriklerin yasallığı ve kullanım koşulları ilgili sitelerin sorumluluğundadır. Kullanıcılar, bulundukları ülkenin mevzuatına ve sitelerin kullanım koşullarına uymakla yükümlüdür.

aniclitr; AnimeciX, AnimeTR, HiAnime, MyAnimeList, AniList ya da AniSkip ile bağlantılı değildir ve bu hizmetler tarafından desteklenmemektedir.
