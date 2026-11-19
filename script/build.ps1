# 一键构建 (Windows)。用法:
#   .\script\build.ps1            构建 linux/amd64 到 dist\opencode2api (默认, 供 Linux 部署)
#   .\script\build.ps1 -Current   构建当前平台 (windows/amd64) 到 dist\
#   .\script\build.ps1 -All       交叉编译 windows/linux/darwin 到 dist\packages\
#   .\script\build.ps1 -All -Version v1.0.0
param([switch]$All, [switch]$Current, [string]$Version = "dev")
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

$env:CGO_ENABLED = "0"
$root = (Get-Location).Path
New-Item -ItemType Directory -Force .local/go-cache, .local/go-tmp, .local/go-mod, dist | Out-Null
$env:GOCACHE = "$root\.local\go-cache"
$env:GOTMPDIR = "$root\.local\go-tmp"
$env:GOMODCACHE = "$root\.local\go-mod"
$ldflags = "-s -w -X main.version=$Version"

function Build-One($goos, $goarch, $out) {
  Write-Host ">> $goos/$goarch -> $out"
  $env:GOOS = $goos; $env:GOARCH = $goarch
  go build -trimpath -ldflags $ldflags -o $out ./cmd/opencode2api
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

if ($All) {
  New-Item -ItemType Directory -Force dist/packages | Out-Null
  foreach ($t in @("windows/amd64","windows/arm64","linux/amd64","linux/arm64","darwin/amd64","darwin/arm64")) {
    $goos, $goarch = $t -split "/"
    $ext = if ($goos -eq "windows") { ".exe" } else { "" }
    $name = "opencode2api_${Version}_${goos}_${goarch}"
    New-Item -ItemType Directory -Force "dist/packages/$name" | Out-Null
    Build-One $goos $goarch "dist/packages/$name/opencode2api$ext"
    Copy-Item config.example.json, README.zh-CN.md "dist/packages/$name/" -ErrorAction SilentlyContinue
    Compress-Archive -Path "dist/packages/$name" -DestinationPath "dist/packages/$name.zip" -Force
    Remove-Item -Recurse -Force "dist/packages/$name"
    Write-Host "   packaged dist/packages/$name.zip"
  }
  Write-Host "完成: dist/packages/*.zip"
} elseif ($Current) {
  Build-One "windows" "amd64" "dist/opencode2api.exe"
  Write-Host "完成: dist/opencode2api.exe"
} else {
  Build-One "linux" "amd64" "dist/opencode2api"
  Write-Host "完成: dist/opencode2api (linux/amd64)"
}
