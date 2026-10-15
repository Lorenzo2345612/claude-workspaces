# cw — Claude Code workspaces

A terminal app that keeps your Claude Code sessions organised in **tabs**, each tab rooted in a
directory, and saves / restores the whole set with one command. Built on tmux; runs on **macOS**,
**Linux** and **Windows (through WSL)**.

```
 ≡ kyc    bug-185 ●2    front    infra ?1   +                                  ? ayuda
          ──────────    ━━━━━    ────────
   fix-matriz ○   review-arb ● yolo   shell     + Claude    + Terminal      ~/work/frontend
```

- **Workspaces** — named sets of tabs. `cw` opens a start screen to create one in the current
  directory or resume a saved one. **Stop** saves the exact state (tabs, directories, sessions,
  their names and which ones were working); **start** reopens everything with `claude --resume`
  and sends a configurable *continue* message to the sessions that were working.
- **Tabs** with a title and a base directory; **sessions** inside them are Claude Code or plain
  shells. Per-tab counters show sessions working (●N) or waiting for a permission (?N).
- **Claude aliases** — named ways of launching claude (env vars + arguments, or your own shell
  alias such as `yolo`, run through your shell). Pick one per session; switching it restarts the
  session in the same conversation, optionally sending the continue message.
- An empty tab offers to **resume recent conversations** of its directory, and any conversation
  can be brought in **by its ID** (full or first characters) — for sessions started outside cw.
- Menus, dialogs and confirmations are floating popups, and everything is clickable: buttons,
  fields, lists, cards. Right-click a tab or a session for its menu.
- Every tab and session has a ✕ to close it, and ⏻ in the corner stops the whole workspace.
  Right-click any session to rename it; switch the claude alias of a whole tab or workspace at once.
- Colour themes (blue, purple, green, orange, pink, grey) in the configuration menu.
- **Views with several sessions**: drag a session from the bar and drop it onto the terminal
  to see it next to the one there, or use *Combinar sesiones en esta vista* (menu) to pick
  several and a layout (side by side, stacked, grid). Each pane has its own title bar; right-click
  a pane to split it back out or change the layout. Views and layouts are saved and restored.
- **Reorder** tabs and sessions by dragging them in the bar, from the right-click menu, or with
  `Alt+<` / `Alt+>` (tabs) and `Alt+Shift+←/→` (sessions).

## Install

Download the package for your system from the
[latest release](https://github.com/Lorenzo2345612/claude-workspaces/releases/latest) and follow
[`packaging/INSTALAR.md`](packaging/INSTALAR.md) (Spanish). In short:

| System | Steps |
|---|---|
| macOS / Linux | `tar xzf cw-*-macos.tar.gz` (or `-linux`), then `sh install.sh` |
| Windows | unzip `cw-*-windows.zip`, double-click `install.cmd` (installs into WSL and adds a «cw» Windows Terminal profile) |

The installer asks before installing **tmux ≥ 3.3** (Homebrew / apt / dnf / pacman / zypper / apk,
or builds it from source when the distro ships an older one), copies `cw` to `~/.local/bin`, and
offers to install **Claude Code** with its official installer.

On macOS the `Alt` shortcuts use the **Option** key: enable *“Use Option as Meta key”* in your
terminal.

## Use

```sh
cw                 # start screen: new workspace here, or resume one
cw new             # new workspace in the current directory
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

## How it tracks Claude sessions

Each new session gets a UUID from cw and starts with `claude --session-id <uuid> --name <name>`.
Hooks passed through `--settings` (they add to your own hooks) record the current session ID —
it changes under `/clear` or `/resume` — and whether the session is working, waiting for a
permission or idle. State lives in `~/.local/share/cw/workspaces/<name>.json` and is autosaved
while a workspace runs; settings in `~/.config/cw/config.json`.

## Build

Requires Go 1.26 (or Docker). `sh packaging/build.sh 0.1.1` cross-compiles every platform in a
`golang` container and writes the packages to `dist/`. `./install.sh` builds and links a local copy.
