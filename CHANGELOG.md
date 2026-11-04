# Değişiklik günlüğü

Bu projedeki önemli değişiklikler bu dosyada belgelenir. Sürüm numaraları [Anlamsal Sürümleme](https://semver.org/lang/tr/) kurallarını izler. Uygulama içindeki "Yenilikler" kartı aynı bilgiyi özet olarak gösterir. 0.11.0 öncesindeki sürümler GitHub üzerinden yayımlanmamıştır.

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

[0.12.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.12.0
[0.11.0]: https://github.com/RobloxPro1453/aniclitr/releases/tag/v0.11.0
