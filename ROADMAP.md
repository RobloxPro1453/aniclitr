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

## v1.1+ için Değerlendirilenler

- **Birlikte izleme:** Syncplay entegrasyonu.
- **Sezon & Keşfet gelişmiş filtreleri:** Çoklu tür kesişimi, yıl ve puan aralığı süzgeci.
- **Yıllık izleme özeti:** Yıl sonu istatistik kartları ve izleme alışkanlıkları.
- **Ek açık kaynak sağlayıcıları:** Bot koruması ve Cloudflare engeli olmayan açık API/HTML sunan yeni Türkçe kaynaklar.
