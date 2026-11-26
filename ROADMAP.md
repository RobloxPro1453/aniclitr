# Yol haritası

Bu belge `aniclitr`'in tamamlanan temel kilometre taşlarını ve `v1.0.0` sonrası değerlendirilen çalışmaları listeler. Sürüm ayrıntıları [CHANGELOG.md](CHANGELOG.md) dosyasındadır.

**Ürün ilkesi:** Tüm işlevler klavyeyle kullanılabilir olmalıdır. Fareye bağımlı özellikler geliştirilmez.

## v1.0.0 ile Tamamlananlar

- **Nova varsayılan arayüz:** Tüm ana ekranlar Nova tasarımında (3 sütunlu Keşfet, üst çubukta indirme dalgası, Sixel katman kararlılığı, tuş kapakları); klasik görünüm ayarlardan seçilebilir.
- **Canlı altyazı & senkron ince ayarı:** `mpv` içi ASS OSD seçici (`y`), API anahtarsız [AniSub](https://anisub.co/) + OpenSubtitles, yerel `.srt`/`.ass`/`.vtt` yükleme (`l`) ve `z`/`x`/`Z`/`X`/`Alt+0` canlı zamanlama ayarı.
- **İki yönlü MyAnimeList & AniList OAuth eşitleme:** Durum, puan, bölüm ilerlemesi, özel listeler, çok sezonlu seri hizalaması ve her iki hesaptan ana sayfa "Devam et" köprüsü.
- **Nyaa.si torrent desteği:** Açık RSS araması (`alt+t`, anime ekranında `T` ve `aniclitr torrent` CLI), Türkçe/Güvenilir filtreleri, `aria2c` indirme ve `webtorrent`/magnet açma.
- **İndirme ve depolama:** Çoklu seçim (`v`), özel aralık (`30-100`), canlı hız kıvılcım grafiği (`▁▂▃▅▆▇`) ve sürücü depolama çubuğu.
- **Hızlı CLI & taşınabilirlik:** `devam`, `ara`, `izle`, `indir`, `torrent`, tek dosya güvenli `yedekle`/`geri-yukle`, `keys.json` özel kısayollar ve ekran geçmişi (`alt+←`/`alt+→`).
- **Ekosistem:** Discord Rich Presence (sıfır bağımlılık yerel IPC), işletim sistemi masaüstü bildirimleri, kaynak OP/ED zamanları, HiAnime `filler` rozeti ve otomatik atlama.

## v1.2.0 – v1.2.1 ile Tamamlananlar

- **Gömülü BitTorrent akışı & otomatik klasörleme:** Harici magnet istemcisi olmadan torrent içinden bölüm seçip `mpv` ile anlık izleme, `ctrl+d` kuyruğunda canlı takip ve ham torrent dosyalarını otomatik `<Seri Adı>/SxxEyy [Grup] [Kalite].mkv` klasörleme.
- **Discord Rich Presence sıfır-ayar düzeltmesi:** Windows `winio` named pipe ve kayıtlı genel App ID ile doğrudan çalışma.
- **Anime4K GPU görüntü iyileştirme:** 4 profilli gömülü GLSL shader motoru + `mpv` içinde `Alt+4` canlı geçiş.
- **Canlı animasyonlu Anime Özeti (Wrapped):** 6 slaytlık hikâye, sıfır-model Türkçe kişilik/yorum motoru ve `900×1200` paylaşılabilir PNG kart (`aniclitr ozet --png`).
- **Birlikte İzle (Watch Party) & Uyku Zamanlayıcısı:** Sıfır bağımlılık TCP oda eşitleme (`P` / `ctrl+k`) ve bölüm/süre/PC uyutma zamanlayıcısı (`Alt+s` / `T`).
- **Sıfır bekleme sonraki bölüm ön-hazırlama:** %70 sonrasında sonraki bölümün akışını arka planda çözüp anında oynatma.
- **Keşfet Ruh Hali Sihirbazı & Tek Tuşla Araç Kurulumu:** Öneriler içinde 6 küratörlü ruh hali (`m`), `aniclitr kur` otomatik araç kurucu, Winget otomatik senkron ve tam-eşleşme arama sıralaması.

## v1.3.0 ile Tamamlananlar

- **İki bağımsız açık kaynak (MIT) video motoru:** (1) AI'sız gerçek hedef çözünürlük ölçekleme (**AMD FidelityFX FSR v1.0.2**: `Otomatik`, `1080p / 1920×1080`, `2K / 2560×1440`, `4K / 3840×2160`) ve (2) resmi **Bloc97/Anime4K v4.0.1** CNN pürüzsüz çizgi onarımı (`Hızlı`, `Cam Gibi`, `Sinema`) + **`mpv` GPU Frame-Gen** (`Akıcı Smooth Motion` / `60+ FPS Catmull-Rom`), tamamı sıfır-kopya (`--hwdec=auto-safe`).
- **Tokyo Studio `mpv` içi kontrol merkezi (`m` / `Tab` / `Alt+4`):** Keskin mat obsidyen mimari kart, iki katmanlı alt menüler (`Esc`/`Backspace` üst menüye döner), tekrarlı akıcı `↑↓←→` gezinme, canlı çözünürlük telemetrisi ve `<1 ms` tepki.
- **İnsanüstü performans & UX psikolojisi:** 3,3× hızlı `tty.Frame.setLine` (`0 allocs`), doğrudan TrueColor yazıcı (`<1 ms/kare`), fare hareketinde `0 bayt`, çift-hızlı **60/20/0 FPS** uyarlamalı ambiyans saati, kademeli açılış (`OpenLazy`) ve HTTP/2 bağlantı havuzu.
- **Tüm menülerde eksiksiz akıllı arama:** Birleşik `match.RankTitle` motoru tüm arama yüzeylerine (`results`, `suggest`, `library`, CLI) ve Nyaa.si'nin tüm 5 sekmesine (TF-DF gürültü ayıklamalı otomatik seeders ikinci geçişiyle) yayıldı.
- **Tam Arayüz Discord Rich Presence & Bölüm Geçişi:** Sıralı yarışsız RPC işçisi, tüm ekranlarda canlı gezinti durumu, duraklatma koruması, sarma (seek) senkronu, özel `aniclitr` ikonu ve `--force-window=immediate` ile anında sonraki bölüm `mpv` penceresi.

## Sonraki Sürümler için Değerlendirilenler

- **Ek açık kaynak sağlayıcıları:** Bot koruması ve Cloudflare engeli olmayan açık API/HTML sunan yeni Türkçe kaynaklar.
