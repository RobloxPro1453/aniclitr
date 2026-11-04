# Kurulum ve güncelleme

Bu belge aniclitr'in nasıl kurulduğunu, kendini nasıl güncel tuttuğunu ve güncelleme mekanizmasının teknik ayrıntılarını açıklar.

## Kurulum

1. [Releases](https://github.com/RobloxPro1453/aniclitr/releases/latest) sayfasından sisteminize uygun dosyayı indirin:

   | Sistem | Dosya |
   |---|---|
   | Windows (x64) | `aniclitr_windows_amd64.exe` |
   | Windows (ARM) | `aniclitr_windows_arm64.exe` |
   | Linux | `aniclitr_linux_amd64` · `aniclitr_linux_arm64` |
   | macOS | `aniclitr_darwin_arm64` (Apple Silicon) · `aniclitr_darwin_amd64` (Intel) |

2. Dosyayı çalıştırın. Tanıtım turunun **Terminalden erişim** adımında `y` tuşuna basıldığında aniclitr kurulur ve kullanıcı PATH'ine eklenir. Yönetici izni gerekmez.
3. Bu adımdan sonra her yeni terminalde `aniclitr` komutu yeterlidir.

| Sistem | Kurulum klasörü |
|---|---|
| Windows | `%LOCALAPPDATA%\Programs\aniclitr\` |
| Linux / macOS | `~/.local/share/aniclitr/` (bağlantı: `~/.local/bin/aniclitr`) |

Kurulum **Ayarlar → Terminalden aç** üzerinden sonradan yapılabilir ya da kaldırılabilir.

## Otomatik güncelleme

1. aniclitr açıldıktan kısa bir süre sonra arka planda GitHub'daki son sürümü denetler.
2. Yeni bir sürüm varsa indirilir ve `checksums.txt` içindeki SHA-256 özetiyle doğrulanır. Özet eşleşmezse hiçbir değişiklik yapılmaz.
3. Yeni sürüm bir sonraki açılışta etkinleşir ve "Yenilikler" kartı bir kez gösterilir.

| İşlem | Yöntem |
|---|---|
| Otomatik güncellemeyi açma / kapatma | Ayarlar → Otomatik güncelle |
| Elle denetleme | Ayarlar → Güncellemeleri denetle ya da `aniclitr update` |
| Sürümü görüntüleme | `aniclitr --version` |

Kişisel veriler güncellemeden etkilenmez. Ayarlar, izleme geçmişi, kitaplık ve MyAnimeList oturumu `%APPDATA%\aniclitr\` altında; görsel önbelleği `%LOCALAPPDATA%\aniclitr\` altında saklanır.

## Sorun giderme

| Belirti | Çözüm |
|---|---|
| `aniclitr update` "güncelleme deposu yok" bildiriyor | Bu, kaynaktan derlenmiş bir kopyadır. **Ayarlar → Terminalden aç** seçeneğini kapatıp yeniden açın; resmi sürüm kurulur. |
| "SHA-256 özeti tutmuyor" | İndirme bozulmuştur ve hiçbir şey kurulmamıştır. Bir süre sonra yeniden deneyin. |
| Önceki bir sürüme dönme | Releases sayfasından o sürümün dosyasını indirin. aniclitr kapalıyken kurulum klasöründeki dosyanın yerine koyun ve **Ayarlar → Otomatik güncelle** seçeneğini kapatın. |

## Sürüm numaraları

Sürümler `vX.Y.Z` etiketiyle yayımlanır ve [Anlamsal Sürümleme](https://semver.org/lang/tr/) kurallarını izler:

| Değişiklik | Artırılan |
|---|---|
| Hata düzeltmesi | Z |
| Yeni özellik | Y |
| Uyumsuz değişiklik | X |

Bir etiket GitHub'a gönderildiğinde [`release.yml`](../.github/workflows/release.yml) iş akışı testleri çalıştırır, altı platform için derleme yapar, `checksums.txt` dosyasını üretir ve sürümü yayımlar.

## Teknik ayrıntılar

- **Güncelleyici** (`internal/update`): `api.github.com/repos/<depo>/releases/latest` adresine kimlik doğrulamasız istek gönderir, dosyayı indirir ve SHA-256 ile doğrular. Çalışan dosya `.old` uzantısıyla yeniden adlandırılır ve yeni dosya yerine taşınır; Windows çalışan bir dosyanın yeniden adlandırılmasına izin verir. `.old` dosyası bir sonraki açılışta silinir.
- **Kurulum** (`internal/pathx`): çalışan dosyayı kurulum klasörüne kopyalar ve klasörü kullanıcı PATH'ine ekler. Ardından resmi sürümü indirip kopyanın yerine koyar; çalışan derleme yayımlanmış sürümden yeniyse dokunulmaz.
- Yalnızca **kurulu kopya** kendini günceller. Kaynaktan derlenen kopyalar yalnızca yeni bir sürüm olduğunu bildirir.
- **Derleme damgaları:** depo adı (`update.Repo`) ve sürüm (`main.version`) derleme sırasında `-ldflags -X` ile yazılır. Depo adının kaynak koddaki varsayılan değeri `RobloxPro1453/aniclitr`'dir.
