# aniclitr — yapılacaklar

Kural: terminal raconu — her şey klavyeyle; fareye/imleç ipucu moduna dayalı özellik yok.

## v0.7 (yapıldı)
- [x] Yazarken anında arama (otomatik tamamlama açılır listesi)
- [x] MyAnimeList entegrasyonu: OAuth (PKCE) girişi, liste çekme/gönderme; kitaplık MAL'in aynası olur
- [x] Keşfet ekranı (AniList — Jikan bu ağdan erişilemiyor): gerçek yayın takvimi + geri sayım, bu sezon, öneriler, rastgele anime
- [x] Takip: izlediğin serilere yeni bölüm düşünce rozet + bildirim
- [x] Kapaktan tema (animenin kapağından renk paleti)
- [x] Bölümlere not
- [x] Fragman (AniList trailer → mpv)
- [x] Minik arayüz sesleri (tık, seçim, konfeti) — ayarlanabilir
- [x] Kaynak sağlığı: canlı yoklama, gecikme, düşen kaynağı işaretleme; yeni kaynaklar için genişletilebilir
- [x] Harici altyazı altyapısı (OpenSubtitles; API anahtarı gerekir) — mpv'ye canlı ekleme

## v0.8 (yapıldı)
- [x] Bitiş kartı (AniSkip ED zamanı / yoksa son 90 sn) · kaldığın yer önizlemesi (kare)
- [x] Keşfet: bu akşam ne izlesem · türler (en iyi / popüler / kült + zar)
- [x] Benzer animeler (y) · ilk açılış rehberi + her sürümde "yenilikler"
- [x] ctrl+z geri al · akıllı kurtarma (bozuk kaynakta sıradakine geç)
- [x] Hız: bölümde bekleyince ön çözüm, akış + kapak disk önbelleği, boş aramada enter = devam
- [x] Çevrimdışı kitaplık (kapaklı), esc ile aramadan çıkma

## v0.9.2 (yapıldı)
- [x] Yeni kaynak: AnimeTR (OK.ru + Google Drive); Drive sayfa olarak mpv/yt-dlp'ye verilir
- [x] Seride `s`: tek kaynak bilinse de diğer kaynaklarda arar

## v0.9.3 (yapıldı)
- [x] Kaynaklar arası tek anime kimliği (raf, palet, bu akşam, öneriler, bildirimler, silme)
- [x] `s`: geçmişteki kaynaklar + kapalı kaynaklar dahil arka planda arama; bölüm taşınır
- [x] Kaynak adları/renkleri arayüzde sabit kod değil; boş sezon, dublaj sırası, kaynak kapatma uyarısı

## v0.9.4 (yapıldı)
- [x] MAL ilerlemesi/durumu/puanı kaynak ekranında; `w` geri alma MAL sayısını düşürür; durum değişikliği geri alınabilir

## v0.11.0 (yapıldı)
- [x] Kurulu kopya (%LOCALAPPDATA%\Programs\aniclitr) + GitHub Releases'tan kendini güncelleme, `aniclitr update`
- [x] Yayın: `scripts/release.ps1` + GitHub Actions (6 platform, checksums)
- [x] Ö4: kare başına tahsis 1375 → 573 KB (hücre ızgarası havuzu, tahsissiz SGR, tuval geri kullanımı)

## Sonra
- [ ] "Devam et" rafı MAL'de izlediğin ama burada hiç açmadığın serileri de göstersin
- [ ] Başka açık Türkçe kaynak adayları: anihub.com.tr, asyaanimeleri.top (WordPress), animpow.com — incelenmedi
- [ ] MAL: çok sezonlu seriler (AnimeciX tek başlıkta topluyor, MAL her sezona ayrı id veriyor) — sezon→MAL id eşlemesi
- [ ] OpenSubtitles anahtarını arayüzden girme; Jimaku (JP) sağlayıcısı
- [ ] **Torrent**: nyaa.si arama → aria2/webtorrent ile akış + indirme (en büyük hedef)
- [ ] **İnternetten altyazı** genişletme: Jimaku (JP), OpenSubtitles (TR/EN), torrent sürümüne göre eşleştirme
- [ ] AniList eşitleme (MAL'e alternatif; özel listeleri doğrudan destekler)
- [ ] Yıllık özet ("wrapped") + rozetler/başarımlar
- [ ] Geri/ileri ekran geçmişi (alt+← / alt+→)
- [ ] Kendi kısayolların (keys.json), vim modu
- [ ] Komut satırı modu: `aniclitr ara/izle/indir/devam`
- [ ] Seri başına kalite / ses / altyazı hafızası
- [ ] Birlikte izleme (syncplay benzeri)
- [ ] Sezon ekranında tür/puan filtresi
- [ ] Bölüm küçük resimleri
- [ ] Güncelleme kontrolü, taşınabilir yedek (dışa/içe aktarma)
- [ ] Windows bildirimleri (yeni bölüm)
