#!/usr/bin/env bash
# 一键构建。用法:
#   script/build.sh            构建 linux/amd64 到 dist/opencode2api (默认, 供 Linux 部署)
#   script/build.sh current    构建当前平台二进制到 dist/
#   script/build.sh all        交叉编译 windows/linux/darwin 到 dist/packages/
#   VERSION=v1.0.0 script/build.sh all
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-dev}"
export CGO_ENABLED=0
mkdir -p .local/go-cache .local/go-tmp .local/go-mod dist
if command -v cygpath >/dev/null 2>&1; then
  export GOCACHE="$(cygpath -w "$PWD/.local/go-cache")"
  export GOTMPDIR="$(cygpath -w "$PWD/.local/go-tmp")"
  export GOMODCACHE="$(cygpath -w "$PWD/.local/go-mod")"
else
  export GOCACHE="$PWD/.local/go-cache" GOTMPDIR="$PWD/.local/go-tmp" GOMODCACHE="$PWD/.local/go-mod"
fi

LDFLAGS="-s -w -X main.version=$VERSION"

build_one() {
  local goos="$1" goarch="$2" out="$3"
  echo ">> $goos/$goarch -> $out"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags "$LDFLAGS" -o "$out" ./cmd/opencode2api
}

if [ "${1:-}" = "all" ]; then
  mkdir -p dist/packages
  for t in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
    goos="${t%/*}"; goarch="${t#*/}"
    ext=""; [ "$goos" = windows ] && ext=".exe"
    name="opencode2api_${VERSION}_${goos}_${goarch}"
    build_one "$goos" "$goarch" "dist/packages/$name/opencode2api$ext"
    cp config.example.json README.zh-CN.md "dist/packages/$name/" 2>/dev/null || true
    (cd dist/packages && rm -f "$name.zip" && zip -qr "$name.zip" "$name" && rm -rf "$name")
    echo "   packaged dist/packages/$name.zip"
  done
  echo "完成: dist/packages/*.zip"
elif [ "${1:-}" = "current" ]; then
  ext=""; [ "$(go env GOOS)" = windows ] && ext=".exe"
  build_one "$(go env GOOS)" "$(go env GOARCH)" "dist/opencode2api$ext"
  echo "完成: dist/opencode2api$ext"
else
  build_one linux amd64 dist/opencode2api
  echo "完成: dist/opencode2api (linux/amd64)"
fi
