#!/usr/bin/env bash
# 一键启动: 缺 config 时从示例生成, 缺二进制时自动构建, 然后前台运行。
# 用法: script/start.sh [额外的 opencode2api 参数...]
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p .local/go-cache .local/go-tmp .local/go-mod
if command -v cygpath >/dev/null 2>&1; then
  export GOCACHE="$(cygpath -w "$PWD/.local/go-cache")"
  export GOTMPDIR="$(cygpath -w "$PWD/.local/go-tmp")"
  export GOMODCACHE="$(cygpath -w "$PWD/.local/go-mod")"
else
  export GOCACHE="$PWD/.local/go-cache" GOTMPDIR="$PWD/.local/go-tmp" GOMODCACHE="$PWD/.local/go-mod"
fi

BIN=".local/opencode2api"
[ "$(go env GOOS)" = windows ] && BIN=".local/opencode2api.exe"

if [ ! -f config.json ]; then
  cp config.example.json config.json
  echo "已生成 config.json —— 请修改 server_keys 和 webui.password 后使用。"
fi

if [ ! -f "$BIN" ]; then
  echo "构建 $BIN ..."
  go build -trimpath -o "$BIN" ./cmd/opencode2api
fi

echo "启动: http://127.0.0.1:8080/web  (API: /v1, 管理: /api)"
exec "$BIN" -config config.json "$@"
