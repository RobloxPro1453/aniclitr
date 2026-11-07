# Yol haritası

Bu belge planlanan ve değerlendirilen çalışmaları listeler. Öncelikler değişebilir; buradaki maddeler bir taahhüt değildir. Tamamlanan çalışmalar [CHANGELOG.md](CHANGELOG.md) dosyasında yer alır.

**Ürün ilkesi:** Tüm işlevler klavyeyle kullanılabilir olmalıdır. Fareye bağımlı özellikler geliştirilmez.

## Öncelikli

- **Arayüz yenilemesi:** her ekranın tek bakışta anlaşılması; başlık çubuğunda indirme hız grafiği ve kaynakların ad/simgeyle (açık renkli, kapalı soluk) gösterilmesi; daha zarif bildirimler; her terminal boyutunda uyumlu yerleşim.

- **Depolama alanı göstergesi:** İndirilenler ekranında toplam kullanılan alanı ve diskteki boş alanı gösteren depolama çubuğu.
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

- Ek Türkçe kaynak adayları (ör. Animexe, ÇizgiMax): yalnızca otomatik istemcileri bilinçli olarak engellemeyen ve açık API/HTML sunan kaynaklar eklenir.
- Kaynakların verdiği açılış/bitiş zamanlarının (SonAnime, Animim) AniSkip'e ek olarak kullanılması.
- HiAnime'nin filler işaretiyle filler bölümlerini atlama.
- Yıllık izleme özeti ve başarımlar.
- Birlikte izleme (Syncplay entegrasyonu).
- Sezon listelerinde çoklu tür ve puan aralığı filtresi.
