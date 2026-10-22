# cw — Claude Code workspaces

A terminal app that keeps your Claude Code sessions organised in **tabs**, each tab rooted in a
directory, and saves / restores the whole set with one command. Built on tmux; runs on **macOS**,
**Linux** and **Windows (through WSL)**.

```
 ≡ kyc    bug-185 ●2    front    infra ?1   +                                  ? ayuda
          ──────────    ━━━━━    ────────
   fix-matriz ○   review-arb ● yolo   shell     + Claude    + Terminal      ~/work/frontend
```

- **Desktop app** (`cw app`, new in 0.8) — the same workspaces in a window of their own: a sidebar
  with the tabs, sessions as browser-like tabs, real terminals (xterm.js) in rounded panels,
  floating right-click panels, dialogs and a settings screen. One session per view by default;
  combine views in columns, rows or a grid. The window is the system's Edge/Chrome engine in app
  mode, served by a small local cw server; sessions keep running in tmux when the window closes
  and reappear when it opens again. On Windows the installer adds a «Claude Workspaces» shortcut
  (Start menu and desktop) that runs it inside WSL.
- **Browser shortcuts for the sessions of a tab** (0.9, both versions): Ctrl+T new Claude session,
  Ctrl+W close it, Ctrl+Shift+T reopen the last closed, Ctrl+Tab / Ctrl+PgDn and Ctrl+Shift+Tab /
  Ctrl+PgUp to switch, Ctrl+Shift+PgUp/PgDn to move, Ctrl+1…8 and Ctrl+9. They can be turned off in
  the settings (Ctrl+W otherwise deletes a word in Claude's prompt).
- **Suspend** a session or a whole tab: Claude stops running (no memory, no CPU) while the session
  keeps its place and its conversation, also across stop/start; Enter or ▶ resumes it.
- **Claude usage per account** (`cw uso`, Alt+u, the app's «Uso»): how much of the 5-hour and the
  weekly windows each account behind the aliases has used, when they reset, and the tokens spent.
- **Historial por alias y modelo**: `cw uso` y el panel «Uso» de la app muestran entrada,
  salida, escritura y lectura de caché. Un proceso local lee los transcripts incrementalmente
  cada hora; `usage_interval_seconds` permite cambiar el intervalo (60–86400 segundos, 0 = 1 hora),
  también desde Ajustes → General. Se conservan 30 días. Las conversaciones históricas en
  carpetas compartidas sin atribución fiable aparecen como «Sin atribuir», sin duplicar tokens.
- **Tamaños de sesiones combinadas**: arrastra los separadores en la app con ratón o pantalla
  táctil; también puedes enfocarlos y usar las flechas. Las proporciones se guardan al reiniciar.
  En CLI, arrastra el borde o usa Ctrl+Alt+flechas.
- **Resources**: memory and CPU of every session with all its processes (Alt+m → Recursos, the
  app's «Recursos»).
- **Switch alias when out of quota** (on by default): sessions stopped by an exhausted account
  move to an alias of an account with quota and continue.
- **Skynet, the orchestrator** (off by default; Ajustes → Skynet, or Alt+m → Configuración): a
  Claude session (Sonnet) with no project context that sees every workspace and drives it with
  the cw tools — list tabs, views and sessions, read a session, send it an instruction, answer
  its question, open, close, rename, suspend… It tells you when a session asks something (a card
  with the options), and asks before answering a permission. Chat panel in the app (Alt+K) and
  `cw skynet` (Alt+K) in the terminal. In the app it also listens: say its name (configurable)
  and the instruction; Whisper transcribes on this machine. It runs on an alias with quota and
  moves to another when that account runs out.
- **`cw mcp`**: the same tools as an MCP server (stdio), for any Claude session.
- **Folders**: change a tab's folder (where its new sessions start) and the workspace's default
  folder (where new tabs start).
- **Workspaces** — named sets of tabs. `cw` opens a start screen to create one in the current
  directory or resume a saved one. **Stop** saves the exact state (tabs, directories, sessions,
  their names and which ones were working); **start** reopens everything with `claude --resume`
  and sends a configurable *continue* message to the sessions that were working.
- **Tabs** with a title and a base directory; **sessions** inside them are Claude Code or plain
  shells. Per-tab counters show sessions working (●N) or waiting for a permission (?N).
- **Claude aliases** — named ways of launching claude (env vars + arguments, or your own shell
  alias such as `yolo`, run through your shell). Pick one per session; switching it restarts the
  session in the same conversation, optionally sending the continue message.
  Shell aliases invoking `claude-codex` are supported, and the executable itself
  appears when installed on PATH. Combining views preserves each session's alias
  and process; changing an alias afterwards resumes that session through its launcher.
- Workspaces can be **renamed** (even while open); commands accept the new name or the original id.
- A workspace can also **start from a conversation**: give its ID and it opens in that
  conversation's directory with the conversation resumed.
- An empty tab offers to **resume recent conversations** of its directory, and any conversation
  can be brought in **by its ID** (full or first characters) — for sessions started outside cw.
- Menus, dialogs and confirmations are floating popups, and everything is clickable: buttons,
  fields, lists, cards. Right-click a tab or a session for its menu.
- Every tab and session has a ✕ to close it, and ⏻ in the corner stops the whole workspace.
  Right-click any session to rename it; switch the claude alias of a whole tab or workspace at once.
- Colour themes (blue, purple, green, orange, pink, grey) in the configuration menu.
- **Views with several sessions**: right-click a session → *Ver junto a …*, or *Combinar
  sesiones en esta vista* (menu) to pick several and a layout (side by side, stacked, grid).
  Each pane has its own title bar. Views and layouts are saved and restored.
- **Right-click panels** for tabs, views and sessions stay open while you run several actions
  (move, combine, layout, rename, change alias, split out, close).
- **Reorder** tabs and sessions by dragging them in the bar, from the right-click menu, or with
  `Alt+<` / `Alt+>` (tabs) and `Alt+Shift+←/→` (sessions).

## Packages

Each release ships two packages with the same engine and the same saved format:

| Package | Contents |
|---|---|
| `claude-workspaces-<v>-{windows,macos,linux}` | desktop app (`cw app`) **and** the terminal version, with shortcuts (Start menu, `~/Applications`, Linux menu) |
| `cw-terminal-<v>-{windows,macos,linux}` | terminal version only (built with `-tags terminal`: no web UI, no server) |

`cw update` keeps each install on its own package. (`cw-<v>-{linux,macos}` are kept for the updater of 0.9 and older.)

## Install

Download your package from the
[latest release](https://github.com/Lorenzo2345612/claude-workspaces/releases/latest):
`claude-workspaces-<v>-<os>` for the desktop app (it includes the terminal version too), or
`cw-terminal-<v>-<os>` for the terminal version only. A step-by-step guide in Spanish is in
[`packaging/INSTALAR.md`](packaging/INSTALAR.md).

The installer asks before each step: it installs **tmux ≥ 3.3** (Homebrew / apt / dnf / pacman /
zypper / apk, or builds it from source when the distro ships an older one), copies `cw` to
`~/.local/bin`, creates the app shortcut (desktop package only) and offers to install
**Claude Code** with its official installer.

### macOS

```sh
cd ~/Downloads
tar xzf claude-workspaces-<v>-macos.tar.gz      # or cw-terminal-<v>-macos.tar.gz
cd claude-workspaces-<v>-macos
sh install.sh
```

- Homebrew is offered when missing (it asks for your Mac password).
- The desktop package adds **Claude Workspaces.app** to `~/Applications` (Finder → your home
  folder → Applications; drag it to the Dock). `cw app` in a terminal opens it too.
- The app window is Chrome, Edge, Chromium or Brave in app mode; without any of them it opens as
  a tab of your default browser.
- Open a new terminal afterwards so `~/.local/bin` is on the `PATH`.
- Terminal version: its `Alt` shortcuts use the **Option** key — enable *“Use Option as Meta
  key”* in Terminal (*Settings → Profiles → Keyboard*) or iTerm2 (*Profiles → Keys → Left Option
  key: Esc+*).

### Windows (through WSL)

1. Unzip `claude-workspaces-<v>-windows.zip` (or `cw-terminal-<v>-windows.zip`).
2. Double-click **`install.cmd`**. Without WSL it offers to install Ubuntu (admin permission and
   a restart); run `install.cmd` again afterwards.
3. Open **Claude Workspaces** from the Start menu or the desktop (desktop package). The terminal
   version is the **cw** profile of Windows Terminal, or `cw` in any Ubuntu terminal.

### Linux

```sh
tar xzf claude-workspaces-<v>-linux.tar.gz      # or cw-terminal-<v>-linux.tar.gz
cd claude-workspaces-<v>-linux
sh install.sh
```

The desktop package adds **Claude Workspaces** to the applications menu; `cw app` opens it from a
terminal.

### Updates

Both versions tell you when a new release is out and install it from there (the start screen of
`cw`, a popup when entering a workspace, or a card in the app). `cw update` does it from the
command line. Each install updates from its own package.

## Use

```sh
cw                 # start screen: new workspace here, or resume one
cw new             # new workspace in the current directory
cw new --id ID     # new workspace around an existing Claude conversation (full ID or prefix)
cw ls              # list workspaces
cw stop NAME       # save and shut down
cw update          # self-update to the latest release (--check only reports)
cw help
```

| Key | Action |
|---|---|
| `Alt+m` | menu |
| `Alt+t` | new tab |
| `Alt+n` | new session (Claude or terminal) |
| `Alt+e` | all sessions: edit name / claude alias / arguments, close |
| `Alt+p` | switch the claude alias of the current session |
| `Alt+←/→`, `Alt+↑/↓`, `Alt+1..9` | move between sessions and tabs |
| `Alt+h` | help |

## Abrir Markdown desde Claude

Dentro de una sesión de cw abierta en la app, ejecuta:

```sh
cw open -- docs/plan.md
```

No requiere MCP ni Skynet. Al iniciar sus sesiones en la app, cw añade una instrucción
para que Claude descubra el comando y lo ejecute con Bash cuando le pidas mostrar un
documento, sin modificar el `CLAUDE.md` del proyecto. Las sesiones ya abiertas deben
reiniciarse para recibir esa instrucción. Las rutas relativas parten del directorio actual.

Desde otra terminal, indica el destino antes de la ruta:

```sh
cw open --workspace NOMBRE --session ID -- docs/plan.md
cw open --workspace NOMBRE --tab 1 --view 1 -- docs/plan.md
```

Cada archivo se abre en una subpestaña de la vista, junto a «Sesiones»; abrir el
mismo archivo selecciona su subpestaña existente. Puedes alternar, cerrar documentos
y actualizar su contenido sin detener las sesiones. Las subpestañas se guardan con
el workspace. Admite `.md` y `.markdown`, UTF-8, hasta 2 MiB, en modo de solo lectura.
El visor muestra encabezados, listas, negritas, citas y bloques de código; no ejecuta
HTML ni carga recursos remotos. Requiere un workspace abierto en la app.

## How it tracks Claude sessions

Each new session gets a UUID from cw and starts with `claude --session-id <uuid> --name <name>`.
Hooks passed through `--settings` (they add to your own hooks) record the current session ID —
it changes under `/clear` or `/resume` — and whether the session is working, waiting for a
permission or idle. State lives in `~/.local/share/cw/workspaces/<name>.json` and is autosaved
while a workspace runs; settings in `~/.config/cw/config.json`.

## Build

Requires Go 1.26 (or Docker). `sh packaging/build.sh 0.1.1` cross-compiles every platform in a
`golang` container and writes the packages to `dist/`. `./install.sh` builds and links a local copy.
