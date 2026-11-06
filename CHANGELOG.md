# Değişiklik günlüğü

Bu projedeki önemli değişiklikler bu dosyada belgelenir. Sürüm numaraları [Anlamsal Sürümleme](https://semver.org/lang/tr/) kurallarını izler. Uygulama içindeki "Yenilikler" kartı aynı bilgiyi özet olarak gösterir. 0.11.0 öncesindeki sürümler GitHub üzerinden yayımlanmamıştır.

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
