#!/bin/sh
# Builds the cw binaries (in a golang container) and the install packages into dist/.
#   sh packaging/build.sh 0.1.0 [output-dir]
set -eu
VERSION=${1:?uso: build.sh VERSION [DIR]}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
OUT=${2:-$ROOT/dist}
STAGE=$(mktemp -d)
mkdir -p "$OUT"

docker run --rm -u "$(id -u):$(id -g)" -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
  -v "$ROOT":/src -v "$STAGE":/out -w /src golang:1.26 sh -c "
    set -e
    for target in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do
      os=\${target%/*}; arch=\${target#*/}
      CGO_ENABLED=0 GOOS=\$os GOARCH=\$arch go build -trimpath \
        -ldflags '-s -w -X main.version=$VERSION' -o /out/cw-\$os-\$arch .
    done"

pack_dir() { # name, binaries…
  name=$1; shift
  d="$STAGE/$name"
  mkdir -p "$d/bin"
  for b in "$@"; do cp "$STAGE/$b" "$d/bin/"; done
  cp "$ROOT/packaging/install.sh" "$ROOT/packaging/LEEME.txt" "$ROOT/packaging/INSTALAR.md" "$d/"
  chmod 755 "$d/install.sh" "$d"/bin/*
}

pack_dir "cw-$VERSION-linux" cw-linux-amd64 cw-linux-arm64
pack_dir "cw-$VERSION-macos" cw-darwin-amd64 cw-darwin-arm64
pack_dir "cw-$VERSION-windows" cw-linux-amd64 cw-linux-arm64
cp "$ROOT/packaging/install.ps1" "$ROOT/packaging/install.cmd" "$STAGE/cw-$VERSION-windows/"

tar -C "$STAGE" -czf "$OUT/cw-$VERSION-linux.tar.gz" "cw-$VERSION-linux"
tar -C "$STAGE" -czf "$OUT/cw-$VERSION-macos.tar.gz" "cw-$VERSION-macos"
python3 - "$STAGE" "cw-$VERSION-windows" "$OUT/cw-$VERSION-windows.zip" <<'PY'
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
(cd "$OUT" && sha256sum "cw-$VERSION-"* > "cw-$VERSION-SHA256SUMS.txt")
rm -rf "$STAGE"
ls -la "$OUT"
