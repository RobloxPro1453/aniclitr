# aniclitr — bellek ve kaynak raporu (2026-11-04, v0.10.0)

**Durum:** Ö1, Ö2, Ö3 ve Ö5 **uygulandı (v0.10.1)**; sonuçlar §7'de. Ö4 bekliyor.

**Kural:** her adım *kayıpsız* olmalı. Ekranda aynı görüntü, aynı akıcılık, aynı tepki süresi.

---

## 1. Özet

- Görev Yöneticisi'nde **253,7 MB** görünüyor. Bunun neredeyse tamamı **kapak görselleri**.
- Bir kapak diskte ortalama **103 KB** yer tutuyor, bellekte **1,47 MB**. Yani yaklaşık **14 kat**.
- Kapak önbelleği hiç boşalmıyor. Ana sayfa, kitaplık duvarı, keşfet ve arama gezildikçe büyüyor.
- Go'nun çöp toplayıcısı (GC) varsayılan ayarla, canlı belleğin yaklaşık **2 katını** işletim sisteminden ayırıyor.
- Öneriler uygulanırsa ağır bir oturumda tahminen **~40–60 MB** civarına inilir. Bu bir tahmin; her adımdan sonra aşağıdaki ölçüm testiyle doğrulanacak.

---

## 2. Nasıl ölçüldü

Geçici bir Go testi gerçekçi bir oturumu taklit etti:
- Uygulamanın kendi `fetchPoster` yolu (dosya → çöz → küçült) kullanıldı.
- 150 adet 500×750 JPEG yüklendi. Bu boyut TMDB'nin `w500` boyutu; diskteki gerçek önbellekte en sık görülen boyut da bu (158 dosya, ortalama 103 KB).
- Sixel açık, 200×56 pencere.
- Kitaplık poster duvarı baştan sona kaydırıldı.
- Her aşamada iki kez `runtime.GC()` çalıştırılıp `HeapAlloc` (canlı yığın) ve `Sys` (işletim sisteminden alınan) okundu.

| Aşama | Canlı yığın | İşletim sisteminden alınan |
|---|---|---|
| A. açılış | 0,5 MB | 12,8 MB |
| B. 150 kapak yüklendi | **220,9 MB** (kapak başına 1,47 MB) | 410,8 MB |
| C. kitaplık duvarı tamamen kaydırıldı | **234,4 MB** | **520,4 MB** |

C aşamasında yığının dökümü:

| Ne | Adet | Boyut | Not |
|---|---|---|---|
| Kapakların 480 px'lik tam renkli ana kopyası (`posterState.full`) | 150 | **197,8 MB** | yalnızca sixel kodlarken kullanılıyor |
| Kapakların 160 px'lik küçük kopyası (`posterState.img`) | 150 | 22,0 MB | yarım blok çizim ve renk için |
| Sixel kodlamaları (`gfx.enc`) | 180 | 10,5 MB | boyut başına bir kez |
| Ölçeklenmiş ve çizilmiş satırlar (`scaled`, `rendered`) | 180 + 180 | 1,8 MB | küçük |
| Ana sayfa karesi (animasyonlu) | — | 1,16 ms/kare, **0,4 MB tahsis/kare** | GC baskısı |

**Kök neden:** `fetchPoster` her kapağı çözüp 480 px genişliğinde sıkıştırılmamış RGBA olarak saklıyor (480×720×4 = 1,38 MB). Bu kopya yalnızca bir kapağın sixel kodlaması **ilk kez** üretilirken gerekiyor; kodlama zaten önbelleğe alınıyor. Ama kopya sonsuza kadar bellekte kalıyor ve önbelleklerin hiçbirinde boyut sınırı yok.

---

## 3. Öneriler (öncelik sırasıyla)

### Ö1 — Tam boy kapak kopyasını bellekte tutma

- **Ne:** `posterState.full` (çözülmüş RGBA) yerine kapağın **sıkıştırılmış JPEG baytları** (≈100 KB) saklanır. Sixel kodlaması gerektiğinde bayt, kodlayıcı işçinin içinde çözülür. Kodlama zaten arka planda (asenkron) yapılıyor ve hazır olana kadar yarım blok gösteriliyor, yani bugünkü akış aynen kalır.
- **Kazanç:** kapak başına ~1,38 MB → ~0,10 MB. 150 kapakta **~198 MB → ~15 MB**.
- **Kayıpsız mı:** Evet. Aynı çözme ve aynı 480 px küçültme, yalnızca daha geç yapılıyor; piksel piksel aynı sixel çıkar.
  - Ek maliyet: bir kapağın **ilk** sixel kodlamasında ~30–40 ms fazladan çözme. Bu arka plan işçisinde oluyor ve kullanıcı görmüyor (o an zaten yarım blok gösteriliyor).
  - Sonraki gösterimler önbellekten gelir.
- **Risk / efor:** Düşük. `fetchPoster`, `posterState`, `gfx.request` değişir.

### Ö2 — Önbelleklere sınır (LRU), görünenlere asla dokunmadan

- **Ne:**
  - `posters.items` (küçük kopya), `gfx.enc` (sixel), `scaled` ve `rendered` için en son kullanılanı tutan, bellek bütçeli sınırlar. Öneri: kapak ≈ 200 adet ya da 32 MB, sixel ≈ 32 MB.
  - Yalnızca son birkaç saniyede **ekrana çizilmemiş** girdiler atılır.
  - Atılan kapak tekrar gerekirse disk önbelleğinden (`kapaklar/`) gelir. Ağ gerekmez; birkaç milisaniyede, mevcut 350 ms'lik yumuşak geçişle görünür.
- **Kazanç:** Uzun oturumlarda bellek sınırsız büyümez; tavan sabit kalır.
- **Kayıpsız mı:** Evet. Görünen ve yakın zamanda görünen hiçbir şey atılmaz, geri gelen kapak diskten anında yüklenir.
- **Risk / efor:** Düşük–orta. Mimari belgesindeki "atılan komut kapağı sonsuza kadar yükleniyor bırakır" tuzağına dikkat: `want()` yeniden istek döndürmeli.

### Ö3 — Çöp toplayıcıyı belleğe göre ayarla

- **Ne:**
  - `debug.SetMemoryLimit(~150 MB)` ile yumuşak bir bellek tavanı konur ve `GOGC` 100'den ~50'ye çekilir.
  - mpv oynatmaya başladığında (terminal zaten boşta) ve ağır bir ekrandan (kitaplık duvarı, keşfet) çıkınca bir kez `debug.FreeOSMemory()` çağrılır.
- **Kazanç:** Ölçümde işletim sisteminden alınan bellek canlı yığının 2,2 katı (520 MB / 234 MB). Bu oran ~1,3–1,5'e iner.
- **Kayıpsız mı:** Evet. Uygulama boştayken hiç çalışmıyor, animasyonda GC fazladan birkaç milisaniyelik kısa duraklama yapar. Kare süresi bugün 1,2 ms, 16 ms'lik bütçenin çok altında.
- **Risk / efor:** Çok düşük, birkaç satır.

### Ö4 — Kare başına tahsisi azalt

- **Ne:**
  - Bugün her kare ~0,4 MB geçici bellek ayırıyor; 30 fps animasyonda saniyede ~12 MB çöp ediyor.
  - `tty.Frame` hücre dizileri havuzdan yeniden kullanılır, satır kesme ve birleştirmede ara stringler azaltılır, sabit parçalar (başlık, alt satır) önbelleğe alınır.
- **Kazanç:** Animasyon sırasında daha az GC, daha düşük CPU ve daha düz bellek grafiği.
- **Kayıpsız mı:** Evet. Çıktı aynı; çizici testleri (emülatör, fuzz, uçtan uca kayma testi) bunu bayt bayt korur.
- **Risk / efor:** Orta. Çizici hassas bir yer; her adım `tty` fuzz testinden geçmeli.

### Ö5 — (İsteğe bağlı, gözle görülür) küçük kapak kopyasını 160 → 96 px yap

- **Ne:** Yarım blok kapaklar en fazla ~40 sütun genişliğinde çiziliyor. 96 px kaynak bunun için yeterli.
- **Kazanç:** Ek ~14 MB (150 kapakta).
- **Kayıpsız mı:** **Tam değil.** Çok büyük yarım blok kapaklarda küçültme kalitesi çok az değişebilir. Ö1 ve Ö2'den sonra gerek kalmayabilir; ölçüp karar verelim.

### Ö6 — Küçükler (şimdilik dokunmaya gerek yok)

- `details`, `episodes`, `variants` haritaları da sınırsız ama küçük (One Piece'in 1142 bölümü bile birkaç yüz KB). İleride Ö2'nin LRU'suna eklenebilir.
- Ambiyans animasyonu ana sayfada sürekli 30 fps çiziyor (boşta ~%1 CPU). 20 fps'e çekmek CPU'yu düşürür ama hareketi biraz değiştirir, yani kayıpsız değil. Öneri: dokunma.

---

## 4. Beklenen sonuç (tahmin, ölçülecek)

| Senaryo | Bugün | Ö1 sonrası | Ö1 + Ö2 + Ö3 sonrası |
|---|---|---|---|
| 150 kapaklı ağır oturum, canlı yığın | 234 MB | ~37 MB | ~35 MB (tavanlı) |
| İşletim sisteminden alınan (Görev Yöneticisi'ne yakın) | ~250–520 MB | ~80 MB | **~40–60 MB** |

---

## 5. Doğrulama planı

- Bu rapordaki ölçüm testi kalıcı bir **bellek bütçesi testine** dönüşür: "150 kapak + duvar kaydırma → canlı yığın < 40 MB". Gerileme olursa test kırılır.
- Her öneri ayrı bir adımda uygulanır ve her adımdan sonra şunlar çalışır:
  - `go test ./internal/...` (çizici fuzz, uçtan uca kayma, ekran taraması, tuş fırtınası dahil),
  - bellek testi,
  - PNG anlık görüntü karşılaştırması (görüntü aynı mı).
- Son olarak gerçek Windows Terminal'de Görev Yöneticisi değeri önce ve sonra karşılaştırılır.

---

## 6. Check-up: bu turda bulunan diğer konular

| Konu | Durum |
|---|---|
| MAL'deki Bleach kaydı uygulama tarafından "tamamlandı 366/366" yapılmıştı | Düzeltildi (izliyor · 154). Durum değişiklikleri artık geri alınabiliyor. Kök neden günlükte iz bırakmadığı için kesin bulunamadı. |
| AnimeciX, Youjo Senki dizisine filmin MAL id'sini (37055) veriyor | Kitaplıkta film ile dizi artık birbirini ezmiyor. Dizinin MAL eşleşmesi kaynak verisi yüzünden yanlış kalıyor. |
| MAL ve mangacix.net için ara sıra DNS hatası (`no such host`) | Ağ tarafı; uygulama yeniden deniyor. Kalıcı olursa DNS ayarı önerilebilir. |
| Ölü video sunucuları (Vidmoly, Vidoza, streamtape, uqload…) | Akıllı kurtarma atlıyor. İsteğe bağlı: bilinen ölü sunucuları sıranın sonuna almak çözüm süresini kısaltır. |
| `staticcheck` | Bu oturumda indirip çalıştırmak engellendi; çalıştırılamadı. `go vet` temiz. |

---

## 7. Sonuç (v0.10.1, uygulandıktan sonra)

Ölçüm yöntemi §2'dekiyle aynı. Bu kez gerçek uygulamanın GC ayarları ve bellek iadesiyle ölçüldü; ölçüm testi kalıcı oldu: `ANICLITR_MEM=1 go test ./internal/ui -run TestMemHeavySession`.

| | Önce | Sonra |
|---|---|---|
| Bir kapağın bellekte kalan maliyeti | 1,47 MB | **56 KB** (26 kat az) |
| 150 kapak yüklendikten sonra canlı yığın | 220,9 MB | **8,8 MB** |
| Duvar sonuna kadar kaydırılınca canlı yığın (175 sixel dahil) | 234,4 MB | **21,7 MB** |
| Bellek iadesinden sonra yerleşik bellek tahmini (Go tarafı) | — | **32,9 MB** |
| 150 kapağı çözme süresi | 6,5 s | 3,5 s |

- Görev Yöneticisi'nde bunun üzerine programın kendisi ve yüklü DLL'ler eklenir (~15–20 MB). En ağır oturumda beklenen değer **~50 MB**; önceki 253 MB'ın yaklaşık beşte biri. Gerçek terminalde henüz doğrulanmadı.
- **Kayıpsızlık:**
  - Sixel aynı dosyadan, aynı 480 px adımıyla üretiliyor, yani aynı görüntü.
  - Yarım blok kapaklar 160 yerine 96 px kaynaktan çiziliyor. Gerçek bir kapakla 26, 40 ve 60 sütunda yan yana karşılaştırıldı; fark gözle görülmüyor.
  - Bütün çizici ve ekran testleri (fuzz, uçtan uca kayma, tarama, tuş fırtınası) geçiyor.
  - Atılan bir kapak yeniden çizildiğinde diskten kendiliğinden geliyor ve tekrar belirme animasyonu oynamıyor.
