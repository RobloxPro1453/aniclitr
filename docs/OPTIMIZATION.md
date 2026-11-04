# Bellek ve Kaynak Kullanımı Raporu

**Kapsam:** aniclitr v0.10.0 → v0.11.0
**Durum:** Tamamlandı. Tüm iyileştirmeler v0.10.1 ve v0.11.0 sürümlerinde yayımlanmıştır.

---

## 1. Yönetici Özeti

v0.10.0 sürümünde, kapak görseli yoğun ağır kullanım senaryolarında uygulamanın bellek tüketimi yaklaşık 250 MB düzeyine ulaşmaktaydı. Yapılan inceleme, tüketimin büyük bölümünün sınırsız büyüyen görsel önbelleklerinden kaynaklandığını ortaya koymuştur.

Uygulanan iyileştirmeler, kullanıcı deneyimini değiştirmeden aşağıdaki sonuçları sağlamıştır:

| Ölçüt | v0.10.0 | v0.11.0 | Değişim |
|---|---|---|---|
| Kapak başına bellek maliyeti | 1,47 MB | 56 KB | −96% |
| Ağır oturumda canlı bellek (150 kapak, 175 sixel) | 234 MB | 22 MB | −91% |
| Ağır oturumda yerleşik bellek (tahmini, süreç geneli) | ~250 MB | ~50 MB | −80% |
| Animasyon sırasında kare başına bellek tahsisi | 1.375 KB | 573 KB | −58% |
| 150 kapağın yüklenme süresi | 6,5 sn | 3,5 sn | −46% |

---

## 2. Yöntem

Ölçümler, uygulamanın kendi görsel yükleme yolunu kullanan otomatik bir test senaryosuyla alınmıştır. Senaryonun ayrıntıları:
- 500×750 piksel boyutunda 150 JPEG kapak yüklendi. Bu boyut, TMDB `w500` boyutudur ve gerçek kullanımda en sık karşılaşılan boyuttur.
- Sixel grafik etkin, pencere boyutu 200×56 hücreydi.
- Kitaplık poster duvarı baştan sona kaydırıldı.
- Her aşamada çöp toplama zorlanarak canlı yığın (`HeapAlloc`) ve sistemden ayrılan bellek ölçüldü.
- Kare başına tahsis, Go `pprof` bellek profili ile ölçüldü.

Bu senaryo kalıcı bir test olarak depoda yer almaktadır:

```
ANICLITR_MEM=1 go test ./internal/ui -run TestMemHeavySession
```

---

## 3. Bulgular

1. **Tam çözünürlüklü görsel kopyaları:** Her kapak, yalnızca sixel kodlaması sırasında bir kez gerekmesine rağmen 480 piksel genişliğinde sıkıştırılmamış biçimde (≈1,38 MB) bellekte süresiz tutuluyordu. 150 kapakta bu, 198 MB'a karşılık geliyordu.
2. **Sınırsız önbellekler:** Kapak, sixel ve ölçeklenmiş görsel önbelleklerinde boyut sınırı bulunmuyordu.
3. **Çöp toplayıcı yapılandırması:** Varsayılan ayarlarla sistemden ayrılan bellek, canlı belleğin yaklaşık 2,2 katına ulaşıyordu.
4. **Kare başına geçici tahsis:** Ekran ızgarası her karede yeniden oluşturuluyor, renk kodları ayrıştırılırken geçici listeler üretiliyordu.

---

## 4. Uygulanan İyileştirmeler

| No | İyileştirme | Etki | Kullanıcı deneyimine etkisi |
|---|---|---|---|
| Ö1 | Bellekte yalnızca küçük ana kopya tutulur. Sixel kaynağı gerektiğinde disk önbelleğinden okunur ve arka planda çözülür. | Kapak başına −1,38 MB | Yok: sixel çıktısı piksel düzeyinde aynıdır |
| Ö2 | Kapak (160 adet) ve sixel (24 MB) önbelleklerine en az kullanılanı ilk çıkaran (LRU) sınırlar getirildi. Ekranda olan ve yakın zamanda gösterilen içerik hiçbir zaman çıkarılmaz. | Uzun oturumlarda sabit üst sınır | Yok: çıkarılan kapak diskten anında yeniden yüklenir |
| Ö3 | Çöp toplayıcı ayarlandı (GOGC 50, 128 MB yumuşak sınır). Oynatma başladığında ve görsel yoğun ekranlardan çıkıldığında boşalan bellek işletim sistemine iade edilir. | Ayrılan / canlı bellek oranı ~2,2 → ~1,4 | Yok |
| Ö4 | Ekran ızgarası, çizim tuvali ve geçmiş listesi yeniden kullanılır. Renk kodları geçici tahsis yapılmadan ayrıştırılır. | Kare başına −58% tahsis | Yok: çıktı bayt düzeyinde aynıdır |
| Ö5 | Yarım blok çizim için kullanılan küçük ana kopya 160 pikselden 96 piksele indirildi. | Kapak başına −97 KB | Görsel fark gözlenmemiştir (bkz. §5) |

---

## 5. Doğrulama

- **Çizici doğruluğu:** Uzun süreli fuzz testi, uçtan uca kayma testi, ekran taraması ve tuş fırtınası testleri başarıyla geçmiştir. Çizici paketi yarış dedektörüyle (`-race`) doğrulanmıştır.
- **Görsel karşılaştırma:** Gerçek bir kapak 26, 40 ve 60 sütun genişliğinde eski ve yeni yöntemle çizilerek karşılaştırılmış, gözle ayırt edilebilir fark bulunmamıştır.
- **Gerileme koruması:** Aşağıdaki sınırlar aşıldığında testler başarısız olur:

  | Test | Sınır |
  |---|---|
  | `TestPosterMemoryBudget` | Kapak başına 80 KB |
  | `TestMemHeavySession` | Ağır oturumda 40 MB canlı bellek |
  | `TestPosterTrimReloads` | Önbellekten çıkarılan kapağın kendiliğinden yeniden yüklenmesi |

---

## 6. Notlar ve Sınırlamalar

- Süreç geneli yerleşik bellek değeri, Go çalışma zamanı ölçümlerine program ve sistem kitaplıklarının tahmini payı (~15–20 MB) eklenerek hesaplanmıştır. Gerçek değer işletim sistemine ve terminal ortamına göre farklılık gösterebilir.
- Kare başına kalan tahsis, ekranın satır tabanlı olarak oluşturulmasından kaynaklanmaktadır. Bu bölümde yapılacak ek iyileştirmeler ekran bileşenlerinin kapsamlı biçimde yeniden düzenlenmesini gerektirdiğinden kapsam dışında bırakılmıştır.
