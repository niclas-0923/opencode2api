# 一键启动 (Windows): 缺 config 时生成, 缺二进制时构建, 然后前台运行。
param([Parameter(ValueFromRemainingArguments=$true)][string[]]$ExtraArgs)
$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

$root = (Get-Location).Path
New-Item -ItemType Directory -Force .local/go-cache, .local/go-tmp, .local/go-mod | Out-Null
$env:GOCACHE = "$root\.local\go-cache"
$env:GOTMPDIR = "$root\.local\go-tmp"
$env:GOMODCACHE = "$root\.local\go-mod"

if (-not (Test-Path config.json)) {
  Copy-Item config.example.json config.json
  Write-Host "已生成 config.json —— 请修改 server_keys 和 webui.password 后使用。"
}

$bin = ".local\opencode2api.exe"
if (-not (Test-Path $bin)) {
  Write-Host "构建 $bin ..."
  go build -trimpath -o $bin ./cmd/opencode2api
  if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }
}

Write-Host "启动: http://127.0.0.1:8080/web  (API: /v1, 管理: /api)"
& $bin -config config.json @ExtraArgs
