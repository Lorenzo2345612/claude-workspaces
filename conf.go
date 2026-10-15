package main

import (
	"fmt"
	"strings"
)

// colAccent (current tab, borders) and colPillOn (current session) come from the theme: theme.go.

// Colors of the bar. Backgrounds stay "default" so the bar blends into the terminal theme.
const (
	colDim    = "colour240" // inactive underlines
	colText   = "colour252"
	colMuted  = "colour245"
	colButton = "colour114"
	colBusy   = "colour214"
	colWait   = "colour203"
	colPill   = "colour238"

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
	renameTab := fmt.Sprintf(`display-popup -E -w 70 -h 10 -T " Renombrar pestaña " "%s _ui rename %s tab"`, bq, ws)
	configMenu := fmt.Sprintf(`run-shell -b "%s _configmenu %s '#{client_name}'"`, bq, ws)
	renameSlot := fmt.Sprintf(`display-popup -E -w 70 -h 10 -T " Renombrar sesión " "%s _ui rename %s slot"`, bq, ws)
	bulkTab := fmt.Sprintf(`display-popup -E -w 70 -h 16 -T " Alias de claude de la pestaña " "%s _ui bulkalias %s tab"`, bq, ws)
	bulkAll := fmt.Sprintf(`display-popup -E -w 70 -h 16 -T " Alias de claude del workspace " "%s _ui bulkalias %s all"`, bq, ws)
	switchAliasPopup := fmt.Sprintf(`display-popup -E -w 72 -h 24 -T " Cambiar alias de claude " "%s _ui editcurrent %s"`, bq, ws)
	helpPopup := fmt.Sprintf(`display-popup -E -w 82 -h 30 -T " Ayuda " "%s _ui help %s"`, bq, ws)
	save := fmt.Sprintf(`run-shell "%s save %s" ; display-message "Estado guardado"`, bq, ws)
	resumeIDPopup := fmt.Sprintf(`display-popup -E -w 84 -h 20 -T " Retomar sesión de Claude por ID " "%s _ui resumeid %s"`, bq, ws)
	combinePopup := fmt.Sprintf(`display-popup -E -w 78 -h 24 -T " Combinar sesiones en esta vista " "%s _ui combine %s"`, bq, ws)
	splitOut := fmt.Sprintf(`run-shell "%s _splitout %s '#{pane_id}'"`, bq, ws)
	layoutCmd := func(name string) string {
		return fmt.Sprintf(`run-shell "%s _layout %s '#{window_id}' %s"`, bq, ws, name)
	}
	confirmStop := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Apagar " "%s _ui confirm %s stop"`, bq, ws)
	confirmCloseTab := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Cerrar pestaña " "%s _ui confirm %s closetab"`, bq, ws)
	confirmClose := fmt.Sprintf(`display-popup -E -w 66 -h 13 -T " Cerrar sesión " "%s _ui confirm %s close"`, bq, ws)

	slotMenu := fmt.Sprintf(`display-menu -T "#[align=centre] Nueva sesión " -x C -y C "✦ Claude" c { %s } "↻ Claude por ID…" i { %s } "❯ Terminal" s { %s }`,
		newClaudePopup, resumeIDPopup, newShell)

	// Items whose name starts with "-" are disabled: they serve as group headings.
	mainMenu := strings.Join([]string{
		// "--" ends the options: otherwise the first heading ("-CREAR") parses as a flag.
		fmt.Sprintf(`display-menu -T "#[align=centre] cw %s · #{@cw_ws_title} " -x C -y C --`, version),
		`"-CREAR" "" ""`,
		fmt.Sprintf(`"  ✦ Sesión de Claude" c { %s }`, newClaudePopup),
		fmt.Sprintf(`"  ↻ Retomar sesión por ID…" i { %s }`, resumeIDPopup),
		fmt.Sprintf(`"  ❯ Terminal" s { %s }`, newShell),
		fmt.Sprintf(`"  ▤ Pestaña" t { %s }`, newTabPopup),
		`"-ESTA PESTAÑA" "" ""`,
		fmt.Sprintf(`"  ▸ Renombrar pestaña" r { %s }`, renameTab),
		fmt.Sprintf(`"  ▸ Renombrar sesión" n { %s }`, renameSlot),
		fmt.Sprintf(`"  ▸ Editar sesión actual" a { %s }`, editCurrentPopup),
		fmt.Sprintf(`"  ✦ Cambiar alias de claude" p { %s }`, switchAliasPopup),
		fmt.Sprintf(`"  ✦ Alias de claude de la pestaña" k { %s }`, bulkTab),
		fmt.Sprintf(`"  ≡ Todas las sesiones" e { %s }`, sessionsPopup),
		`"-VISTA" "" ""`,
		fmt.Sprintf(`"  ⧉ Combinar sesiones en esta vista…" v { %s }`, combinePopup),
		fmt.Sprintf(`"#{?#{e|>:#{window_panes},1},,-}  ⇱ Separar esta sesión de la vista" u { %s }`, splitOut),
		fmt.Sprintf(`"#{?#{e|>:#{window_panes},1},,-}  ▥ Lado a lado" "" { %s }`, layoutCmd("lado")),
		fmt.Sprintf(`"#{?#{e|>:#{window_panes},1},,-}  ▤ Arriba y abajo" "" { %s }`, layoutCmd("apilado")),
		fmt.Sprintf(`"#{?#{e|>:#{window_panes},1},,-}  ▦ Cuadrícula" "" { %s }`, layoutCmd("cuadricula")),
		fmt.Sprintf(`"  ✕ Cerrar sesión actual" x { %s }`, confirmClose),
		fmt.Sprintf(`"  ✕ Cerrar pestaña" w { %s }`, confirmCloseTab),
		`"-WORKSPACE" "" ""`,
		fmt.Sprintf(`"  ↓ Guardar ahora" g { %s }`, save),
		fmt.Sprintf(`"  ▸ Renombrar workspace…" W { display-popup -E -w 70 -h 10 -T " Renombrar workspace " "%s _ui renamews %s" }`, bq, ws),
		fmt.Sprintf(`"  ✦ Alias de claude de todo el workspace" K { %s }`, bulkAll),
		fmt.Sprintf(`"  ◆ Configuración" o { %s }`, configMenu),
		fmt.Sprintf(`"  ? Ayuda" h { %s }`, helpPopup),
		`"  ⇥ Desconectar" d detach-client`,
		fmt.Sprintf(`"  ⏻ Apagar y guardar" q { %s }`, confirmStop),
	}, " ")

	isCurrent := func(cur, other string) string {
		return fmt.Sprintf(`#{?#{==:#{session_name},#{client_session}},%s,%s}`, cur, other)
	}
	// The brand shows the workspace's title (renamable while it runs); the underline row pads
	// with one space per character of it.
	brand := " ≡ #{@cw_ws_title}  "
	tabsRow := fmt.Sprintf(`#[range=user|menu]#[fg=%s bold]%s#[norange default]`+
		`#{S:#[range=session|#{session_id}]%s  #{@cw_tab_label}#[norange] #[range=user|xt:#{session_name}]#[fg=%s]✕#[norange default]  }`+
		`#[range=user|newtab]#[bg=colour237 fg=%s bold] + #[norange default]`+
		`#[align=right]#[range=user|help]#[bg=colour237 fg=%s] ? Ayuda #[norange default] `+
		`#[range=user|stop]#[bg=colour52 fg=colour217 bold] ⏻ #[norange default] `,
		colAccent, brand, isCurrent(`#[fg=`+colAccent+` bold]`, `#[fg=`+colMuted+`]`), colDim, colButton, colMuted)
	// The underline spans the label and its ✕ (refreshTabLabel sizes it to match).
	underlineRow := "   #{s/./ /:@cw_ws_title}  " +
		fmt.Sprintf(`#{S:#[range=session|#{session_id}]  %s  #[norange default]}`,
			isCurrent(`#[fg=`+colAccent+`]#{@cw_tab_on}`, `#[fg=`+colDim+`]#{@cw_tab_off}`))

	indicator := fmt.Sprintf(`#{?#{==:#{@cw_kind},claude},#{?#{==:#{@cw_status},busy}, #[fg=%s]●,#{?#{==:#{@cw_status},waiting}, #[fg=%s]?,#{?#{==:#{@cw_status},sleeping}, #[fg=%s]z, #[fg=%s]○}}},}`,
		colBusy, colWait, colMuted, colButton)
	pill := func(style string) string {
		// The start screen of an empty tab is not a session: it gets no pill.
		alias := `#{?#{&&:#{@cw_claude_alias},#{!=:#{@cw_claude_alias},claude}}, #[fg=` + colMuted + `]#{@cw_claude_alias},}`
		// A tmux range also claims the cell right after it, so a space outside any range separates
		// the session's range from its ✕ (otherwise clicking the ✕ would select the session).
		// A view showing several sessions lists them all: "⧉ api ● · ui ○".
		item := fmt.Sprintf(`#{@cw_alias}%s#[%s]%s#{?#{e|<:#{pane_index},#{window_panes}}, · ,}`, indicator, style, alias)
		return fmt.Sprintf(`#{?#{==:#{@cw_kind},home},,#[range=window|#{window_index}]#[%s] #{?#{e|>:#{window_panes},1},⧉ ,}#{P:%s,%s}#[norange] #[range=user|xw:#{window_id}]#[%s]✕#[norange] #[default] }`,
			style, item, item, style)
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
	// Views with several sessions show a title bar per session.
	w(`set -g pane-border-lines heavy`)
	w(`set -g pane-border-style "fg=%s"`, colDim)
	w(`set -g pane-active-border-style "fg=%s"`, colAccent)
	w(`set -g pane-border-format "#{?pane_active,#[fg=%s bold],#[fg=%s]} #{@cw_alias}#{?#{==:#{@cw_kind},claude}, #{?#{==:#{@cw_status},busy},●,#{?#{==:#{@cw_status},waiting},?,○}},} "`, colAccent, colMuted)
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
	// Pills swap natively (run-shell -C runs a tmux command, no process): the dragged session
	// follows the mouse. Tabs only call cw when the mouse reaches another tab.
	// Swap only towards where the mouse is going: items have different widths, so right after a
	// swap the mouse can sit on the item just moved past, which would swap it straight back.
	right := `#{e|>:#{mouse_x},#{@cw_dragx}}`
	left := `#{e|<:#{mouse_x},#{@cw_dragx}}`
	winDir := fmt.Sprintf(`#{||:#{&&:%s,#{e|>:#{window_index},#{active_window_index}}},#{&&:%s,#{e|<:#{window_index},#{active_window_index}}}}`, right, left)
	tabDir := fmt.Sprintf(`#{||:#{&&:%s,#{>:#{session_name},#{client_session}}},#{&&:%s,#{<:#{session_name},#{client_session}}}}`, right, left)
	w(`bind -n MouseDrag1Status { if -F "#{&&:#{==:#{mouse_status_range},window},%s}" { run-shell -C "swap-window -s #{session_name}:#{active_window_index} -t #{session_name}:#{window_index} ; select-window -t #{session_name}:#{window_index}" } { if -F "#{&&:#{==:#{mouse_status_range},session},%s}" { run-shell -t = -C "rename-session -t #{client_session} cw-swap ; rename-session -t #{session_name} #{client_session} ; rename-session -t cw-swap #{session_name} ; set -g @cw_active #{session_name}" } } ; set -gF @cw_dragx "#{mouse_x}" }`, winDir, tabDir)
	// Right click: a context menu for the tab or the session under the mouse.
	tabMenu := fmt.Sprintf(`display-menu -T "#[align=centre] #{@cw_title} " -x M -y S "← Mover a la izquierda" "" { run-shell "%[1]s _tabmove %[2]s '#{client_session}' -1" } "→ Mover a la derecha" "" { run-shell "%[1]s _tabmove %[2]s '#{client_session}' +1" } "" "▸ Renombrar pestaña" "" { %[3]s } "✦ Alias de claude de la pestaña" "" { %[5]s } "✕ Cerrar pestaña" "" { %[4]s }`,
		bq, ws, renameTab, confirmCloseTab, bulkTab)
	w(`bind -n MouseDown3Status { if -F "#{==:#{mouse_status_range},session}" { switch-client -t = } { if -F "#{==:#{mouse_status_range},window}" { run-shell -t = "%s _slotmenu %s '#{client_name}' '#{window_id}' '#{mouse_x}' '#{mouse_y}'" } } }`, bq, ws)
	// The session menu is prepared on press and opened on release: a menu opened after the
	// press's run-shell closes as soon as the button comes up.
	w(`bind -n MouseUp3Status { if -F "#{==:#{mouse_status_range},session}" { %s } { if -F "#{==:#{mouse_status_range},window}" { source-file -q "%s" } } }`, tabMenu, slotMenuFile(ws))
	// Menus open when the button comes up: one opened on press closes as soon as it is released
	// away from an item, and keeping it open (-O) stops its items from taking clicks.
	w(`bind -n MouseUp1Status { if -F "#{==:#{mouse_status_range},menu}" { %s } }`, mainMenu)
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
		{"newtab", newTabPopup}, {"newclaude", newClaudePopup}, {"newshell", newShell},
		{"help", helpPopup}, {"stop", confirmStop},
	}
	// A click on a session pill selects that window; a click on a tab switches to that tab.
	click := `if -F "#{==:#{mouse_status_range},window}" { select-window -t = } { switch-client -t = }`
	click = fmt.Sprintf(`if -F "#{m:x[tw]:*,#{mouse_status_range}}" { run-shell "%s _barclick %s '#{client_name}' '#{mouse_status_range}'" } { %s }`, bq, ws, click)
	for i := len(clicks) - 1; i >= 0; i-- {
		click = fmt.Sprintf(`if -F "#{==:#{mouse_status_range},%s}" { %s } { %s }`, clicks[i].rng, clicks[i].cmd, click)
	}
	// Drag and drop: pressing a session pill remembers it and the view that was open; dragging it
	// down onto the terminal goes back to that view, and releasing over a session shows both
	// together (split next to the pane under the mouse).
	w(`bind -n MouseDown1Status { set -gF @cw_dragx "#{mouse_x}" ; set -gF @cw_dragwin "#{?#{==:#{mouse_status_range},window},#{window_id},}" ; run-shell -C "set -g @cw_prevwin #{session_name}:#{active_window_index}" ; set -g @cw_dragging 0 ; %s }`, click)
	w(`bind -n MouseDrag1Pane { if -F "#{@cw_dragwin}" { if -F "#{!=:#{@cw_dragging},1}" { set -g @cw_dragging 1 ; run-shell -C "select-window -t #{@cw_prevwin}" ; display-message -d 4000 " Suelta sobre una sesión para verlas juntas" } } { if -F "#{||:#{pane_in_mode},#{mouse_any_flag}}" { send-keys -M } { copy-mode -M } } }`)
	w(`bind -n MouseDragEnd1Pane { if -F "#{&&:#{@cw_dragwin},#{==:#{@cw_dragging},1}}" { run-shell -t = "%s _join %s '#{@cw_dragwin}' '#{pane_id}'" } { if -F "#{mouse_any_flag}" { send-keys -M } } ; set -g @cw_dragwin "" ; set -g @cw_dragging 0 }`, bq, ws)
	w(`bind -n MouseDragEnd1Status { set -g @cw_dragwin "" ; set -g @cw_dragging 0 }`)
	// Right click inside a session (unless the program there uses the mouse): its own menu.
	paneMenu := fmt.Sprintf(`display-menu -T "#[align=centre] #{@cw_alias} " -x M -y M "#{?#{e|>:#{window_panes},1},,-}⇱ Separar esta sesión" "" { %s } "#{?#{e|>:#{window_panes},1},,-}▥ Lado a lado" "" { %s } "#{?#{e|>:#{window_panes},1},,-}▤ Arriba y abajo" "" { %s } "#{?#{e|>:#{window_panes},1},,-}▦ Cuadrícula" "" { %s } "" "⧉ Combinar sesiones aquí…" "" { %s } "▸ Renombrar" "" { %s } "✕ Cerrar sesión" "" { %s }`,
		splitOut, layoutCmd("lado"), layoutCmd("apilado"), layoutCmd("cuadricula"), combinePopup, renameSlot, confirmClose)
	w(`bind -n MouseDown3Pane { select-pane -t = ; if -F "#{||:#{mouse_any_flag},#{pane_in_mode}}" { send-keys -M } }`)
	w(`bind -n MouseUp3Pane { if -F "#{||:#{mouse_any_flag},#{pane_in_mode}}" { send-keys -M } { %s } }`, paneMenu)
	return b.String()
}
