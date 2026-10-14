package main

import (
	"fmt"
	"strings"
)

// Colors of the bar. Backgrounds stay "default" so the bar blends into the terminal theme.
const (
	colAccent = "colour75"  // current tab and its underline
	colDim    = "colour240" // inactive underlines
	colText   = "colour252"
	colMuted  = "colour245"
	colButton = "colour114"
	colBusy   = "colour214"
	colWait   = "colour203"
	colPill   = "colour238"
	colPillOn = "colour31"

	statusRows = 4
)

// tmuxConf renders the private tmux configuration of a workspace. Styles use spaces, never
// commas, because a comma would split the #{?cond,a,b} conditionals they sit in.
//
// The bar is four status lines at the bottom, under the terminal:
//
//	(blank line)
//	 ≡ kyc    bug-185 ●2    front    infra ?1    +                        ? ayuda
//	          ──────────    ━━━━━    ────────
//	   [fix-matriz ○] [review ●] [shell]    + Claude   + Terminal       ~/work/x
//
// Tab labels and their underlines come from refreshTabLabel, which keeps both the same width.
func tmuxConf(ws, bin string, barTop bool) string {
	bq := "'" + bin + "'" // sh-quoted binary, used inside tmux double-quoted strings

	newTabPopup := fmt.Sprintf(`display-popup -E -w 72 -h 18 -T " Nueva pestaña " "%s _ui newtab %s"`, bq, ws)
	newClaudePopup := fmt.Sprintf(`display-popup -E -w 72 -h 24 -T " Nueva sesión de Claude " "%s _ui newclaude %s"`, bq, ws)
	sessionsPopup := fmt.Sprintf(`display-popup -E -w 78 -h 22 -T " Sesiones " "%s _ui sessions %s"`, bq, ws)
	editCurrentPopup := fmt.Sprintf(`display-popup -E -w 72 -h 24 -T " Editar sesión " "%s _ui editcurrent %s"`, bq, ws)
	newShell := fmt.Sprintf(`run-shell "%s _new shell %s '#{session_name}'"`, bq, ws)
	renameTab := fmt.Sprintf(`command-prompt -I "#{@cw_title}" -p "Nombre de la pestaña:" { run-shell "%s _title %s '#{session_name}' '%%%%'" }`, bq, ws)
	configPopup := fmt.Sprintf(`display-popup -E -w 90 -h 17 -T " Configuración " "%s _ui config %s"`, bq, ws)
	switchAliasPopup := fmt.Sprintf(`display-popup -E -w 72 -h 24 -T " Cambiar alias de claude " "%s _ui editcurrent %s"`, bq, ws)
	helpPopup := fmt.Sprintf(`display-popup -E -w 72 -h 26 -T " Ayuda " "%s _ui help %s"`, bq, ws)
	save := fmt.Sprintf(`run-shell "%s save %s" ; display-message "Estado guardado"`, bq, ws)
	confirmStop := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Apagar " "%s _ui confirm %s stop"`, bq, ws)
	confirmCloseTab := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Cerrar pestaña " "%s _ui confirm %s closetab"`, bq, ws)
	confirmClose := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Cerrar sesión " "%s _ui confirm %s close"`, bq, ws)

	slotMenu := fmt.Sprintf(`display-menu -T "#[align=centre] Nueva sesión " -x C -y C "✦ Claude" c { %s } "❯ Terminal" s { %s }`,
		newClaudePopup, newShell)

	// Items whose name starts with "-" are disabled: they serve as group headings.
	mainMenu := strings.Join([]string{
		// "--" ends the options: otherwise the first heading ("-CREAR") parses as a flag.
		fmt.Sprintf(`display-menu -T "#[align=centre] cw · %s " -x C -y C --`, ws),
		`"-CREAR" "" ""`,
		fmt.Sprintf(`"  ✦ Sesión de Claude" c { %s }`, newClaudePopup),
		fmt.Sprintf(`"  ❯ Terminal" s { %s }`, newShell),
		fmt.Sprintf(`"  ▤ Pestaña" t { %s }`, newTabPopup),
		`"-ESTA PESTAÑA" "" ""`,
		fmt.Sprintf(`"  ▸ Renombrar pestaña" r { %s }`, renameTab),
		fmt.Sprintf(`"  ▸ Editar sesión actual" a { %s }`, editCurrentPopup),
		fmt.Sprintf(`"  ✦ Cambiar alias de claude" p { %s }`, switchAliasPopup),
		fmt.Sprintf(`"  ≡ Todas las sesiones" e { %s }`, sessionsPopup),
		fmt.Sprintf(`"  ✕ Cerrar sesión actual" x { %s }`, confirmClose),
		fmt.Sprintf(`"  ✕ Cerrar pestaña" w { %s }`, confirmCloseTab),
		`"-WORKSPACE" "" ""`,
		fmt.Sprintf(`"  ↓ Guardar ahora" g { %s }`, save),
		fmt.Sprintf(`"  ◆ Configuración" o { %s }`, configPopup),
		fmt.Sprintf(`"  ? Ayuda" h { %s }`, helpPopup),
		`"  ⇥ Desconectar" d detach-client`,
		fmt.Sprintf(`"  ⏻ Apagar y guardar" q { %s }`, confirmStop),
	}, " ")

	isCurrent := func(cur, other string) string {
		return fmt.Sprintf(`#{?#{==:#{session_name},#{client_session}},%s,%s}`, cur, other)
	}
	brand := " ≡ " + ws + "  "
	tabsRow := fmt.Sprintf(`#[range=user|menu]#[fg=%s bold]%s#[norange default]`+
		`#{S:#[range=session|#{session_id}]%s  #{@cw_tab_label}  #[norange default]}`+
		`#[range=user|newtab]#[bg=colour237 fg=%s bold] + #[norange default]`+
		`#[align=right]#[range=user|help]#[bg=colour237 fg=%s] ? Ayuda #[norange default] `,
		colAccent, brand, isCurrent(`#[fg=`+colAccent+` bold]`, `#[fg=`+colMuted+`]`), colButton, colMuted)
	underlineRow := strings.Repeat(" ", len([]rune(brand))) +
		fmt.Sprintf(`#{S:#[range=session|#{session_id}]  %s  #[norange default]}`,
			isCurrent(`#[fg=`+colAccent+`]#{@cw_tab_on}`, `#[fg=`+colDim+`]#{@cw_tab_off}`))

	indicator := fmt.Sprintf(`#{?#{==:#{@cw_kind},claude},#{?#{==:#{@cw_status},busy}, #[fg=%s]●,#{?#{==:#{@cw_status},waiting}, #[fg=%s]?,#{?#{==:#{@cw_status},sleeping}, #[fg=%s]z, #[fg=%s]○}}},}`,
		colBusy, colWait, colMuted, colButton)
	pill := func(style string) string {
		// The start screen of an empty tab is not a session: it gets no pill.
		alias := `#{?#{&&:#{@cw_claude_alias},#{!=:#{@cw_claude_alias},claude}}, #[fg=` + colMuted + `]#{@cw_claude_alias},}`
		return fmt.Sprintf(`#{?#{==:#{@cw_kind},home},,#[range=window|#{window_index}]#[%s] #{window_name}%s#[%s]%s #[default]#[norange] }`,
			style, indicator, style, alias)
	}
	sessionsRow := fmt.Sprintf(`   #{W:%s,%s}  #[range=user|newclaude]#[bg=colour237 fg=%s bold] + Claude #[norange default]  `+
		`#[range=user|newshell]#[bg=colour237 fg=%s bold] + Terminal #[norange default]`+
		`#[align=right]#[fg=%s]#{@cw_dir} `,
		pill(`bg=`+colPill+` fg=`+colText), pill(`bg=`+colPillOn+` fg=colour255 bold`), colButton, colButton, colMuted)
	// An empty line keeps the bar from sticking to the terminal output; it also hosts the
	// autosave job, which prints nothing.
	spacerRow := fmt.Sprintf(`#(%s _autosave %s)`, bq, ws)
	rows := []string{spacerRow, tabsRow, underlineRow, sessionsRow}
	position := "bottom"
	if barTop {
		// Tabs first, sessions under them, and the blank line next to the terminal.
		rows = []string{tabsRow, underlineRow, sessionsRow, spacerRow}
		position = "top"
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	w("# Generated by cw for workspace %q — regenerated on every start; do not edit.", ws)
	w(`set -g default-terminal "tmux-256color"`)
	w(`set -as terminal-features ",xterm*:RGB:extkeys:clipboard,*256col*:RGB"`)
	w(`set -g extended-keys on`)
	w(`set -g escape-time 10`)
	w(`set -g history-limit 50000`)
	w(`set -g mouse on`)
	w(`set -g set-clipboard on`)
	w(`set -g allow-passthrough on`)
	w(`set -g focus-events on`)
	w(`set -g base-index 1`)
	w(`set -g renumber-windows on`)
	w(`set -g detach-on-destroy off`)
	w(`set -g exit-empty on`)
	w(`setw -g pane-base-index 1`)
	w(`setw -g allow-rename off`)
	w(`setw -g automatic-rename off`)
	w(`setw -g remain-on-exit failed`)
	w(`setw -g remain-on-exit-format " La sesión terminó con error (código #{pane_dead_status}) · Enter: reabrir · Alt+m → Cerrar sesión "`)
	w(`set -g message-style "bg=colour236 fg=%s"`, colText)
	w(`set -g mode-style "bg=colour31 fg=colour255"`)
	w(`set -g menu-border-lines rounded`)
	w(`set -g popup-border-lines rounded`)
	w(`set -g popup-border-style "fg=%s"`, colAccent)
	w(`set -g menu-border-style "fg=%s"`, colAccent)
	// Ctrl+B is Claude Code's "run in background" key, so tmux gets another prefix.
	w(`set -g prefix M-Space`)
	w(`unbind C-b`)
	w(`bind M-Space send-prefix`)
	w(`set -g status %d`, statusRows)
	w(`set -g status-position %s`, position)
	w(`set -g status-interval 15`)
	w(`set -g status-style "bg=default fg=%s"`, colText)
	for i, row := range rows {
		w(`set -g status-format[%d] "%s"`, i, row)
	}
	w(`set-hook -g client-attached { set -gF @cw_active "#{client_session}" }`)
	w(`set-hook -g client-session-changed { set -gF @cw_active "#{client_session}" }`)
	w(`bind -n M-m { %s }`, mainMenu)
	w(`bind -n M-t { %s }`, newTabPopup)
	w(`bind -n M-n { %s }`, slotMenu)
	w(`bind -n M-h { %s }`, helpPopup)
	w(`bind -n M-e { %s }`, sessionsPopup)
	w(`bind -n M-p { %s }`, switchAliasPopup)
	// Closing a session changes its tab's counters.
	w(`set-hook -g window-unlinked { run-shell -b "%s _labels %s" }`, bq, ws)
	// Enter on a session that exited with an error reopens it; anywhere else it is a plain Enter.
	w(`bind -n Enter { if -F "#{pane_dead}" { run-shell "%s _respawn %s '#{pane_id}'" } { send-keys Enter } }`, bq, ws)
	// Reordering: Alt+Shift+←/→ moves the current session, Alt+< / Alt+> the current tab.
	w(`bind -n M-S-Left swap-window -d -t -1`)
	w(`bind -n M-S-Right swap-window -d -t +1`)
	w(`bind -n M-< run-shell "%s _tabmove %s '#{client_session}' -1"`, bq, ws)
	w(`bind -n M-> run-shell "%s _tabmove %s '#{client_session}' +1"`, bq, ws)
	// Dragging in the bar: a session pill swaps with the one under the mouse; a tab moves to the
	// position of the tab under the mouse.
	w(`bind -n MouseDrag1Status { if -F "#{==:#{mouse_status_range},window}" { run-shell -t = "%[1]s _winmove %[2]s '#{client_session}' '#{window_id}'" } { if -F "#{==:#{mouse_status_range},session}" { run-shell -t = "%[1]s _tabmove %[2]s '#{client_session}' '#{session_name}'" } } }`, bq, ws)
	// Right click: a context menu for the tab or the session under the mouse.
	tabMenu := fmt.Sprintf(`display-menu -T "#[align=centre] #{@cw_title} " -x M -y S "← Mover a la izquierda" "" { run-shell "%[1]s _tabmove %[2]s '#{client_session}' -1" } "→ Mover a la derecha" "" { run-shell "%[1]s _tabmove %[2]s '#{client_session}' +1" } "" "▸ Renombrar pestaña" "" { %[3]s } "✕ Cerrar pestaña" "" { %[4]s }`,
		bq, ws, renameTab, confirmCloseTab)
	slotCtx := fmt.Sprintf(`display-menu -T "#[align=centre] #W " -x M -y S "← Mover a la izquierda" "" { swap-window -d -t -1 } "→ Mover a la derecha" "" { swap-window -d -t +1 } "" "▸ Editar sesión" "" { %s } "✦ Cambiar alias de claude" "" { %s } "✕ Cerrar sesión" "" { %s }`,
		editCurrentPopup, switchAliasPopup, confirmClose)
	w(`bind -n MouseDown3Status { if -F "#{==:#{mouse_status_range},session}" { switch-client -t = ; %s } { if -F "#{==:#{mouse_status_range},window}" { select-window -t = ; %s } } }`, tabMenu, slotCtx)
	w(`bind -n M-Left previous-window`)
	w(`bind -n M-Right next-window`)
	w(`bind -n M-Up switch-client -p`)
	w(`bind -n M-Down switch-client -n`)
	for i := 1; i <= 9; i++ {
		w(`bind -n M-%d select-window -t :=%d`, i, i)
	}
	// Wheel up enters scroll mode only when there is history to scroll; with an empty history
	// tmux would sit in copy mode ("[0/0]") swallowing keystrokes. -e leaves it at the bottom.
	w(`bind -n WheelUpPane { if -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send -M } { if -F "#{history_size}" { copy-mode -e } } }`)
	clicks := []struct{ rng, cmd string }{
		{"menu", mainMenu}, {"newtab", newTabPopup}, {"newclaude", newClaudePopup}, {"newshell", newShell},
		{"help", helpPopup},
	}
	// A click on a session pill selects that window; a click on a tab switches to that tab.
	click := `if -F "#{==:#{mouse_status_range},window}" { select-window -t = } { switch-client -t = }`
	for i := len(clicks) - 1; i >= 0; i-- {
		click = fmt.Sprintf(`if -F "#{==:#{mouse_status_range},%s}" { %s } { %s }`, clicks[i].rng, clicks[i].cmd, click)
	}
	w(`bind -n MouseDown1Status { %s }`, click)
	return b.String()
}
