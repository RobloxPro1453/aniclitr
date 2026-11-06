# Yol haritası

Bu belge planlanan ve değerlendirilen çalışmaları listeler. Öncelikler değişebilir; buradaki maddeler bir taahhüt değildir. Tamamlanan çalışmalar [CHANGELOG.md](CHANGELOG.md) dosyasında yer alır.

**Ürün ilkesi:** Tüm işlevler klavyeyle kullanılabilir olmalıdır. Fareye bağımlı özellikler geliştirilmez.

## Öncelikli

- **Torrent desteği:** nyaa.si üzerinden arama; aria2 ya da WebTorrent ile akış ve indirme.
- **Ek çevrimiçi altyazı sağlayıcıları:** Günlük kota sınırı olmayan ücretsiz Türkçe/İngilizce/Japonca altyazı kaynakları (ör. Subdl, Jimaku) ve yerel `.srt`/`.ass` dosyası seçici.
- **AniList hesap eşitlemesi:** MyAnimeList'e alternatif olarak AniList OAuth entegrasyonu (özel listeleri ve çok sezonlu yapıyı doğrudan destekler).

## Planlanan

- "Devam et" bölümünde, MyAnimeList'te izlenen ancak aniclitr'de henüz açılmamış serilerin de doğrudan gösterilmesi.
- Seri bazında kalite, ses, çeviri grubu ve altyazı tercihlerinin ayrı ayrı hatırlanması.
- Hızlı komut satırı alt komutları: `aniclitr ara`, `izle`, `indir`, `devam`.
- Kullanıcı tanımlı kısayollar (`keys.json`) ve vim modu.
- Ekran geçmişinde geri ve ileri gitme (`alt+←`, `alt+→`).
- Verilerin tek dosyada dışa ve içe aktarılması (taşınabilir yedek).
- Yeni bölümler için işletim sistemi bildirimleri.

## Değerlendirilen

- Ek Türkçe kaynak adayları: yalnızca otomatik istemcileri bilinçli olarak engellemeyen ve açık API/HTML sunan kaynaklar eklenir.
- Yıllık izleme özeti ve başarımlar.
- Birlikte izleme (Syncplay entegrasyonu).
- Sezon listelerinde çoklu tür ve puan aralığı filtresi.
