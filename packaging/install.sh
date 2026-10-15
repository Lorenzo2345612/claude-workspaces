#!/bin/sh
# Installer for cw (Claude Code workspaces) — macOS and Linux (including WSL).
#
# Run from the unpacked package:   sh install.sh          (asks before installing anything)
#                                  sh install.sh --yes    (accepts every question)
#                                  sh install.sh --no-claude
#
# What it does:
#   1. installs tmux >= 3.3 if missing or too old (Homebrew on macOS; apt/dnf/pacman/zypper/apk
#      on Linux; builds tmux from source when the distro's package is too old, e.g. Ubuntu 22.04);
#   2. copies the cw binary for this OS/CPU into ~/.local/bin and makes sure it is on the PATH;
#   3. desktop package only: adds «Claude Workspaces» to the Linux menu or ~/Applications on macOS;
#   4. offers to install Claude Code with its official installer when `claude` is missing.
set -eu

TMUX_SOURCE_VERSION=3.5a
MIN_TMUX_MAJOR=3
MIN_TMUX_MINOR=3
BIN_DIR="${CW_BIN_DIR:-$HOME/.local/bin}"
HERE=$(cd "$(dirname "$0")" && pwd)

YES=0
CLAUDE=1
for arg in "$@"; do
  case "$arg" in
    --yes|-y) YES=1 ;;
    --no-claude) CLAUDE=0 ;;
    -h|--help) sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "opción desconocida: $arg" >&2; exit 2 ;;
  esac
done

say()  { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m aviso:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31m error:\033[0m %s\n' "$*" >&2; exit 1; }

ask() { # ask "question" → 0 yes / 1 no (default yes)
  [ "$YES" = 1 ] && return 0
  if [ ! -t 0 ] && [ ! -r /dev/tty ]; then return 0; fi
  printf '%s [S/n] ' "$1"
  read -r answer < /dev/tty || answer=""
  case "$answer" in n|N|no|No|NO) return 1 ;; *) return 0 ;; esac
}

SUDO=""
need_sudo() {
  if [ "$(id -u)" -ne 0 ]; then
    command -v sudo >/dev/null 2>&1 || die "hace falta sudo para instalar paquetes del sistema"
    SUDO=sudo
  fi
}

# ---------------------------------------------------------------- platform
OS=$(uname -s)
case "$OS" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *) die "sistema no soportado: $OS (en Windows usa install.ps1, que instala dentro de WSL)" ;;
esac
ARCH=$(uname -m)
case "$ARCH" in
  x86_64|amd64) ARCH=amd64 ;;
  arm64|aarch64) ARCH=arm64 ;;
  *) die "procesador no soportado: $ARCH" ;;
esac

BINARY="$HERE/bin/cw-$OS-$ARCH"
[ -f "$BINARY" ] || BINARY="$HERE/cw"
[ -f "$BINARY" ] || die "este paquete no trae cw para $OS/$ARCH"

# ---------------------------------------------------------------- tmux
tmux_ok() {
  command -v tmux >/dev/null 2>&1 || return 1
  v=$(tmux -V 2>/dev/null | sed -n 's/[^0-9]*\([0-9][0-9]*\)\.\([0-9][0-9]*\).*/\1 \2/p')
  [ -n "$v" ] || return 0
  set -- $v
  [ "$1" -gt "$MIN_TMUX_MAJOR" ] || { [ "$1" -eq "$MIN_TMUX_MAJOR" ] && [ "$2" -ge "$MIN_TMUX_MINOR" ]; }
}

install_tmux_package() {
  if [ "$OS" = darwin ]; then
    if ! command -v brew >/dev/null 2>&1; then
      warn "tmux se instala con Homebrew y no lo tienes."
      ask "¿Instalar Homebrew ahora (instalador oficial de brew.sh)?" || die "instala Homebrew (https://brew.sh) y vuelve a correr este instalador"
      /bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
      for b in /opt/homebrew/bin/brew /usr/local/bin/brew; do [ -x "$b" ] && eval "$("$b" shellenv)"; done
    fi
    brew install tmux || brew upgrade tmux
    return
  fi
  need_sudo
  if command -v apt-get >/dev/null 2>&1; then $SUDO apt-get update -qq && $SUDO apt-get install -y tmux
  elif command -v dnf >/dev/null 2>&1; then $SUDO dnf install -y tmux
  elif command -v pacman >/dev/null 2>&1; then $SUDO pacman -Sy --noconfirm tmux
  elif command -v zypper >/dev/null 2>&1; then $SUDO zypper --non-interactive install tmux
  elif command -v apk >/dev/null 2>&1; then $SUDO apk add tmux
  else die "no reconozco el gestor de paquetes; instala tmux ${MIN_TMUX_MAJOR}.${MIN_TMUX_MINOR}+ a mano"
  fi
}

# Some distros (Ubuntu 22.04: 3.2a) ship a tmux too old for cw: build a current one into /usr/local.
build_tmux_from_source() {
  need_sudo
  if command -v apt-get >/dev/null 2>&1; then
    $SUDO apt-get install -y build-essential pkg-config bison libevent-dev libncurses-dev curl
  elif command -v dnf >/dev/null 2>&1; then
    $SUDO dnf install -y gcc make pkgconf-pkg-config bison libevent-devel ncurses-devel curl
  elif command -v zypper >/dev/null 2>&1; then
    $SUDO zypper --non-interactive install gcc make pkg-config bison libevent-devel ncurses-devel curl
  else
    die "no sé compilar tmux en esta distro; instala tmux ${MIN_TMUX_MAJOR}.${MIN_TMUX_MINOR}+ a mano"
  fi
  tmp=$(mktemp -d)
  say "Compilando tmux $TMUX_SOURCE_VERSION (un par de minutos)…"
  curl -fsSL "https://github.com/tmux/tmux/releases/download/$TMUX_SOURCE_VERSION/tmux-$TMUX_SOURCE_VERSION.tar.gz" \
    | tar -xz -C "$tmp"
  (cd "$tmp/tmux-$TMUX_SOURCE_VERSION" && ./configure --prefix=/usr/local >/dev/null && make -j"$(nproc 2>/dev/null || echo 2)" >/dev/null)
  $SUDO make -C "$tmp/tmux-$TMUX_SOURCE_VERSION" install >/dev/null
  rm -rf "$tmp"
  hash -r 2>/dev/null || true
}

if tmux_ok; then
  say "tmux ya está: $(tmux -V)"
else
  if command -v tmux >/dev/null 2>&1; then
    warn "tu tmux es $(tmux -V | cut -d' ' -f2); cw necesita ${MIN_TMUX_MAJOR}.${MIN_TMUX_MINOR} o mayor."
  else
    say "Falta tmux."
  fi
  ask "¿Instalar tmux?" || die "cw no funciona sin tmux ${MIN_TMUX_MAJOR}.${MIN_TMUX_MINOR}+"
  install_tmux_package || true
  if ! tmux_ok; then
    [ "$OS" = linux ] || die "no pude instalar un tmux reciente"
    warn "el paquete de tu distro sigue siendo viejo ($(tmux -V 2>/dev/null || echo 'sin tmux'))."
    ask "¿Compilar tmux $TMUX_SOURCE_VERSION desde el código fuente en /usr/local?" || die "cw necesita tmux ${MIN_TMUX_MAJOR}.${MIN_TMUX_MINOR}+"
    build_tmux_from_source
    PATH="/usr/local/bin:$PATH"
    tmux_ok || die "tmux sigue sin funcionar después de compilarlo"
  fi
  say "tmux listo: $(tmux -V)"
fi

# ---------------------------------------------------------------- cw
mkdir -p "$BIN_DIR"
cp "$BINARY" "$BIN_DIR/cw.new"
chmod 755 "$BIN_DIR/cw.new"
[ "$OS" = darwin ] && xattr -d com.apple.quarantine "$BIN_DIR/cw.new" 2>/dev/null || true
mv -f "$BIN_DIR/cw.new" "$BIN_DIR/cw"
say "cw instalado en $BIN_DIR/cw ($("$BIN_DIR/cw" version))"

case ":$PATH:" in
  *":$BIN_DIR:"*) ;;
  *)
    rc="$HOME/.bashrc"
    case "${SHELL:-}" in *zsh) rc="$HOME/.zshrc" ;; esac
    line="export PATH=\"$BIN_DIR:\$PATH\""
    if ! grep -qsF "$line" "$rc"; then
      if ask "$BIN_DIR no está en tu PATH. ¿Agregarlo a $rc?"; then
        printf '\n# cw\n%s\n' "$line" >> "$rc"
        say "agregado a $rc (abre una terminal nueva)"
      else
        warn "agrega $BIN_DIR a tu PATH para poder escribir cw"
      fi
    fi
    ;;
esac

# ---------------------------------------------------------------- app launcher
# A menu entry for the desktop app (cw app) on Linux desktops; Windows gets its shortcut from
# install.ps1, macOS opens it with `cw app`.
if [ "$OS" = linux ] && [ -z "${WSL_DISTRO_NAME:-}" ] && [ -f "$HERE/cw.png" ]; then
  apps="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
  icons="${XDG_DATA_HOME:-$HOME/.local/share}/icons"
  mkdir -p "$apps" "$icons"
  cp "$HERE/cw.png" "$icons/claude-workspaces.png"
  cat > "$apps/claude-workspaces.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Claude Workspaces
Comment=Sesiones de Claude Code en pestañas
Exec=$BIN_DIR/cw app
Icon=$icons/claude-workspaces.png
Terminal=false
Categories=Development;
DESKTOP
  say "Agregada «Claude Workspaces» al menú de aplicaciones."
fi

# macOS: «Claude Workspaces.app» in ~/Applications, for the desktop package (it brings cw.icns).
if [ "$OS" = darwin ] && [ -f "$HERE/cw.icns" ]; then
  app="$HOME/Applications/Claude Workspaces.app"
  mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
  cp "$HERE/cw.icns" "$app/Contents/Resources/cw.icns"
  cat > "$app/Contents/MacOS/claude-workspaces" <<APP
#!/bin/sh
# Opened from Finder or the Dock the PATH is the bare system one: Homebrew and ~/.local/bin first.
export PATH="/opt/homebrew/bin:/usr/local/bin:\$HOME/.local/bin:\$PATH"
mkdir -p "\$HOME/.local/share/cw/run"
exec "$BIN_DIR/cw" app --mantener >>"\$HOME/.local/share/cw/run/app-launch.log" 2>&1
APP
  chmod 755 "$app/Contents/MacOS/claude-workspaces"
  cat > "$app/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>Claude Workspaces</string>
  <key>CFBundleIdentifier</key><string>com.github.lorenzo2345612.claude-workspaces</string>
  <key>CFBundleExecutable</key><string>claude-workspaces</string>
  <key>CFBundleIconFile</key><string>cw.icns</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSUIElement</key><true/>
</dict></plist>
PLIST
  say "Agregada «Claude Workspaces» a ~/Applications."
fi

# ---------------------------------------------------------------- Claude Code
if command -v claude >/dev/null 2>&1 || [ -x "$HOME/.local/bin/claude" ]; then
  say "Claude Code ya está instalado."
elif [ "$CLAUDE" = 1 ]; then
  if ask "No encuentro Claude Code. ¿Instalarlo con el instalador oficial (claude.ai/install.sh)?"; then
    curl -fsSL https://claude.ai/install.sh | bash
    say "Claude Code instalado. La primera vez, corre 'claude' para iniciar sesión."
  else
    warn "cw necesita Claude Code: https://code.claude.com/docs/en/setup"
  fi
fi

if [ -f "$HERE/cw.png" ]; then
  say "Listo. Abre «Claude Workspaces» (o escribe cw app); cw abre la versión de terminal."
else
  say "Listo. Escribe cw para empezar."
fi
