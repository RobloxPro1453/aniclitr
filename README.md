<div align="center">

# aniclitr

**Türkçe altyazılı anime için klavyeyle kullanılan terminal oynatıcısı**

[![Sürüm](https://img.shields.io/github/v/release/RobloxPro1453/aniclitr?label=s%C3%BCr%C3%BCm)](https://github.com/RobloxPro1453/aniclitr/releases/latest)
[![Go](https://img.shields.io/badge/Go-1.27%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platformlar](https://img.shields.io/badge/platform-Windows%20%C2%B7%20Linux%20%C2%B7%20macOS-informational)](#kurulum)
[![Lisans: GPL-3.0](https://img.shields.io/badge/lisans-GPL--3.0-blue)](LICENSE)

</div>

aniclitr; anime arama, çeviri grubu (fansub) seçimi, oynatma, toplu/aralıklı indirme, karakter kadrosu inceleme ve izleme takibini tek bir terminal uygulamasında toplar. Kalınan yer, sıradaki bölüm, çok sezonlu seri hizalaması ve jenerik atlama otomatik olarak yönetilir. Kitaplık MyAnimeList ile iki yönlü eşitlenebilir ve bölümler çevrimdışı izlenmek üzere indirilebilir.

> **English summary.** aniclitr is a keyboard-driven terminal anime player for Turkish-subtitled content. It searches six live sources in parallel (AnimeciX, AnimeTR, AniHub, SonAnime, Animim, HiAnime), resolves streams natively (Tau Video, AniHub HLS, OK.ru, Google Drive, Sibnet, Voe, Vidmoly), lets you pick the fansub, plays through mpv, resumes where you left off, downloads individual episodes or custom episode ranges (`30-100`) for offline viewing, browses characters & voice actors (`Seiyuu`) with Sixel portraits, and syncs your library two-way with MyAnimeList across multi-season chains. The interface and documentation are in Turkish.

---

## İçindekiler

- [Özellikler](#özellikler)
- [Kurulum](#kurulum)
- [Hızlı başlangıç](#hızlı-başlangıç)
- [Arayüz](#arayüz)
- [Klavye kısayolları](#klavye-kısayolları)
- [Kaynaklar ve yerel çözücüler](#kaynaklar-ve-yerel-çözücüler)
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
| **Arama ve Doğrudan Bağlantı** | Etkin tüm kaynaklarda eşzamanlı arama; farklı kaynaklardaki aynı anime tek sonuçta birleştirilir. Arama kutusuna, komut paletine veya komut satırına yapıştırılan anime/bölüm bağlantıları doğrudan açılır. |
| **Oynatma** | mpv ile oynatma, saniye hassasiyetinde kalınan yerden devam, AniSkip ile açılış ve bitiş jeneriği atlama, otomatik sonraki bölüm. |
| **Yerel Çözücüler ve Dayanıklılık** | Tau Video, AniHub HLS, OK.ru, Vidmoly, Voe, Sibnet ve Google Drive bağlantıları yerel Go çözücüleriyle anında açılır; HLS/MP4 akışları oynatılmadan önce doğrulanır, ölü dosyalar oynatıcıya gitmez. Bir sunucu açılmazsa sıradaki sunucu, çeviri grubu veya diğer kaynak otomatik olarak denenir. Seçicide her sunucunun geçmiş başarı karnesi görünür. |
| **Kaynak Seçici** | Anime ekranında `s` ortada bir seçici açar: aynı anime hangi kaynaklarda varsa listelenir, `1`–`9` ile doğrudan geçilir. Ayarlarda kapattığın kaynakta izlediğin seri, açık kaynaklarda kaldığın yerden açılır. |
| **Karakterler ve Seslendirenler** | Anime sayfasında `c` tuşuyla açılan kadro ekranı: Sixel/halfblock karakter ve seiyuu portreleri, ana/yardımcı roller, spoiler korumalı biyografiler (`x`) ve Japonca seslendirme sanatçıları. |
| **Kitaplık ve İstatistikler** | İzliyor, tamamlandı, planlanan, beklemede ve bırakıldı durumları; 1–10 puan, özel listeler, favoriler ve `i` tuşuyla açılan grafiksel izleme istatistikleri (durum dağılımı, puan histogramı, favori türler). |
| **Keşfet** | AniList verisiyle haftalık yayın takvimi, bu akşam yayınlananlar, sezon listesi, türe göre listeler, kişisel öneriler ve rastgele öneri. |
| **MyAnimeList** | Durum, puan, izlenen bölüm sayısı ve listelerin iki yönlü eşitlenmesi (OAuth PKCE). AniList `PREQUEL`/`SEQUEL` zinciriyle çok sezonlu serilerde her bölüm MAL'deki kendi kaydına yazılır (seri başına bir kez onayla); eşleme elle düzeltilebilir. |
| **Esnek ve Toplu İndirme** | Seçilen çeviri grubuyla tek bölüm (`d`), çoklu bölüm seçimi (`v`), özel aralık (`30-100`), seçili bölümden sona kadar veya tüm sezon (`D`) indirme; canlı hız grafiği (`▁▂▃▅▆▇`), aria2c, yt-dlp veya dahili indirici. |
| **Görünüm** | Hazır ve özel temalar, kapak görsellerine göre renklenen arayüz, Sixel destekli yüksek çözünürlüklü kapak ve portreler, isteğe bağlı animasyonlar. |
| **Güncelleme** | Kurulu kopya GitHub Releases üzerinden kendini günceller; her dosya SHA-256 ile doğrulanır. |

---

## Kurulum

### Gereksinimler

| Araç | Amaç | Windows kurulumu |
|---|---|---|
| **mpv** | Video oynatıcı (zorunlu) | `winget install mpv-player.mpv-CI.MSVC` |
| yt-dlp | Bazı yedek sunucular, fragmanlar ve HLS indirmeleri | `winget install yt-dlp.yt-dlp` |
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
aniclitr                     Arayüzü başlatır
aniclitr <url>               Verilen anime veya bölüm bağlantısını doğrudan açar
aniclitr doctor [--blame]    Tüm kaynakları canlı test eder (--blame: yerel hata karnesini gösterir)
aniclitr update              Son sürümü kurar
aniclitr --version           Sürümü gösterir
```

---

## Hızlı başlangıç

1. İlk açılışta tanıtım turu başlar: tema seçimi, gerekli araçların denetimi, temel kullanım, MyAnimeList bağlantısı (isteğe bağlı) ve terminalden erişim. Tur **Ayarlar → Tanıtım turu** üzerinden her zaman yeniden açılabilir.
2. Ana sayfadaki arama kutusuna bir anime adı (veya doğrudan desteklenen bir site bağlantısı) yazın ve `enter` tuşuna basın.
3. Bölüm listesinde `enter` çeviri grubunu sorar; `p` kural tabanlı seçiciyle en uygun sunucu ve kalitede doğrudan oynatır.
4. Oynatıcı kapatıldığında kalınan an kaydedilir. Arama kutusu boşken `enter`, son izlenen animeye kalınan yerden devam eder.

Her ekranda `?` tuşu **İşlemler** menüsünü açar. Bu menü o ekrandaki tüm eylemleri kısayollarıyla listeler; bir eylem seçilip `enter` ile doğrudan çalıştırılabilir. Kısayolların ezberlenmesi gerekmez.

---

## Arayüz

Arayüz adım adım ilerler: **menü → liste → öğe → eylem**. `esc` her zaman bir adım geri döner. Her sayfanın başında başlığı ve amacı yazar; liste ile ayrıntı bölgeleri ayrı panellerde gösterilir.

| Ekran | Erişim | İçerik |
|---|---|---|
| **Ana sayfa** | — | Arama kutusu ve ana menü: Devam et, Yeni bölümler, Kitaplık, Keşfet, İndirilenler, Ayarlar, Çıkış. Seçilen öğenin önizlemesi yanda; geniş ekranlarda haftalık takvim ve istatistikler. |
| **Yeni bölümler** | Ana menü | Tüm kaynaklarda son yayınlanan bölümler; `f` yalnızca takip edilenleri gösterir. |
| **Anime** | Arama sonuçları, listeler | Sezonlar, bölümler, çoklu seçim (`v`), izleme durumu, notlar, ayrıntılar, kapak ve `❖ Karakterler & Kadro · c` rozeti. |
| **Karakterler ve Kadro** | Anime ekranında `c` | Karakter listesi, ana/yardımcı rol etiketleri, Sixel/halfblock portreler, spoiler maskeli biyografi (`x`) ve Japonca seslendiren (`v` ile portre geçişi). |
| **Çeviri seçici** | Bölümde `enter` veya `D` | Çeviri grubu → sunucu → kalite; oynatma, tekil indirme veya toplu/aralıklı indirme menüsü (`30-100`). `u` çözülen ham akış bağlantılarını gösterir. |
| **Kitaplık** | `ctrl+b` | Durumlar, özel listeler, puanlar ve `◈ istatistikler` (`i`); liste ya da kapak duvarı görünümü (`v`). |
| **Keşfet** | `ctrl+e` | Önce bölüm seçilir (takvim, bu akşam, bu sezon, türler, öneriler, rastgele), ardından içerik kendi sayfasında açılır. Türler bölümünde önce tür ve sıralama seçilir. |
| **İndirilenler** | `ctrl+d` | Kapaklı çevrimdışı kitaplık; `v` canlı hız grafikli indirme kuyruğunu gösterir. |
| **Ayarlar** | `ctrl+o` | Genel, görünüm, oynatma ve kaynak ayarları (`←` / `→` ile seçenekler arasında iki yönlü geçiş); **Sunucu sırası** ekranı. |

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
| `ctrl+k` | Komut paleti (bulanık arama ve doğrudan bağlantı açma) |
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
| `enter` | Çeviri grubunu ve sunucuyu seç |
| `p` · `space` | Doğrudan oynat (indirilmişse çevrimdışı) |
| `c` | Karakterler ve seslendirenler (Seiyuu) ekranı |
| `tab` · `[` `]` | Sezon değiştir |
| `w` | İzlendi olarak işaretle |
| `v` · `V` | Bölümü toplu indirme için seç (`[✓]`) · seçimi temizle |
| `d` · `D` | Bölümü indir · toplu / aralıklı indirme menüsü (`30-100`, seçili bölümden sona, izlenmeyenler, tüm sezon) |
| `x` | İndirilmiş bölümü (`↓`) diskten sil veya aktif indirmeyi iptal et |
| `N` | Bölüme not ekle |
| `a` | Kitaplık: durum, puan, listeler |
| `t` | Fragman |
| `y` | Benzer animeler |
| `s` | Kaynak seçici: aynı animeyi başka bir kaynakta aç, `1`–`9` ile doğrudan (kalınan bölüm korunur) |
| `r` | Eşleme ve kurtarma: MAL eşlemesini düzelt, eşlemeyi sıfırla, sezon eşitlemesini aç/kapat |

### Karakterler ekranı (`c`)

| Tuş | İşlev |
|---|---|
| `↑` `↓` | Karakter seç |
| `x` · `enter` | Biyografideki gizli spoiler alanlarını (`[SPOILER - x ile göster]`) aç / kapat |
| `v` | Karakter portresi ↔ seslendirme sanatçısı (Seiyuu) portresi arasında geçiş yap |
| `esc` | Anime ekranına dön |

### Oynatma sırasında

mpv penceresinde de geçerlidir: `n` sonraki bölüm, `b` önceki bölüm, `space` duraklat, `←` `→` 10 saniye sar, `u` çevrimiçi altyazı ara (terminalde), `q` çık.

### Kitaplık

`1`–`9`, `0` puan (0 = 10) · `s` durum · `f` favori · `v` liste / kapak duvarı · `i` izleme istatistikleri · `o` sıralama · `/` filtre · `n` yeni liste · `d` kaldır.

---

## Kaynaklar ve yerel çözücüler

| Kaynak | Rol | Yöntem |
|---|---|---|
| **AnimeciX** | Birincil kaynak, Türkçe | JSON API → Tau Video → doğrudan MP4 (HTTP Range doğrulamalı) |
| **AnimeTR** | Ek kaynak, Türkçe (anime ve donghua) | Arama JSON'u ve bölüm sayfası → yerel OK.ru, Vidmoly, Voe, Google Drive çözücüleri |
| **AniHub** | Ek kaynak, Türkçe | REST API (`anihub.com.tr`) → belirteç imzalı doğrudan HLS akışı |
| **SonAnime** | Ek kaynak, Türkçe | JSON API (`api.sonanime.com`) → bölüm başına doğrudan MP4 (1080p/720p/480p); MAL kimliği hazır |
| **Animim** | Ek kaynak, Türkçe | JSON API (`animim.app`) → fansub seçimi, doğrudan MP4 ve ayrı Türkçe altyazı (`.ass`) |
| **HiAnime** | Ek kaynak, İngilizce (altyazılı / dublajlı) | HTML ve gömülü oynatıcı → HLS ve altyazı |
| **TürkAnime arşivi** | Eski seriler, `alt+a` ile etkinleşir | AnimeDepo dizini → üçüncü taraf sunucular |

- **Yerel çözücüler (`internal/extract`):** Tau Video, AniHub HLS, Google Drive, OK.ru, Sibnet, Voe ve Vidmoly bağlantıları harici bir araca gerek kalmadan doğrudan Go içinde çözülür; HLS `#EXT-X-STREAM-INF` manifestlerinden gerçek çözünürlük, bitrate ve codec (`H.264`, `HEVC`, `AV1`) bilgisi okunur.
- **Kural tabanlı seçim ve yedekleme:** Bir sunucu açılmazsa sıradaki sunucu, o çeviri grubunun tüm sunucuları başarısız olursa sıradaki çeviri grubu, tüm gruplar başarısız olursa diğer açık kaynaklardaki karşılığı otomatik olarak denenir. Silinmiş veya telif nedeniyle kaldırılmış bağlantılar kalıcı hata olarak sınıflandırılır ve tekrar denenmez.
- **Birleşik kimlik eşleme:** Aynı anime hangi kaynaktan izlenirse izlensin kitaplıkta ve geçmişte tek kayıt olarak tutulur; eşlenen MAL/AniList kimlikleri `%APPDATA%\aniclitr\mappings.json` dosyasında önbelleğe alınır.
- Başlık çubuğundaki noktalar kaynakların anlık durumunu gösterir: renkli çalışıyor, sarı yavaş, ✗ yanıt vermiyor. Ayrıntılar ve sunucu başarı karnesi **Menü → Kaynak durumu** ekranındadır.
- Keşif ve karakter verileri (takvim, sezon, öneriler, fragmanlar, karakter kadrosu) [AniList](https://anilist.co)'ten alınır ve hesap gerektirmez.

---

## MyAnimeList entegrasyonu

Hesap bağlandığında kitaplık, MyAnimeList listesinin yansıması olur:

- Durum, puan, izlenen bölüm sayısı ve listeler iki yönlü eşitlenir. Listeler MyAnimeList'te etiket olarak saklanır.
- **Çok sezonlu seri hizalaması:** AnimeciX sezonları tek başlık altında toplarken MyAnimeList her sezon için ayrı kayıt tutar. aniclitr, AniList `PREQUEL`/`SEQUEL` zincirini kullanarak izlenen bölümü ait olduğu MyAnimeList kaydına yazar; örneğin AnimeciX'teki Bleach 2. sezon 30. bölüm, MAL'de *Sennen Kessen-hen* 3. kısmın 4. bölümü olur. Başlığın kendi kaydı dışındaki kayıtlara yazmadan önce seri başına bir kez sorulur; emin olunmayan durumlarda (site MAL'den fazla bölüm verirse) hiçbir şey yazılmaz.
- **Elle eşleme:** Yanlış eşleşen bir seri için anime ekranında `r` → **MAL eşlemesini düzelt** ile AniList adaylarından doğrusu seçilir; seçim kalıcıdır.
- **Tutarlı ilerleme ve tamamlanma:** MyAnimeList'te veya yerel kitaplıkta `Tamamlandı` olan bir serinin bölüm ilerlemesi otomatik olarak toplam bölüm sayısıyla uzlaştırılır; henüz tüm bölümleri yüklenmemiş güncel bir sezonda son mevcut bölümü bitirmek seriyi erken `Tamamlandı` durumuna geçirmez.
- Kitaplıktaki durum değişiklikleri `ctrl+z` ile geri alınabilir; geri alma işlemi MyAnimeList'e de gönderilir.
- Çevrimdışıyken yapılan değişiklikler bağlantı kurulduğunda gönderilir.

### Kurulum (bir kez)

1. [myanimelist.net/apiconfig](https://myanimelist.net/apiconfig) adresinde **Create ID** seçeneğini kullanın.
2. **App Type** olarak `other`, **App Redirect URL** olarak `http://localhost:47615/callback` girin. Diğer alanlar serbesttir.
3. Oluşturulan **Client ID** değerini kopyalayın.
4. aniclitr'de **Ayarlar → MyAnimeList** (ya da kitaplıkta `m`) ekranına yapıştırıp `enter` tuşuna basın ve tarayıcıda **Allow** seçeneğini onaylayın.

Parolanız aniclitr'e hiçbir zaman iletilmez (OAuth PKCE). Oturum otomatik olarak yenilenir.

---

## İndirme ve çevrimdışı kullanım

- **Tekil ve çoklu seçim:** Bölüm listesinde `d` imlecin üzerindeki bölümü indirir. `v` tuşuyla istediğiniz bölümleri `[✓]` olarak işaretleyip `D` tuşuna basarak yalnızca seçtiğiniz bölümleri indirebilirsiniz.
- **Aralıklı ve toplu indirme (`D`):** Seçili bölüm yokken `D` tuşuna basıldığında çeviri grubu seçiminin ardından şu seçenekler sunulur:
  - **Seçili bölümden sona kadar indir** (ör. 30. bölümden son bölüme kadar)
  - **Özel bölüm aralığı yazarak indir** (ör. `30-100`, `5-12, 15`, `25-`)
  - **Sadece izlenmeyen bölümleri indir**
  - **Tüm sezonu indir**
- **Seçilen çeviri grubu bağlayıcıdır.** Grup o bölümde yoksa başka bir gruba geçilmez; hata olarak bildirilir.
- **Canlı hız grafiği:** İndirilenler (`ctrl+d` → `v`) ekranında hem toplam hız hem de aktif her indirme satırı için canlı kıvılcım grafiği (`▁▂▃▅▆▇`) gösterilir (**Ayarlar → Görünüm → İndirme hız grafiği** ile kapatılabilir).
- **İndirme araçları:** Doğrudan dosyalar için aria2c (çok bağlantılı, devam ettirilebilir), HLS akışları için yt-dlp; ikisi de yoksa dahili HTTP indirici kullanılır.
- **Bağlantı süresi:** Kaynak bağlantıları süreli olduğundan her iş, sırası geldiğinde güncel bağlantıyla başlar.
- **Dosya düzeni:** `~/Videos/aniclitr/<Anime>/S01E05 [çeviri grubu] [1080p].mp4`. Varsa altyazı aynı adla, kapak `kapak.jpg` olarak aynı klasöre kaydedilir.
- **Çevrimdışı izleme ve yönetim:** İndirilen bölümler listede `↓` ile işaretlenir ve `p` ile internet bağlantısı olmadan oynatılır. Hem bölüm listesinde hem de İndirilenler (`ctrl+d`) rafında `x` tuşuyla tek bir bölümü ya da tüm seriyi diskten silebilirsiniz. Kuyruk geçmişini `c` ile temizlemek çevrimdışı kitaplığı etkilemez; ayrıca klasördeki mevcut videolar otomatik taranarak kitaplığa eklenir.
- Açılışta internet bağlantısı yoksa yalnızca çevrimdışı kitaplık gösterilir; bağlantı geldiğinde ana sayfa otomatik olarak açılır.

---

## Görünüm

- **Temalar:** gece, catppuccin, tokyo night, rosé pine, gruvbox, nord, matcha, şafak (açık tema). `ctrl+t` ile canlı önizlemeyle seçilir.
- **Özel temalar:** `%APPDATA%\aniclitr\themes\<ad>.json` dosyası olarak eklenebilir. Alanlar: `name`, `bg`, `text`, `sub`, `muted`, `faint`, `band`, `surface`, `accent`, `accent2`, `sky`, `ok`, `warn`, `err` ve isteğe bağlı `light`.
- **Kapaktan tema:** Bir anime açıldığında arayüz kapak görselinin renklerini alır.
- **Kapak ve karakter görselleri:** Sixel destekleyen terminallerde tam çözünürlükte, diğerlerinde renkli blok karakterlerle çizilir.
- **Animasyonlar:** Logo girişi, arka plan efektleri, geçişler. **Ayarlar → Animasyonlar** ya da `ANICLITR_REDUCED_MOTION=1` ile kapatılabilir.
- Oynatma sırasında arayüz boşta bekler ve işlemci kullanmaz.

---

## Yapılandırma

Ayarların tamamı uygulama içinden (`ctrl+o`) değiştirilebilir ve `%APPDATA%\aniclitr\config.json` dosyasında saklanır.

| Anahtar | Açıklama |
|---|---|
| `theme`, `theme_bg`, `cover_theme` | Tema, terminal arka planını boyama, kapaktan tema |
| `ambient`, `intro`, `motion`, `sounds` | Arka plan efekti, logo efekti, animasyonlar, ses düzeyi |
| `posters`, `graphics`, `speed_graph` | Kapak görselleri (`auto`, `sixel`, `blocks`), indirme hız grafiği |
| `quality` | Kalite: `best`, `1080p`, `720p`, `480p` |
| `auto_skip`, `auto_next`, `fullscreen`, `cross_source_fallback` | Oynatma davranışı ve kaynaklar arası otomatik yedekleme |
| `sources`, `legacy` | Etkin kaynaklar (`animecix`, `animetr`, `anihub`, `sonanime`, `animim`, `hianime`, `animedepo`), arşiv modu. Ayarlarda kapalı bir kaynakta izlenen bir seri açılırsa önce açık kaynaklarda aranır; yalnızca orada varsa işaretlenerek açılır. |
| `host_order`, `never_hosts`, `only_hosts` | Sunucu sırası ve otomatik seçim kuralları (**Ayarlar → Sunucu sırası**); `asla`/`yalnızca` uygun sunucu açılmazsa diğerlerine dönülür |
| `season_sync` | Seri başına MAL sezon eşitlemesi kararı |
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
| `mappings.json` | Kaynak ve MAL/AniList kimlik eşleme önbelleği |
| `diag.json` | Kaynak ve sunucu sağlık/başarı istatistikleri |
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
| Oynatma başlamıyor | Başka bir sunucu ve kaynak otomatik olarak denenir. Anime ekranında `r` tuşuyla kurtarma menüsünü açarak diğer kaynaklarda arayabilir veya eşlemeyi sıfırlayabilirsiniz. |
| Kapaklar bulanık | Windows Terminal 1.22 veya üstünü kullanın; **Ayarlar → Görsel kalitesi → sixel**. |
| Bir kaynak yanıt vermiyor | `aniclitr doctor --blame` komutunu çalıştırın ya da **Menü → Kaynak durumu** ekranına bakın. |
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

aniclitr; AnimeciX, AnimeTR, AniHub, HiAnime, MyAnimeList, AniList ya da AniSkip ile bağlantılı değildir ve bu hizmetler tarafından desteklenmemektedir.
