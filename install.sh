#!/bin/sh
# Builds cw in a golang container (no local Go toolchain needed) and links it into ~/.local/bin.
set -eu
cd "$(dirname "$0")"
if command -v go >/dev/null 2>&1; then
  CGO_ENABLED=0 go build -o bin/cw .
else
  docker run --rm -u "$(id -u):$(id -g)" -e GOCACHE=/tmp/gocache -e GOPATH=/tmp/gopath \
    -e GOOS="$(uname -s | tr A-Z a-z)" -e GOARCH="$(uname -m | sed 's/x86_64/amd64/;s/aarch64/arm64/')" \
    -v "$PWD":/src -w /src golang:1.26 sh -c 'CGO_ENABLED=0 go build -o bin/cw .'
fi
mkdir -p "$HOME/.local/bin"
ln -sf "$PWD/bin/cw" "$HOME/.local/bin/cw"
command -v tmux >/dev/null 2>&1 || echo "aviso: falta tmux (brew install tmux / apt install tmux)"
echo "listo: $HOME/.local/bin/cw"
