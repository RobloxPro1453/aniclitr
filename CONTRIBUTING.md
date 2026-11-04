# Katkı rehberi

aniclitr'e katkıda bulunmak istediğiniz için teşekkür ederiz. Bu belge geliştirme ortamının kurulmasını, test sürecini ve kod kurallarını açıklar.

## Hata bildirimi ve öneriler

- Hata bildirmeden önce [açık kayıtlara](https://github.com/RobloxPro1453/aniclitr/issues) göz atın.
- Bildirime şunları ekleyin: aniclitr sürümü (`aniclitr --version`), işletim sistemi, terminal uygulaması, sorunu yeniden üretme adımları ve `%APPDATA%\aniclitr\aniclitr.log` dosyasının ilgili bölümü.
- Kaynak sitelerle ilgili sorunlarda `aniclitr doctor` çıktısını paylaşın.

## Geliştirme ortamı

**Gereksinimler:** Go 1.27 veya üstü, mpv. İsteğe bağlı: yt-dlp, aria2c, ffmpeg.

```bash
git clone https://github.com/RobloxPro1453/aniclitr.git
cd aniclitr
go build -o bin/aniclitr ./cmd/aniclitr
./bin/aniclitr
```

Geliştirme sırasında kişisel verilerinizi etkilememek için ayrı bir veri klasörü kullanabilirsiniz:

```bash
ANICLITR_HOME=./.yerel go run ./cmd/aniclitr
```

## Testler

```bash
go vet ./...
go test ./internal/...                    # çevrimdışı testler (zorunlu)
ANICLITR_LIVE=1 go test ./internal/...    # gerçek kaynaklara karşı testler
ANICLITR_FUZZ=150 go test ./internal/tty  # uzun çizici zorlama testi
```

Bir değişiklik gönderilmeden önce `go vet` temiz olmalı ve çevrimdışı testlerin tamamı geçmelidir. Testler kullanıcının gerçek yapılandırmasına, geçmişine ya da indirme klasörüne dokunmamalıdır; `testApp()` yardımcısı bunun için geçici bir klasör kullanır.

## Kod kuralları

Kodda değişiklik yapmadan önce şu belgeleri okuyun:

- [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md): mimari, değişmezler ve bilinen tuzaklar.
- [docs/DESIGN.md](docs/DESIGN.md): arayüz tasarım sistemi. Yeni ekranlar `pageHead`, `panes` ve `hints()` kurallarını izlemelidir.

Temel kurallar:

1. **Klavye önceliklidir.** Her işlev klavyeyle erişilebilir olmalı ve ekranın `hints()` listesinde yer almalıdır; İşlemler menüsü bu listeden oluşur.
2. **Arayüz metinleri Türkçedir.** Büyük harf dönüşümünde `strings.ToUpperSpecial(unicode.TurkishCase, …)` kullanılır.
3. **Olay döngüsü engellenmez.** Ağ ve disk işlemleri `run(…)` ya da `guard(…)` içinde yürütülür.
4. **Renkler tema değişkenlerinden alınır.** Ekran kodunda sabit renk değeri kullanılmaz.
5. **Animasyonlar isteğe bağlıdır.** Her animasyonun hareket azaltılmış bir karşılığı bulunmalıdır.
6. **Kaynaklar koruma mekanizmalarını aşmaz.** Otomatik istemcileri bilinçli olarak engelleyen sitelere kaynak yazılmaz.

Kod `gofmt` ile biçimlendirilmelidir.

## Yeni kaynak eklemek

1. `internal/source/<ad>/` altında `source.Provider` arayüzünü uygulayın (isteğe bağlı olarak `Feeder` ve `Prober`).
2. Kaynağı `init()` içinde `source.Register(...)` ile kaydedin ve `Info.Note` alanını doldurun.
3. Paketi `cmd/aniclitr/main.go` ve `internal/ui/snap_test.go` dosyalarına boş içe aktarma olarak ekleyin.
4. `aniclitr doctor` ile doğrulayın.

Arayüzde değişiklik gerekmez. Ayrıntılar: [docs/ARCHITECTURE.md §6](docs/ARCHITECTURE.md#6-sources).

## Değişiklik gönderme

1. Depoyu çatallayın ve `main` dalından yeni bir dal oluşturun.
2. Değişikliklerinizi küçük ve odaklı commit'ler halinde yapın. Commit mesajları kısa ve açıklayıcı olmalıdır (örnek: `ui: keşfet türler adımına sıralama ekle`).
3. Testlerin geçtiğini doğrulayın.
4. Bir çekme isteği (pull request) açın; değişikliğin amacını ve nasıl test edildiğini açıklayın. Arayüz değişikliklerinde ekran görüntüsü ekleyin.
