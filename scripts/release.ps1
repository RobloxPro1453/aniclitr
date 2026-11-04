<#
.SYNOPSIS
  aniclitr'in yeni sürümünü çıkarır: test, sürüm numarası, commit, etiket, push.

.DESCRIPTION
  GitHub'a vX.Y.Z etiketi gidince .github/workflows/release.yml exe'leri
  derler ve sürümü yayınlar; kurulu kopyalar kendilerini oradan günceller.

  Kullanım:  .\scripts\release.ps1 0.11.1

  Önce internal/ui/onboard.go içine whatsNew["0.11.1"] girdisini yaz: o
  satırlar hem uygulamadaki "yenilikler" kartı hem de sürüm notu olur.
#>
param([Parameter(Mandatory = $true)][string]$Version)

$ErrorActionPreference = 'Stop'
Set-Location (Split-Path $PSScriptRoot -Parent)

function Fail($msg) { Write-Host "✗ $msg" -ForegroundColor Red; exit 1 }

$Version = $Version.TrimStart('v')
if ($Version -notmatch '^\d+\.\d+\.\d+$') { Fail "sürüm X.Y.Z biçiminde olmalı: $Version" }

$main = 'cmd/aniclitr/main.go'
$current = [regex]::Match((Get-Content $main -Raw), 'var version = "([^"]+)"').Groups[1].Value
if ([version]$Version -le [version]$current) { Fail "yeni sürüm ($Version) şimdikinden ($current) büyük olmalı" }

if (git status --porcelain) { Fail "commit edilmemiş değişiklikler var; önce onları commit et" }
if (git tag -l "v$Version") { Fail "v$Version etiketi zaten var" }

# release notes = the what's-new lines of this version
$onboard = Get-Content 'internal/ui/onboard.go' -Raw
$block = [regex]::Match($onboard, '"' + [regex]::Escape($Version) + '":\s*\{(.*?)\n\t\},', 'Singleline')
if (-not $block.Success) { Fail "internal/ui/onboard.go içinde whatsNew[`"$Version`"] yok" }
$notes = [regex]::Matches($block.Groups[1].Value, '"((?:[^"\\]|\\.)*)"') | ForEach-Object { '- ' + ($_.Groups[1].Value -replace '\\(.)', '$1') }  # undo Go string escapes (\\ → \)
if (-not $notes) { Fail "whatsNew[`"$Version`"] boş" }

Write-Host "→ testler" -ForegroundColor Cyan
go vet ./cmd/... ./internal/...; if ($LASTEXITCODE) { Fail "go vet" }
go test ./internal/...; if ($LASTEXITCODE) { Fail "go test" }

Write-Host "→ sürüm $current → $Version" -ForegroundColor Cyan
(Get-Content $main -Raw) -replace 'var version = "[^"]+"', "var version = `"$Version`"" | Set-Content $main -NoNewline -Encoding utf8
go build -o bin/aniclitr.exe ./cmd/aniclitr; if ($LASTEXITCODE) { Fail "go build" }

git add $main
git commit -m "aniclitr $Version"; if ($LASTEXITCODE) { Fail "git commit" }
$tmp = New-TemporaryFile
Set-Content $tmp (@("aniclitr $Version", "") + $notes) -Encoding utf8
git tag -a "v$Version" -F $tmp; Remove-Item $tmp
if ($LASTEXITCODE) { Fail "git tag" }

Write-Host "→ GitHub'a gönderiliyor" -ForegroundColor Cyan
git push; if ($LASTEXITCODE) { Fail "git push" }
git push origin "v$Version"; if ($LASTEXITCODE) { Fail "git push tag" }

Write-Host "✓ v$Version gönderildi. GitHub Actions birkaç dakikada sürümü yayınlar;" -ForegroundColor Green
Write-Host "  kurulu aniclitr bir sonraki açılışta onu kendisi kurar." -ForegroundColor Green
