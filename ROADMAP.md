# Yol haritası

Bu belge planlanan ve değerlendirilen çalışmaları listeler. Öncelikler değişebilir; buradaki maddeler bir taahhüt değildir. Tamamlanan çalışmalar [CHANGELOG.md](CHANGELOG.md) dosyasında yer alır.

**Ürün ilkesi:** Tüm işlevler klavyeyle kullanılabilir olmalıdır. Fareye bağımlı özellikler geliştirilmez.

## Öncelikli

- **Torrent desteği:** nyaa.si üzerinden arama; aria2 ya da WebTorrent ile akış ve indirme.
- **Çevrimiçi altyazı:** Jimaku (Japonca) ve OpenSubtitles (Türkçe, İngilizce) sağlayıcıları; torrent sürümüne göre eşleştirme. OpenSubtitles API anahtarının arayüzden girilebilmesi.
- **Çok sezonlu serilerde MyAnimeList eşitlemesi:** AnimeciX sezonları tek başlıkta toplar, MyAnimeList her sezon için ayrı kayıt tutar. Sezon ile MyAnimeList kimliği arasında eşleme gereklidir.

## Planlanan

- "Devam et" bölümünde, MyAnimeList'te izlenen ancak aniclitr'de henüz açılmamış serilerin de gösterilmesi.
- AniList eşitlemesi (MyAnimeList'e alternatif; özel listeleri doğrudan destekler).
- Seri bazında kalite, ses ve altyazı tercihlerinin hatırlanması.
- Komut satırı modu: `aniclitr ara`, `izle`, `indir`, `devam`.
- Kullanıcı tanımlı kısayollar (`keys.json`) ve vim modu.
- Ekran geçmişinde geri ve ileri gitme (`alt+←`, `alt+→`).
- Verilerin dışa ve içe aktarılması (taşınabilir yedek).
- Yeni bölümler için işletim sistemi bildirimleri.

## Değerlendirilen

- Yeni Türkçe kaynak adayları: anihub.com.tr, asyaanimeleri.top, animpow.com. Yalnızca otomatik istemcileri bilinçli olarak engellemeyen kaynaklar eklenir.
- Yıllık izleme özeti ve başarımlar.
- Birlikte izleme (syncplay benzeri).
- Sezon listelerinde tür ve puan filtresi.
- Bölüm küçük resimleri.
