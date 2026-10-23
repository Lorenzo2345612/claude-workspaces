//go:build !terminal

package main

import "testing"

func TestAppNotificationReadAndOpenPreserveView(t *testing.T) {
	isolateNotifications(t)
	mux, pane := notificationFixture(t, "app-inbox", newUUID())
	slot, err := paneSlot(mux, pane)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = mux.Run("set", "-g", "@cw_app", "1")
	// App sessions have one tmux session per slot; rename fixture to the real format.
	_, err = mux.Run("rename-session", "-t", "test", slotSession(slot.ID))
	if err != nil {
		t.Fatal(err)
	}
	layout := &appLayout{Active: "tab", Tabs: []*appTab{{ID: "tab", Title: "Test", Views: []*appView{{ID: "view", Slots: []string{slot.ID}, Focus: slot.ID}}, Active: "view"}}}
	if err = mux.storeLayout(layout); err != nil {
		t.Fatal(err)
	}
	_ = recordHookNotification(mux, pane, statusBusy, "Stop", statusIdle)
	list := unreadList(t)
	if len(list) != 1 {
		t.Fatal(list)
	}
	snap := appSnapshot(mux.WS)
	if snap.Slots[slot.ID].Unread != 1 {
		t.Fatal("app session badge absent", snap.Slots)
	}
	out, err := (&appServer{}).do(&doReq{Action: "notification.open", Notification: list[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if out["ws"] != mux.WS || out["slot"] != slot.ID {
		t.Fatal(out)
	}
	if len(unreadList(t)) != 0 {
		t.Fatal("opening did not mark as read")
	}
	after := mux.loadLayout()
	if len(after.Tabs) != 1 || len(after.Tabs[0].Views) != 1 || after.Tabs[0].Views[0].ID != "view" {
		t.Fatal("opening created a view", after)
	}
	_ = recordHookNotification(mux, pane, statusBusy, "Stop", statusIdle)
	n := unreadList(t)[0]
	if _, err = (&appServer{}).do(&doReq{Action: "notification.read", Notification: n.ID}); err != nil {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 0 {
		t.Fatal("explicit read failed")
	}
}

func TestOpeningStoppedWorkspaceKeepsNotice(t *testing.T) {
	isolateNotifications(t)
	_, err := changeNotifications(func(list []pendingNotification) []pendingNotification {
		return []pendingNotification{{ID: "notice", Workspace: "stopped", Slot: "slot", Kind: "completion"}}
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := (&appServer{}).do(&doReq{Action: "notification.open", Notification: "notice"})
	if err != nil {
		t.Fatal(err)
	}
	if out["stopped"] != true || len(unreadList(t)) != 1 {
		t.Fatal("unavailable session notice was consumed", out)
	}
}
