#!/bin/sh
# Builds the cw binaries (in a golang container) and the install packages into dist/:
#   claude-workspaces-<v>-{windows,macos,linux}  desktop app + terminal version, with shortcuts
#   cw-terminal-<v>-{windows,macos,linux}        terminal version only (built with -tags terminal)
#   cw-<v>-{linux,macos}                         same as the desktop ones, for `cw update` of <= 0.9
#   sh packaging/build.sh 0.10.0 [output-dir]
set -eu
VERSION=${1:?uso: build.sh VERSION [DIR]}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${2:-$ROOT/dist}
STAGE=$(mktemp -d)
mkdir -p "$OUT"

docker run --rm -u "$(id -u):$(id -g)" -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
  -v "$ROOT":/src -v "$STAGE":/out -w /src golang:1.26 sh -c "
    set -e
    mkdir -p /out/full /out/term
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
      os=\${target%/*}; arch=\${target#*/}
      CGO_ENABLED=0 GOOS=\$os GOARCH=\$arch go build -trimpath \
        -ldflags '-s -w -X main.version=$VERSION' -o /out/full/cw-\$os-\$arch .
      CGO_ENABLED=0 GOOS=\$os GOARCH=\$arch go build -trimpath -tags terminal \
        -ldflags '-s -w -X main.version=$VERSION' -o /out/term/cw-\$os-\$arch .
    done"

pack_dir() { # name, flavor (full|term), binaries…
  name=$1; flavor=$2; shift 2
  d="$STAGE/$name"
  mkdir -p "$d/bin"
  for b in "$@"; do cp "$STAGE/$flavor/$b" "$d/bin/"; done
  cp "$ROOT/packaging/install.sh" "$ROOT/packaging/LEEME.txt" "$ROOT/packaging/INSTALAR.md" "$d/"
  if [ "$flavor" = full ]; then
    # The desktop packages bring the icons; the installers create shortcuts only when they find them.
    cp "$ROOT/packaging/cw.png" "$ROOT/packaging/cw.icns" "$d/"
  fi
  chmod 755 "$d/install.sh" "$d"/bin/*
}
win_extra() { # name, flavor
  cp "$ROOT/packaging/install.ps1" "$ROOT/packaging/install.cmd" "$STAGE/$1/"
  [ "$2" = full ] && cp "$ROOT/packaging/cw.ico" "$STAGE/$1/"
  true
}

for flavor in full term; do
  prefix=claude-workspaces
  [ "$flavor" = term ] && prefix=cw-terminal
  pack_dir "$prefix-$VERSION-linux" $flavor cw-linux-amd64 cw-linux-arm64
  pack_dir "$prefix-$VERSION-macos" $flavor cw-darwin-amd64 cw-darwin-arm64
  pack_dir "$prefix-$VERSION-windows" $flavor cw-linux-amd64 cw-linux-arm64
  win_extra "$prefix-$VERSION-windows" $flavor
  tar -C "$STAGE" -czf "$OUT/$prefix-$VERSION-linux.tar.gz" "$prefix-$VERSION-linux"
  tar -C "$STAGE" -czf "$OUT/$prefix-$VERSION-macos.tar.gz" "$prefix-$VERSION-macos"
  python3 - "$STAGE" "$prefix-$VERSION-windows" "$OUT/$prefix-$VERSION-windows.zip" <<'PY'
import os, sys, zipfile
stage, name, out = sys.argv[1:]
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as z:
    for root, _, files in os.walk(os.path.join(stage, name)):
        for f in files:
            p = os.path.join(root, f)
            info = zipfile.ZipInfo.from_file(p, os.path.relpath(p, stage))
            info.external_attr = (os.stat(p).st_mode & 0xFFFF) << 16  # keep the exec bit
            with open(p, "rb") as fh:
                z.writestr(info, fh.read(), zipfile.ZIP_DEFLATED)
PY
done
# Compatibility for `cw update` of 0.9 and older, which look for cw-<v>-<os>.tar.gz.
cp "$OUT/claude-workspaces-$VERSION-linux.tar.gz" "$OUT/cw-$VERSION-linux.tar.gz"
cp "$OUT/claude-workspaces-$VERSION-macos.tar.gz" "$OUT/cw-$VERSION-macos.tar.gz"
(cd "$OUT" && sha256sum *-"$VERSION"-* > "cw-$VERSION-SHA256SUMS.txt")
rm -rf "$STAGE"
ls -la "$OUT"
