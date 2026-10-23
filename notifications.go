package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

type pendingNotification struct {
	ID        string `json:"id"`
	Workspace string `json:"workspace"`
	Slot      string `json:"slot"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	At        int64  `json:"at"`
}

func notificationsFile() string { return filepath.Join(dataDir(), "notifications.json") }

func readNotifications() ([]pendingNotification, error) {
	data, err := os.ReadFile(notificationsFile())
	if os.IsNotExist(err) {
		return []pendingNotification{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []pendingNotification
	if err = json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []pendingNotification{}
	}
	return list, nil
}

// Updates serialize across hooks, app requests and terminal popups. Only unread items persist.
func changeNotifications(change func([]pendingNotification) []pendingNotification) (bool, error) {
	lock, err := usageLock("notifications.lock", false)
	if err != nil {
		return false, err
	}
	defer lock.Close()
	list, err := readNotifications()
	if err != nil {
		return false, err
	}
	before, _ := json.Marshal(list)
	list = change(list)
	if list == nil {
		list = []pendingNotification{}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].At > list[j].At })
	if len(list) > 500 {
		list = list[:500]
	}
	after, err := json.Marshal(list)
	if err != nil {
		return false, err
	}
	if string(before) == string(after) {
		return false, nil
	}
	return true, writeFileAtomic(notificationsFile(), append(after, '\n'))
}

func notificationEvent(previous, event, next string) string {
	if next == statusWaiting && previous != statusWaiting {
		return "question"
	}
	if event == "Stop" && previous == statusBusy && next == statusIdle {
		return "completion"
	}
	return ""
}

// Separate from sound settings: muting an alert must not hide the unread notification.
func recordHookNotification(t Tmux, pane, previous, event, next string) error {
	kind := notificationEvent(previous, event, next)
	clearQuestion := next != statusWaiting
	clearCompletion := event == "UserPromptSubmit"
	if kind == "" && !clearQuestion && !clearCompletion {
		return nil
	}
	slot, err := paneSlot(t, pane)
	if err != nil {
		return err
	}
	if slot.Kind != kindClaude {
		return nil
	}
	changed, err := changeNotifications(func(list []pendingNotification) []pendingNotification {
		var out []pendingNotification
		for _, n := range list {
			same := n.Workspace == t.WS && n.Slot == slot.ID
			if same && ((clearQuestion && n.Kind == "question") || (clearCompletion && n.Kind == "completion") || (kind != "" && n.Kind == kind)) {
				continue
			}
			out = append(out, n)
		}
		if kind != "" {
			out = append(out, pendingNotification{ID: newUUID(), Workspace: t.WS, Slot: slot.ID, Name: slot.Alias, Kind: kind, At: time.Now().UnixMilli()})
		}
		return out
	})
	if err != nil {
		return err
	}
	if changed {
		return updateNotificationIndicators()
	}
	return nil
}

func acknowledgeNotifications(id, ws, slot string, all bool) error {
	if !all && id == "" && (ws == "" || slot == "") {
		return errors.New("indica el aviso o la sesión")
	}
	changed, err := changeNotifications(func(list []pendingNotification) []pendingNotification {
		var out []pendingNotification
		for _, n := range list {
			if all || (id != "" && n.ID == id) || (id == "" && n.Workspace == ws && n.Slot == slot) {
				continue
			}
			out = append(out, n)
		}
		return out
	})
	if err != nil {
		return err
	}
	if changed {
		return updateNotificationIndicators()
	}
	return nil
}

var updateNotificationIndicators = refreshNotificationIndicators

func refreshNotificationIndicators() error {
	lock, err := usageLock("notification-indicators.lock", false)
	if err != nil {
		return err
	}
	defer lock.Close()
	list, err := readNotifications()
	if err != nil {
		return err
	}
	counts := map[string]int{}
	for _, n := range list {
		counts[n.Workspace+":"+n.Slot]++
	}
	for _, ws := range runningWorkspaces() {
		t := Tmux{WS: ws}
		// Global inbox is accessible from either interface, including other workspaces.
		args := []string{"set", "-g", "@cw_unread_count", strconv.Itoa(len(list))}
		panes, err := t.Lines("list-panes", "-a", "-F", "#{pane_id}"+sep+"#{@cw_slot}")
		if err != nil {
			continue
		}
		for _, p := range panes {
			if len(p) == 2 {
				args = append(args, ";", "set", "-p", "-t", p[0], "@cw_unread", strconv.Itoa(counts[ws+":"+p[1]]))
			}
		}
		args = append(args, ";", "refresh-client", "-S")
		_, _ = t.Run(args...)
	}
	return requestNativeBadge()
}

func cmdNotificationRead(args []string) error {
	if len(args) != 2 || validWorkspaceName(args[0]) != nil {
		return errors.New("uso: _notification-read <ws> <pane>")
	}
	t := Tmux{WS: args[0]}
	slot, err := paneSlot(t, args[1])
	if err != nil {
		return nil
	}
	return acknowledgeNotifications("", t.WS, slot.ID, false)
}

func notificationTitle(n pendingNotification) string {
	if n.Kind == "question" {
		return "Necesita tu respuesta"
	}
	return "Trabajo terminado"
}

func notificationPane(t Tmux, slot string) (string, error) {
	rows, err := t.Lines("list-panes", "-a", "-F", "#{pane_id}"+sep+"#{@cw_slot}")
	if err != nil {
		return "", err
	}
	for _, r := range rows {
		if len(r) == 2 && r[1] == slot {
			return r[0], nil
		}
	}
	return "", errors.New("la sesión ya no está abierta")
}

func dismissWorkspaceNotifications(ws string) error {
	changed, err := changeNotifications(func(list []pendingNotification) []pendingNotification {
		var out []pendingNotification
		for _, n := range list {
			if n.Workspace != ws {
				out = append(out, n)
			}
		}
		return out
	})
	if err != nil {
		return err
	}
	if changed {
		return updateNotificationIndicators()
	}
	return nil
}
