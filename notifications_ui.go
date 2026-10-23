package main

import (
	"fmt"
	"os"
	"strings"
)

func openTerminalNotification(ws string, n pendingNotification) error {
	target := Tmux{WS: n.Workspace}
	pane, err := notificationPane(target, n.Slot)
	if err != nil {
		return err
	}
	mode, _ := target.Run("show", "-gqv", "@cw_app")
	if mode == "1" {
		_, err := appDo(map[string]any{"action": "notification.open", "notification": n.ID})
		return err
	}
	session, err := target.Run("display", "-p", "-t", pane, "#{session_name}")
	if err != nil {
		return err
	}
	current := Tmux{WS: ws}
	client, _, err := currentClient(current)
	if err != nil {
		return err
	}
	if _, err = target.Run("select-window", "-t", pane, ";", "select-pane", "-t", pane, ";", "set", "-g", "@cw_active", session); err != nil {
		return err
	}
	if ws != n.Workspace {
		bin, err := os.Executable()
		if err != nil {
			return err
		}
		// A tmux client cannot switch sockets. Reuse its terminal after detaching.
		command := joinArgs([]string{"env", "-u", "CW_WS", "-u", "TMUX", "-u", "TMUX_PANE", bin, "start", n.Workspace, "--here"})
		_, err = current.Run("detach-client", "-t", client, "-E", command)
		return err
	}
	if _, err = current.Run("switch-client", "-c", client, "-t", session); err != nil {
		return err
	}
	return acknowledgeNotifications("", n.Workspace, n.Slot, false)
}

func uiPendingNotifications(ws string) error {
	selected := 0
	message := ""
	setRaw(true)
	defer func() { setRaw(false); stdoutWrite("\033[?25h") }()
	for {
		list, err := readNotifications()
		if err != nil {
			return err
		}
		selected = max(0, min(selected, len(list)-1))
		var s screen
		_, height := termSize()
		visible := max(1, min(len(list), height-10))
		top := max(0, min(selected-visible+1, len(list)-visible))
		s.at(1, 4, sgrBold+fmt.Sprintf("Avisos pendientes · %d", len(list))+sgrReset)
		s.at(2, 4, sgrMuted+"Enter: ir a sesión · L: marcar leído · A: marcar todos · Esc: cerrar"+sgrReset)
		if len(list) == 0 {
			s.at(4, 4, "No tienes avisos pendientes.")
		}
		for i := top; i < top+visible && i < len(list); i++ {
			n := list[i]
			style := sgrField
			if i == selected {
				style = sgrSel
			}
			line := " " + fit(n.Workspace, 17) + " " + fit(n.Name, 23) + " " + fit(notificationTitle(n), 24) + " "
			s.btn(4+i-top, 3, style+line+sgrReset, "item:"+itoa(i))
		}
		row := 5 + visible
		s.at(row, 4, sgrMuted+fit(message, 70)+sgrReset)
		s.place(row+2, 3, []lineItem{buttonLine(0, btnDef{label: "Cerrar", id: "close"}, btnDef{label: "Marcar leído", id: "read"}, btnDef{label: "Todos leídos", id: "all"}, btnDef{label: "Ir a sesión", id: "open", primary: true})})
		s.closeX("close")
		s.flush(true)
		keys, err := readKeys()
		if err != nil {
			return nil
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(s.hits, k.x, k.y)
			}
			switch {
			case id == "close" || k.kind == keyEsc:
				return nil
			case strings.HasPrefix(id, "item:"):
				selected = atoiDefault(strings.TrimPrefix(id, "item:"), selected)
			case (k.kind == keyDown || k.kind == keyWheelDown) && len(list) > 0:
				selected = (selected + 1) % len(list)
			case (k.kind == keyUp || k.kind == keyWheelUp) && len(list) > 0:
				selected = (selected - 1 + len(list)) % len(list)
			case id == "all" || (k.kind == keyRune && (k.r == 'a' || k.r == 'A')):
				if err := acknowledgeNotifications("", "", "", true); err != nil {
					message = err.Error()
				}
			case (id == "read" || (k.kind == keyRune && (k.r == 'l' || k.r == 'L'))) && len(list) > 0:
				if err := acknowledgeNotifications(list[selected].ID, "", "", false); err != nil {
					message = err.Error()
				}
			case (id == "open" || k.kind == keyEnter) && len(list) > 0:
				setRaw(false)
				err := openTerminalNotification(ws, list[selected])
				if err == nil {
					return nil
				}
				setRaw(true)
				message = err.Error()
			}
		}
	}
}
