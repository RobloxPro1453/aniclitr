# Değişiklik günlüğü

Bu projedeki önemli değişiklikler bu dosyada belgelenir. Sürüm numaraları [Anlamsal Sürümleme](https://semver.org/lang/tr/) kurallarını izler. Uygulama içindeki "Yenilikler" kartı aynı bilgiyi özet olarak gösterir. 0.11.0 öncesindeki sürümler GitHub üzerinden yayımlanmamıştır.

## [1.3.0]

### Eklenenler
- **Tokyo Studio `mpv` Kontrol Merkezi (`m` / `Tab` / `Alt+4`):** Şişirilmiş mor tasarım yerine keskin mimari köşeli, mat obsidyen (`#0B0B0D`), kırmızı-vermilion imza şeritli, iki katmanlı alt-menülü konsol (`Çözünürlük (FSR)`, `Kalite & Akıcılık`, `Altyazı & Senkron`, `Ses & Oynatma`, `Bölüm Geçişi`). Alt menüdeyken `Esc` / `Backspace` üst menüye döner, menü açıkken `mpv` üst-sol metin bildirimi çakışmaz, `repeatable` `↑↓←→` ve `<1 ms` tepkiyle çalışır.
- **2 Bağımsız Açık Kaynak (MIT) Video Motoru (`internal/shaders` — `//go:embed`):**
  1. **Gerçek Çözünürlük Yükseltme (AI'sız · AMD FidelityFX FSR v1.0.2, MIT):** `Kapalı`, `Otomatik (monitör)`, `1080p (1920×1080)`, `2K (2560×1440)` ve `4K (3840×2160)` gerçek GPU hedef ölçeklemesi.
  2. **Kalite İyileştirici (Resmi Bloc97/Anime4K v4.0.1 CNN, MIT) & Akıcılık / Frame-Gen (`mpv` GPU):** Piksel piksel keskinleştirmek yerine cam gibi pürüzsüz çizgi onarımı (`Hızlı CNN-S`, `Cam Gibi CNN-Soft-M`, `Sinema Çift CNN`) + bağımsız `Akıcı (Smooth Motion · oversample)` ve `60+ FPS (Catmull-Rom)` zamansal ara kare üretimi (`--hwdec=auto-safe` sıfır kopya).
- **Tam Arayüz Discord Rich Presence & Duraklatma Koruması (`internal/discord`, `app.go`, `playing.go`):** Yalnızca izlerken değil, arayüzde gezinirken de (Ana Sayfa, Arama, Anime kapaklı bölüm inceleme, Keşfet, Kitaplık, Torrent, İndirilenler, Anime Özeti) canlı durum gösterilir; video duraklatıldığında otomatik yeniden bağlanma/yeniden denemeyle etkinlik asla kaybolmaz (`❚❚ Duraklatıldı · S1 B1 (02:19)`).
- **İnsanüstü Performans Optimizasyonu, Çift-Hızlı 60/20 FPS Saat & UX Psikolojisi (`internal/tty`, `internal/ui`, `internal/netx`, `internal/download`):**
  - `tty.Frame.setLine` ASCII/Türkçe/kutu hızlı yolu (`331 µs → 100 µs`, **3,3× hızlı**, `0 allocs/op`) ve sıfır-ayırımlı `tty.Diff` (`24 → 2 allocs/op`).
  - `theme.go` / `layout.go` doğrudan TrueColor ANSI yazıcısı + `poster.go` sıfır-ayırımlı RGB kaçışları (tam `140×40` TrueColor kare + diff **< 1 ms**).
  - Fare takibi `\x1b[?1003h` → `\x1b[?1000h` + `MouseActionMotion` sıfır yeniden çizim (fare kıpırdatınca `0 byte` yazım).
  - **Çift-hızlı uyarlamalı saat:** Etkileşim/geçiş/kaydırmada **60 FPS**, Ana Sayfa sakin ambiyansında (yağmur/sakura/kar/yıldız/logo ışıltısı bozulmadan!) ultra hafif **20 FPS**, ambiyans kapalıyken / diğer boş ekranlarda / `mpv` oynarken **0 FPS (%0 CPU)**.
  - Kademeli açılış (`download.OpenLazy` + 320 ms `deferredInitMsg`), HTTP/2 bağlantı havuzu (`netx`), Doherty eşiğine göre pürüzsüz `125ms` geçişler, hızlı tuş tekrarında anında kilitlenen akıllı `glide` ve Ana Sayfa istatistiğinde eşitlenmiş MAL/AniList kitaplığındaki gerçek toplam izlenen bölüm sayısı.

### Düzeltilenler
- **Sonraki bölüm geçişinde `mpv` penceresinin gelmemesi (`internal/player/mpv.go`, `playing.go`):** `--force-window=immediate` ile `mpv` penceresi tamponlama beklenmeden anında açılır; terminal gerçek video akışı başlamadan "oynatılıyor" demez; sessizce kapanan bozuk akışlar otomatik yedek sunucu kurtarmasını tetikler ve süresi dolmuş ön-yükleme bağlantıları elenir.
- **Tüm menü ve kategorilerde eksiksiz akıllı arama (`internal/match/rank.go`, `internal/nyaa`, `results.go`, `suggest.go`, `cli.go`, `library.go`):** Tek birleşik `match.RankTitle` motoru tüm arama yüzeylerine yayıldı. Nyaa.si'de RSS (son 75 yükleme) ilgisiz serilerle dolduğunda (ör. İngilizce sekmesinde `another` aramasını *"In Another World"* serilerinin gizlemesi), otomatik TF-DF gürültü ayıklamalı `s=seeders&o=desc` ikinci geçişi devreye girerek hiçbir sonucu kaybetmeden tam eşleşmeyi (`Another`) 1. sıraya taşır.
- **Discord Rich Presence (`internal/discord`, `playing.go`, `config.go`):** `status_display_type: 2` ile üye listesinde `mpv Media Player` yerine doğrudan izlediğin **anime adı** gösterilir; canlı kalan süre ilerleme çubuğu (`start` + `end`), duraklatma durumu (`❚❚ Duraklatıldı`) ve kart başlığını özelleştirmek isteyenler için isteğe bağlı `config.json → discord_app_id` eklendi.

## [1.2.1]

### Eklenenler
- **Gömülü BitTorrent akışı & dosya seçici (`internal/tstream`, `torrentui.go`):** Harici torrent istemcisine veya Windows `magnet:` uyarısına gerek kalmadan `enter` ile torrentin video listesi açılır; seçilen tek bölüm yerel HTTP Range üzerinden indirmeyi beklemeden doğrudan `mpv` ile anlık izlenir.
- **İndirilenler kuyruğuna bağlı torrent indirme (`internal/download/torrent.go`):** Torrent ekranında `d` (seçili bölüm veya torrent) ve `D` (tüm paket) artık kör arka plan `aria2c` yerine doğrudan `ctrl+d` **İndirilenler** kuyruğuna ve üst çubuktaki canlı hız/yüzde dalgasına bağlanır.
- **Akıllı torrent sınıflandırıcı & otomatik klasörleme (`internal/download/organizer.go`):** Sıfır modelle çalışan ayrıştırıcı, ham torrent dosyalarını (`ReZero.kara...S04E18.1080p.WEB-DL.mkv`) mevcut klasörlerin ve kitaplığınla eşleştirip `<Seri Adı>/S04E18 [Grup] [1080p].mkv` biçiminde klasörler, `.mkv` dosyalarını `ctrl+d` çevrimdışı kitaplığına bağlar ve kök dizinde kalmış eski dağınık torrent videolarını açılışta otomatik düzenler.

### Düzeltilenler
- **Discord Rich Presence sıfır-ayar düzeltmesi (`internal/discord`):** Windows named pipe bağlantısı `go-winio` (`winio.DialPipe`) üzerine taşındı, geçersiz yer tutucu App ID yerine kayıtlı genel App ID tanımlandı, el sıkışma/hata yanıtı doğrulaması ve `aniclitr.log` kaydı eklendi — ekstra Discord ayarı gerekmeden doğrudan çalışır.

## [1.2.0]

### Eklenenler
- **Anime4K GPU Görüntü İyileştirme (`internal/shaders`):** Gömülü, iGPU dostu 4 kademeli GLSL shader motoru (`kapalı`, `hafif`, `dengeli`, `yüksek`) — 5-tap CAS keskinleştirme, kenar koruyan bilateral gürültü temizleyici, çizgi onarım ve yönlü 2× luma yükseltme; **Ayarlar → Oynatma → Anime4K** üzerinden seçilebilir ve video izlerken `mpv` içinde **`Alt+4`** ile yeniden başlatmadan canlı değiştirilebilir.
- **Canlı Animasyonlu Anime Özeti / Wrapped (`internal/wrapped`, `wrappedui.go`):** Kitaplık ve geçmiş verinden 6 slaytlık animasyonlu hikâye (sayaç açılışı, 7 günlük izleme ritmi, tür çubukları, zirve podyumu, 8 arketipli sıfır-model Türkçe kişilik/yorum motoru ve **`e`** ile tek tuşla `900×1200` paylaşılabilir PNG kart); **Kitaplık → `W`**, `ctrl+k` veya CLI'dan `aniclitr ozet [--png]` ile açılır.
- **Birlikte İzle / Watch Party (`internal/watchparty`, `partyui.go`):** Hesap ve harici sunucu gerektirmeyen sıfır bağımlılık TCP oda kurma (`h`) / katılma (`j`); duraklatma ve sarma işlemlerini `mpv` pencereleri arasında 1,5 sn kayma korumasıyla anlık eşitler (`ctrl+k` veya oynatırken `P`), isteğe bağlı Syncplay algılar.
- **Uyku Zamanlayıcısı (`internal/ui/sleep.go`):** `mpv` içinde **`Alt+s`**, terminal oynatma ekranında **`T`** veya `ctrl+k` üzerinden döngü (`Kapalı → Bu bölüm bitince dur → 2 bölüm → 30 dk → 60 dk → Bölüm bitince PC'yi uyut`); yalnızca etkinken oynatılıyor kartında sade `☾` rozeti görünür.
- **Sıfır Bekleme Sonraki Bölüm Ön-Hazırlama:** Bölüm %70'i geçtiğinde veya son 3 dakikaya girildiğinde sıradaki (filler olmayan) bölümün video akışı arka planda sessizce çözülür; geçişte seçici ekranı atlanır ve `mpv` anında açılır.
- **Keşfet "Ruh Haline Göre" Sihirbazı (`internal/anilist/moods.go`):** **Keşfet → Öneriler** içine 6 küratörlü ruh hali modu eklendi (`m` veya `[` `]` ile geçiş: *Kısa & Akıcı*, *Gizli Cevherler*, *Saf Aksiyon*, *Ağlatan Dram*, *Kafa Boşaltmalık*, *Zihin Yakan Gizem*).
- **Tek Tuşla Otomatik Araç Kurulumu (`internal/tools`, `aniclitr kur`):** Eksik `mpv`, `yt-dlp`, `aria2c`, `ffmpeg` araçlarını ilk kurulum turunda (`i`), Ayarlar'da veya terminalden `aniclitr kur` ile otomatik indirip kurar (`yt-dlp` doğrudan taşınabilir olarak, diğerleri sessiz `winget`/`brew`).
- **Winget Otomatik Senkronizasyon (`.github/workflows/release.yml`, `packaging/winget/`):** Her yeni `vX.Y.Z` GitHub Release etiketinde isteğe bağlı `WINGET_TOKEN` üzerinden `microsoft/winget-pkgs` güncelleme akışı eklendi.

### Düzeltilenler
- **Akıllı tam-eşleşme arama sıralaması (`results.go`, `internal/nyaa`):** Hem normal kaynak aramasında hem Nyaa.si torrent aramasında tam/önek başlık eşleşmeleri (ör. `another` aramasında **Another**) alakasız uzun başlıklı serilerin (*"...in Another World"*) üstünde sıralanır.

## [1.0.1]

### Eklenenler
- **Nyaa.si `Raw (Ham)` kategorisi & gelişmiş filtreler:** Torrent ekranına `4 Raw (Ham)` kategorisi eklendi; `o` ile sıralama (`seed ↓`, `en yeni`, `en çok indirilen`, `boyut ↓/↑`), `q` ile kalite/paket süzgeci (`tümü`, `1080p`, `720p`, `2160p`, `paket`), `f` ile remake hariç ve `c` ile magnet bağlantısını tek tuşla panoya kopyalama eklendi. CLI'da `aniclitr torrent <ad> --raw|--tr|--en|--trusted` bayrakları desteklenir.
- **2 sütunlu Nova Torrent arayüzü (`torrentui.go`):** Geniş terminallerde solda rozetli torrent listesi (`★`, `RAW`, `TR`, `PKT`, `1080p`, `Seed`), sağda tam başlık, codec/çözünürlük hapları, seed sağlık çubuğu, `aria2c` hazırlık durumu ve tüm eylem kısayollarını gösteren ayrıntı kartı.
- **Görünür Torrent giriş noktaları:** `✦ Torrent` artık doğrudan **Ana Sayfa** alt menüsünde (klasik ve Nova), **Anime sayfasında** (`T Torrent` tuş kapağı ve `F1 işlemler`), **≡ Menü** içinde ve **Yardım** penceresinde açıkça görünür.

## [1.0.0]

`aniclitr`'in ilk kararlı `1.0.0` sürümü — tüm temel arayüz, altyazı, eşitleme, torrent, CLI ve ekosistem özellikleri bir araya getirildi.

### Eklenenler
- **Nova artık varsayılan arayüz:** Yeni kurulumlarda modern Nova arayüzü (tüm ana ekranlar, 3 sütunlu Keşfet, Sixel katman kararlılığı, tuş kapakları) doğrudan etkin gelir; klasik görünüm **Ayarlar → Görünüm → Arayüz → klasik** ile her zaman seçilebilir.
- **AniList iki yönlü OAuth eşitleme (`internal/anilist/sync.go`, `anilistui.go`):** **Ayarlar → AniList** üzerinden OAuth (veya erişim jetonu) ile bağlanıp durum, puan, bölüm ilerlemesi ve özel listeleri iki yönlü eşitleme; AniList'te `izliyor` olan serilerin ana sayfa "Devam et" kartlarında görünmesi.
- **Nyaa.si torrent desteği (`internal/nyaa`, `torrentui.go`):** Açık Nyaa.si RSS akışı üzerinden hızlı anime torrent araması (`alt+t`, anime ekranında `T` veya `aniclitr torrent <ad> [--magnet N]`), Türkçe/Güvenilir filtreleri (`1`–`4`), `aria2c` ile doğrudan indirme (`d`) ve `webtorrent --mpv` / sistem istemcisiyle açma (`enter`).
- **Discord Rich Presence (`internal/discord`):** Sıfır bağımlılık yerel IPC üzerinden izlediğin animeyi, bölümü ve kapak görselini Discord profilinde gösterme (**Ayarlar → Genel → Discord Rich Presence**).
- **Masaüstü bildirimleri (`internal/notify`):** Takip ettiğin serilerin yeni bölümü çıktığında veya indirme tamamlandığında Windows/Linux/macOS yerel bildirimi (**Ayarlar → Genel → Masaüstü bildirimleri**).
- **Kişisel kısayol dosyası (`keys.json`) ve `aniclitr indir <ad> <aralık>` CLI:** `%APPDATA%\aniclitr\keys.json` üzerinden genel kısayolları özelleştirme ve terminalden doğrudan aralık indirme başlatma.

### Değişenler
- Tüm proje belgeleri (`README.md`, `ROADMAP.md`, `docs/ARCHITECTURE.md`, `docs/DESIGN.md`) `1.0.0` sürümüne göre güncellendi.

## [0.20.0]

### Eklenenler
- **`mpv` içi OSD altyazı seçici (`y`):** Oynatma sırasında Alt-Tab yapmadan doğrudan `mpv` penceresinde `y` tuşuna basınca videonun üzerinde şeffaf ASS AniSub altyazı menüsü açılır (`↑`/`↓` veya `1`–`8` ve `Enter` ile anında yükler, `Esc` menüyü kapatır). `mpv`'nin dahili `u` kısayoluyla çakışma tamamen giderildi.
- **Altyazı senkron ince ayarı (fine-tuning):** Hem `mpv` penceresinde hem terminalde `z`/`x` (`±0,1 sn`), `Shift+Z`/`Shift+X` (`±0,5 sn`) ve `Alt+0`/`0` (sıfırla) ile kaymış altyazıları canlı hizalama; `mpv`'de Türkçe OSD bildirimi ve Nova oynatılıyor ekranında aktif senkron hapı.
- **Hızlı CLI alt komutları (`internal/cli`):** `aniclitr devam [--list]`, `aniclitr ara <ad>` (TUI açmadan paralel kaynak araması), `aniclitr izle <ad> [bölüm]`.
- **Taşınabilir tek dosya yedekleme (`internal/backup`):** `aniclitr yedekle [dosya.json]` (+ **Ayarlar → Genel → Verileri yedekle**) ile ayarlar, geçmiş, kitaplık ve eşlemeler tek JSON dosyasına paketlenir (OAuth jetonları dahil edilmez); `aniclitr geri-yukle <dosya.json>` geri yüklemeden önce otomatik `pre-restore-*` güvenlik yedeği alır.
- **Ekran geçmişinde ileri/geri:** `alt+←` ve `alt+→` ile ekranlar arasında tarayıcı gibi hızlı geçiş.

### Değişenler
- **Altyazı kartı yenilendi (`subsui.go`):** Çerçeveli panel, üstte canlı senkron çubuğu, numaralı hızlı seçim satırları (`1`–`6`), renkli dil/sağlayıcı etiketleri ve temiz yerel dosya girişi.
- **Gerçek Sixel kapaklı README görselleri:** `docs/media/` içindeki ekran görüntüleri ve `demo.gif`, gerçek anime kapak görselleriyle ve tam `gece` zemin rengiyle güncellendi.

## [0.19.0]

### Eklenenler
- **AniSub Türkçe altyazı desteği (`internal/subs/anisub.go`):** Oynatma sırasında `u` tuşuna basıldığında [AniSub](https://anisub.co/) topluluk arşivi hiçbir API anahtarı gerektirmeden aranır; `.zip` paketlerinden doğrudan ilgili bölümün `.ass`/`.srt`/`.vtt` dosyası çıkarılıp çalışan `mpv` penceresine anında yüklenir. OpenSubtitles anahtarı tanımlıysa iki sağlayıcı paralel aranır.
- **Yerel altyazı dosyası yükleme (`u` → `l`):** Oynatma sırasında bilgisayardaki herhangi bir `.ass`, `.srt`, `.ssa` veya `.vtt` dosyasının yolunu yapıştırıp oynatmayı kesmeden `mpv`'ye ekleyebilirsiniz.
- **Kaynak OP/ED zamanları:** `Animim` ve `HiAnime` kaynaklarının sağladığı açılış/bitiş (intro/outro) zaman damgaları AniSkip'e tamamlayıcı/yedek olarak bağlandı; AniSkip kaydı olmayan bölümlerde de otomatik atlama çalışır.
- **HiAnime filler rozeti ve isteğe bağlı atlama:** Filler işaretli bölümler klasik ve Nova bölüm listesinde `filler` etiketiyle görünür; **Ayarlar → Oynatma → Filler bölümleri atla** (`skip_filler`) açıldığında `n` ve otomatik sonraki bölüm geçişi filler bölümleri atlar.
- **Görselli yeni README:** Gerçek Nova ekran görüntüleri, animasyonlu demo GIF ve kapsamlı Teşekkürler bölümüyle `README.md` baştan yenilendi.

## [0.18.0]

### Eklenenler
- **Keşfet ekranı baştan tasarlandı (`nova_discover.go`):** Üç sütunlu stüdyo yerleşimi (sol navigasyon şeridi + orta odak sahnesi + sağ kapaklı ayrıntı kartı), 2 sütunlu tür seçici ızgara, etkileşimli süre hapları (`Bu akşam`), dar terminallerde tam ekran katlanma.
- **MAL "Devam et" köprüsü:** MyAnimeList listesinde `izliyor` durumunda olup henüz `aniclitr`'de açılmamış seriler artık ana sayfadaki "Devam et" kartlarında görünür ve `enter` ile doğrudan açık kaynaklarda aranıp kaldığın bölümden açılır.
- **Seri bazında kalite hafızası:** Her seri için son izlenen kalite (`1080p`, `720p` vb.) geçmişte ayrı hatırlanır ve otomatik oynatmada o seri için öncelikli seçilir.

### Değişenler
- **Keşfet klavye akışı düzeltildi:** `←` / `→` artık bölüm içinde habersiz sekme değiştirmiyor; `→`/`enter` sol şeritten seçili bölüme girer, `←`/`esc` temizce bir adım geri döner (`tab` / `1-6` hızlı geçiş olarak kalır).
- **Türkçe klavye uyumu (`F1 işlemler`):** Alt çubuktaki işlemler ipucu her ekranda `?` yerine tek tuş olan `F1` olarak gösterilir (`?` yedek kısayol olarak çalışmaya devam eder).

## [0.17.0]

### Eklenenler
- **Nova arayüzü tüm ana ekranlarda:** Ana sayfa ve üst/alt çubuğa ek olarak **Anime** (`nova_anime.go`), **Çeviri/Sunucu Seçici** (`nova_picker.go`), **Oynatılıyor** (`nova_playing.go`), **Kitaplık** ve **Keşfet** ekranları Nova tasarım diline (kapaktan renkli paneller, tuş kapakları, dar ekranda akıllı katlanma) geçti; klasik arayüz ayarlarda korunur.
- **İndirilenler depolama alanı göstergesi:** İndirilenler ekranında (`ctrl+d`) hem `aniclitr`'in diskte kapladığı alanı hem de sürücüdeki kalan boş alanı renkli bir çubukla gösterir (düşük disk alanında uyarı rengiyle); Windows, Linux ve macOS uyumludur.
- **Veri katmanı test zırhı:** `internal/store` (geçmiş ve kitaplık) ile `internal/config` için atomik yazım, `.bozuk-*` kurtarma ve MAL kayıt benimseme birim testleri eklendi.

### Düzeltilenler
- **Sixel kapak kayması / ışınlanması:** Arama öneri kutusu, açılır menüler ve yazma kıvılcımları (`overlayAt`, `overlay`, `confetti.draw`) artık Sixel işaretçilerini (`\x1b_kzN\x1b\\`) metin kesme sırasında kutu kenarına ışınlamıyor; kutu altındaki kapak temizce gizlenir, yanındaki kapaklar tam sütununda kalır ve kutu kapanınca yerine döner.
- **Nova ana sayfa kırpmaları:** Tek "Devam et" kartı varken bölüm detayının kesilmesi (`%2.`) ve geniş sayılarda istatistik satırının taşması düzeltildi.

## [0.16.1]

### Eklenenler
- **Yeni arayüz (Nova, deneme aşamasında).** **Ayarlar → Görünüm → Arayüz → yeni** ile açılır; klasik arayüz varsayılan kalır. Yalnızca görünüm ayrılır, veri ve tuşlar ortaktır (`internal/ui/nova_*.go`).
  - Üst çubuk: indirme sürerken canlı hız dalgası, hız ve yüzde; altındaki çizgi indirme ilerlemesiyle dolar. Kaynaklar kısa adlarıyla görünür: açık olan renkli, kapalı olan soluk, yavaş sarı, yanıt vermeyen kırmızı `✗`. Dar terminalde noktalara iner.
  - Alt çubuk: tuş kapakları; bildirimler türüne göre renkli, yuvarlak uçlu, kayarak gelip sönen bir hap (`+N` ile birden fazlası).
  - Ana sayfa: kapak kartları (`←` `→`), seçili kart kendi renginde çerçeveli; her kartın altında ilerleme çizgisi. Yüksek ekranda alt bant: kaldığın anın görüntüsü, takip ettiklerinden yaklaşan bölümler (gün, saat, bölüm), sade istatistik cümleleri. Varış yerleri tek satırda.

### Değişenler
- Bildirimler, işlemler (`?`), kaynak seçici ve arama önerileri açıkken artık bütün resimler gizlenmiyor; yalnızca kutunun altında kalan resim gizlenir. Ekranı karartan pencerelerde (yardım, komut paleti) davranış aynı.
- Ana menüde İndirilenler seri sayısını gösterir ("3 seri"); bölüm sayısı İndirilenler ekranındadır.
- README'deki sürüm rozetinin önbellek süresi kısaltıldı.

## [0.16.0]

### Eklenenler
- **Yeni kaynaklar: SonAnime ve Animim** (`internal/source/sonanime`, `internal/source/animim`). SonAnime bölüm başına doğrudan MP4 (1080p/720p/480p) ve hazır MAL kimliği sunar; Animim fansub seçimi, doğrudan MP4 ve ayrı Türkçe altyazı (`.ass`) sunar. İkisinde de sağlık yoklaması, canlı uçtan uca test ve kayıtlı yanıt testleri bulunur.
- **Kaynak seçici (`s`).** Anime ekranında ortada açılan, başlatıcı (launcher) tarzı liste: bilinen kaynaklar hemen, diğerleri arka planda aranarak canlı dolar; `1`–`9` ile istenen kaynağa doğrudan geçilir. Bölüm listesindeki uzun kaynak çubuğu tek satırlık bir özete indi.
- **Çok sezonlu MAL eşitlemesi** (`internal/ui/seasonsync.go`). AniList sezon zinciri uygulamaya bağlandı: izlenen bölüm MAL'de ait olduğu kayda yazılır, bölüm listesindeki ✓ işaretleri de o kayıttan okunur. Başlığın kendi kaydı dışındaki kayıtlara yazmadan önce seri başına bir kez onay istenir; "evet" geçmişte izlenenleri de aktarır.
- **Elle MAL eşlemesi.** Anime ekranında `r`: AniList adaylarından doğru kaydı seçme, eşlemeyi sıfırlama, sezon eşitlemesini açıp kapatma.
- **Ayarlar → Sunucu sırası.** Sunucuların deneme sırası; `asla` (`never_hosts`) ve `yalnızca` (`only_hosts`) işaretleri.
- **Sunucu karnesi.** Fansub seçicide çözülmemiş sunucuların geçmiş başarı oranı ve ortalama süresi; bu oturumda düşenler ayrıca işaretlenir.
- **Kaynak sağlığı.** AnimeciX, AnimeTR ve HiAnime için arama + bölüm listesi yoklaması; site yapısı değişirse "yanıt vermiyor" yerine ayrı bir "yapısı değişmiş" uyarısı. Kaynaklar ekranındaki tanılama son 24 saati gösterir.
- **AnimeciX fansub adları.** Sitenin çevirmen listesi günde bir kez alınır; serbest künye satırları grubun gerçek adına çevrilir, dublaj grupları işaretlenir.

### Değişenler
- Ayarlarda kapalı bir kaynakta izlenen seri açılınca önce açık kaynaklarda aranır ve bulunursa kaldığın yerden orada açılır; yalnızca kapalı kaynakta varsa işaretlenerek açılır. Yapıştırılan bağlantı ve açık "şurada aç" seçimi yönlendirilmez.
- Varsayılan sunucu dışlaması (Vidmoly) kaldırıldı; sırada en sonda olduğu için yalnızca başka sunucu açılmazsa seçilir.
- Varyantlar artık türünü (`embed`/`hls`/`mp4`), dilini ve dublaj olup olmadığını taşır; seçicide dublaj grupları altyazılılardan sonra gelir.
- README'deki `preferred_host` anahtarı, gerçek adı olan `host_order` olarak düzeltildi.

### Düzeltilenler
- **AniHub sessiz oynatma:** ayrı ses kanalı olan HLS akışlarında seçilen kalite sesi içermiyordu; oynatma ve indirme sessizdi. Kalite artık ana liste üzerinden seçilir (`--hls-bitrate`), indirmede Japonca ses birleştirilir.
- **Yanlış dosya boyutları:** Tau büyük dosyalar için hep 500.000.000 bayt (477 MB) bildiriyordu; artık sunucunun gerçek boyutu gösterilir.
- **Ölü MP4'ler:** her çözümlemede yoklanır; tümü ölüyse sıradaki sunucuya geçilir. Zaman aşımı bozuk sayılmaz.
- **Bleach türü sayı hataları:** tamamlanan bir seride MAL'e sitenin bölüm sayısı (412) yerine MAL kaydının sayısı (366) gönderilir.
- **Ara bölümler (12.5):** HiAnime bunları 0. bölüm, AnimeTR 12. bölümle aynı sayıyordu, AniHub tamsayıya kırpıyordu; artık kendi yerlerinde gösterilir ve ilerleme sayılmaz.
- **HiAnime filler bölümleri** listeden sessizce düşüyordu.
- İndirilen altyazılar kendi uzantısıyla (`.ass`, `.srt`) kaydedilir; eskiden hep `.vtt`.
- Tanılama kayıtlarında saatlik kovaların budanmasındaki bir hata giderildi.

## [0.15.1]

### Eklenenler
- **Otomatik çevrimdışı klasör taraması (`internal/download`)**: Uygulama açılışında ve İndirilenler ekranında `download_dir` (`~/Videos/aniclitr`) klasörü otomatik taranır; kuyruk geçmişi temizlenmiş olsa bile diskteki mevcut `.mp4` bölümleri ve kapakları otomatik olarak **Çevrimdışı Kitaplık** rafına geri kazandırılır.
- **Hızlı diskten silme (`x` / `X`)**:
  - **İndirilenler ana rafında (`ctrl+d`)**: Bir serinin kapağı üzerindeyken `x` veya `delete` tuşuna iki kez basarak tüm seriyi bütün bölümleriyle birlikte diskten silebilirsiniz.
  - **Normal anime bölüm listesinde (`animeScreen`)**: İndirilmiş (`↓`) bir bölümün üzerindeyken `x` tuşuyla o bölümü doğrudan diskten silebilir veya devam eden indirmeyi iptal edebilirsiniz.
  - Bir serinin son bölümü silindiğinde boş kalan klasörü ve `kapak.jpg` dosyası otomatik temizlenir.

### Düzeltilenler
- **İndirme kuyruğunu temizleyince (`c`) çevrimdışı kitaplığın boşalması (`internal/download`, `internal/ui/downloads.go`)**: Kuyruk ekranında `c` (*bitenleri temizle*) veya `x` (*sıradan gizle*) tuşuna basıldığında tamamlanmış indirmelerin `downloads.json` indeksinden silinip Çevrimdışı Kitaplık'tan kaybolmasına yol açan hata giderildi (`Job.Cleared` ayrımı).

## [0.15.0]

### Eklenenler
- **Karakterler ve Seslendirenler ekranı (`internal/ui/characters.go`, `internal/anilist`)**: Anime detay sayfasında `❖ Karakterler & Kadro · c` rozeti ve `c` kısayoluyla açılan yeni ekran eklendi. Karakterlerin Sixel/halfblock portreleri, ana/yardımcı rol etiketleri, biyografileri (`x` ile spoiler açma/kapatma), Japonca seslendirme sanatçıları (Seiyuu) ve `v` ile karakter ↔ seiyuu portre geçişi desteklenir.
- **Çoklu seçim ve aralıkla toplu bölüm indirme (`internal/ui/anime.go`, `internal/ui/picker.go`)**: Bölüm listesinde `v` ile tekil bölümleri `[✓]` olarak işaretleme ve `D` tuşuyla açılan toplu indirme menüsü (*Seçili bölümden sona kadar*, *Özel bölüm aralığı `30-100`*, *Sadece izlenmeyen bölümler*, *Tüm sezon*) eklendi.
- **Kitaplık İstatistikleri (`internal/ui/library.go`)**: Kitaplık sol menüsüne ve `i` kısayoluna `◈ istatistikler` görünümü eklendi; toplam bölüm/saat/gün özeti, renkli durum dağılım çubuğu, 1–10 puan histogramı ve en çok izlenen türler grafiği gösterilir.
- **Canlı indirme hız grafiği (`internal/ui/downloads.go`)**: İndirilenler ekranı üst bilgisinde ve aktif indirme satırlarında anlık hız geçmişi mini kıvılcım grafiği (`▁▂▃▅▆▇`) olarak çizilir; **Ayarlar → Görünüm → İndirme hız grafiği** ile kapatılabilir.

### Düzeltilenler
- **MAL ve yerel kitaplık bölüm/tamamlanma tutarsızlığı (`internal/store/library.go`, `internal/malsync`, `internal/ui/same.go`, `internal/ui/playing.go`)**: `Tamamlandı` durumundaki bir seride `15/16 izlendi` görünmesi veya eksik yüklenmiş sezonda son mevcut bölüm izlendiğinde serinin erken `Tamamlandı` olması düzeltildi (`Item.Reconcile`, `watchedCount` ve `watchView` senkronizasyonu).
- **Ayarlar sol/sağ ok yönü (`internal/ui/settings.go`)**: Çok seçenekli ayarlarda sol okun (`←` / `h`) geri gitmek yerine ileri (`+1`) gitmesine yol açan yön hatası giderildi.

## [0.14.0]

### Eklenenler
- **Yeni kaynak: AniHub (`internal/source/anihub`)**: `anihub.com.tr` REST API'si ve belirteç imzalı doğrudan HLS akış desteği eklendi.
- **Yerel OK.ru, Vidmoly ve Voe çözücüleri (`internal/extract`)**: `OK.ru` (`data-options` metadata), `Vidmoly` (HLS manifest) ve `Voe` (6 adımlı şifre çözme hattı + JS yönlendirme takibi) artık `yt-dlp`'ye düşmeden yerel Go koduyla anında çözülür.
- **Adresten doğrudan açma (`source.URLOpener`)**: Arama kutusuna, komut paletine (`ctrl+k`) veya komut satırına (`aniclitr https://…`) yapıştırılan AnimeciX, AnimeTR, AniHub ve HiAnime bağlantıları doğrudan ilgili anime/bölüm ekranında açılır.
- **Kaynaklar arası otomatik yedek ve kurtarma menüsü (`r`)**: Bir bölümün tüm sunucuları başarısız olduğunda veya bölüm birincil kaynakta henüz olmadığında, diğer açık kaynaklardaki karşılığı otomatik aranır; tek tuşla diğer kaynağa geçme veya eşlemeyi sıfırlama menüsü sunulur.
- **Ayarlar → Oynatma → Kaynaklar arası yedek** seçeneği eklendi.

## [0.13.0]

### Eklenenler
- **Birleşik kimlik ve başlık eşleme (`internal/match`)**: Türkçe harf katlama, sezon/film ayrıştırma, Sorensen-Dice benzerliği ve `%APPDATA%\aniclitr\mappings.json` kalıcı eşleme deposu eklendi. Bir kez eşlenen MAL/AniList kimliği sonraki açılışlarda ağ araması yapmadan doğrudan açılır.
- **Çoklu sezon zinciri (`internal/seasons`)**: AniList `PREQUEL`/`SEQUEL` komşuluklarından sezon zinciri kurulur; tek sezon altında mutlak numaralandırma yapan kaynaklar ile sezon bazlı kaynaklar arasında otomatik bölüm dönüşümü yapılır.
- **Kural tabanlı sunucu ve kalite seçimi (`internal/choose`)**: **Ayarlar → Tercih edilen sunucu** seçeneği eklendi. Otomatik oynatmada (`p`) tam eşleşen sunucu anında açılır; aksi halde ilk çözümden sonra kısa süre beklenerek en yüksek puanlı sunucu seçilir.
- **HLS ve MP4 akış doğrulama (`internal/extract`)**: HLS `#EXT-X-STREAM-INF` manifestlerinden gerçek çözünürlük, bitrate ve codec (`H.264`, `HEVC`, `AV1`) bilgisi okunur; MP4 kaynakları HTTP `Range` ile yoklanarak bozuk dosyalar elenir.
- **Tanılama ve bağlantı inceleme (`internal/diag`)**: `aniclitr doctor --blame` komutu, **Kaynaklar** ekranında kaynak/sunucu hata karnesi ve fansub/kaynak seçicide `u` (**bağlantıları göster**) penceresi eklendi.

### Değişenler
- Fansub/kaynak seçicide eşzamanlı sunucu çözme sınırı 3'ten 6'ya çıkarıldı.
- Kaynak adaptörlerinde (`AnimeciX`, `AnimeTR`, `HiAnime`) alternatif başlıklar (`Titles`) ve film/sezon normalizasyonu standartlaştırıldı.

### Düzeltilenler
- Silinmiş, telif nedeniyle kaldırılmış veya 404 veren videolar (`KindDeleted`, `KindCopyright`, `KindNotFound`) kalıcı hata olarak işaretlenir ve otomatik kurtarma döngüsünde tekrar denenmez.
- İndirme yöneticisinde (`internal/download`) kalıcı hatalarda gereksiz yeniden denemeler kaldırıldı; geçici ağ hatalarında üstel geri çekilme (exponential backoff + jitter) uygulanır.
- `mpv` yalnızca başlangıçtaki yükleme sırasında (`MaxTime == 0`) çökerse yedek sunucuya geçilir; izleme ortasında kullanıcı veya pencere kapanışı hata sayılmaz.

## [0.12.0]

### Değişenler
- Arayüz baştan düzenlendi. Her ekran tek bir işe odaklanır, liste ve ayrıntı bölgeleri çerçeveli panellerde gösterilir ve her sayfanın başında amacı yazar.
- Ana sayfa menü tabanlı oldu: Devam et, Yeni bölümler, Kitaplık, Keşfet, İndirilenler, Ayarlar ve Çıkış. Seçilen öğenin önizlemesi yanda gösterilir; geniş ekranlarda haftalık takvim ve istatistikler de görünür.
- Keşfet adım adım ilerler: önce bölüm seçilir, ardından içerik kendi sayfasında açılır. Türler bölümünde önce tür ve sıralama seçilir.
- Alt satırda yalnızca ekranın en önemli kısayolları gösterilir.
- Uygulama içi metinler ve belgeler daha açık ve tutarlı bir dille yeniden yazıldı.

### Eklenenler
- **İşlemler menüsü** (`?` / `F1`): bulunulan ekrandaki tüm eylemleri kısayollarıyla listeler ve seçilen eylemi doğrudan çalıştırır.
- **Yeni bölümler** ekranı; `f` ile yalnızca takip edilen animeler gösterilebilir.
- Yavaş çizilen kareler ve terminalin geç kabul ettiği yazmalar tanılama amacıyla günlüğe kaydedilir.

### Düzeltilenler
- Kalınan yerin görüntüsü artık kırpılmadan, 16:9 oranında gösterilir.
- Görsel içeren ekranlarda seçim değişimi gereksiz ara kareler üretmez; kaydırma daha akıcıdır.

## [0.11.0]

### Eklenenler
- Kendini güncelleyen kurulum. aniclitr `%LOCALAPPDATA%\Programs\aniclitr` altına kurulur ve yeni sürümleri GitHub Releases üzerinden alır; her dosya SHA-256 ile doğrulanır.
- **Ayarlar → Otomatik güncelle** ve **Güncellemeleri denetle** seçenekleri; `aniclitr update` komutu.

### Değişenler
- Terminal kısayolu her zaman kurulu kopyayı açar.
- Animasyonlarda kare başına bellek ayırma yarıdan fazla azaldı.

## 0.10.1

### Düzeltilenler
- Bellek kullanımı yoğun oturumlarda yaklaşık 250 MB'tan 50 MB'a düştü. Kapak başına bellek maliyeti 1,5 MB'tan 56 KB'a indi; uzun oturumlarda bellek kullanımı artık büyümez.

## 0.10.0

### Eklenenler
- Yeni tanıtım turu; **Ayarlar → Tanıtım turu** üzerinden her zaman yeniden açılabilir.
- **Ayarlar → Terminalden aç**: aniclitr her terminalden `aniclitr` komutuyla açılabilir.

### Değişenler
- Anime her zaman birincil kaynakta açılır; MyAnimeList ilerlemesi bölüm listesinde gösterilir.

## 0.9.4

### Eklenenler
- MyAnimeList ilerlemesi bölüm listesinde işaretlenir; anime açıldığında imleç sıradaki bölümde durur.
- Kitaplıktaki durum değişiklikleri `ctrl+z` ile geri alınabilir; geri alma MyAnimeList'e de gönderilir.

### Değişenler
- MyAnimeList'ten gelen durum ve puan, animenin açıldığı kaynaktan bağımsız olarak gösterilir.
- `w` ile bir bölümün izlendi işaretini kaldırmak MyAnimeList'teki sayıyı da günceller.

## 0.9.3

### Değişenler
- Farklı kaynaklarda izlenen aynı anime tek kayıt olarak gösterilir; silindiğinde tüm kaynaklardaki kayıtları kaldırılır (`ctrl+z` ile geri alınabilir).
- `s`: geçmişteki kaynaklar hemen listelenir; kapalı kaynaklar dahil diğer kaynaklar da aranır. Kalınan bölüm yeni kaynağa taşınır.
- Altyazılı sürümler dublajlı sürümlerden önce listelenir.

### Eklenenler
- Birincil kaynak ya da tüm kaynaklar kapatıldığında uyarı gösterilir.

## 0.9.2

### Eklenenler
- Yeni kaynak: **AnimeTR** (Türkçe altyazılı anime ve donghua).

### Düzeltilenler
- Google Drive kaynakları 1080p olarak oynatılır ve indirilir.

## 0.9.1

### Değişenler
- İndirme öncesinde çeviri grubu seçilir (`d` bölüm, `D` sezon) ve seçilen grup bağlayıcıdır.
- Açılışta internet bağlantısı yoksa yalnızca indirilen bölümler gösterilir.

### Düzeltilenler
- Çevrimdışı kitaplıkta kapak görselleri gösterilir.
- Kopan indirme bağlantıları yeniden denenir; açık bağlantı sayısı gösterilir.

## 0.9.0

### Değişenler
- Tüm ekranlarda tutarlı görünüm; dar pencerelerde taşma giderildi.
- Ayarlar bölümlere ayrıldı ve küçük pencerelerde kaydırılabilir.
- Arşiv modu kısayolu `alt+a` oldu (`ctrl+l` ekranı yeniden çizer).

## 0.8.0

### Eklenenler
- Kalınan yer önizlemesi, bitiş kartı ve Keşfet'te "Bu akşam" ile "Türler" bölümleri.
- Benzer animeler (`y`), kapaklı çevrimdışı kitaplık, `ctrl+z` ile geri alma.
- Bozuk kaynakta sıradaki kaynağa otomatik geçiş ve bölüm bağlantılarının önceden hazırlanması.

[0.15.1]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.15.1
[0.15.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.15.0
[0.14.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.14.0
[0.13.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.13.0
[0.12.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.12.0
[0.11.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.11.0
