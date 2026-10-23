package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func isolateNotifications(t *testing.T) {
	t.Helper()
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	sockets, err := os.MkdirTemp("", "cw-notify-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	t.Setenv("TMUX_TMPDIR", sockets)
	old := requestNativeBadge
	requestNativeBadge = func() error { return nil }
	t.Cleanup(func() { requestNativeBadge = old })
	cfg := defaultConfig()
	cfg.SoundVolume = 0
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func notificationFixture(t *testing.T, ws, id string) (Tmux, string) {
	t.Helper()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	mux := Tmux{WS: ws}
	t.Cleanup(func() { _, _ = mux.Run("kill-server") })
	s := Slot{ID: id, Kind: kindClaude, ClaudeAlias: "codex", Alias: "Chat", Cwd: t.TempDir()}
	win, err := mux.createWindow(windowSpec{session: "test", first: true, name: s.Alias, slot: s, display: statusIdle, argv: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	pane, err := mux.Run("display", "-p", "-t", win, "#{pane_id}")
	if err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(tmuxConfFile(ws), []byte(tmuxConf(ws, "/bin/true", true, true))); err != nil {
		t.Fatal(err)
	}
	if _, err := mux.Run("source-file", tmuxConfFile(ws)); err != nil {
		t.Fatal("invalid terminal notification configuration", err)
	}
	return mux, pane
}

func unreadList(t *testing.T) []pendingNotification {
	t.Helper()
	list, err := readNotifications()
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestPersistentNotificationsIndependentOfSounds(t *testing.T) {
	isolateNotifications(t)
	mux, pane := notificationFixture(t, "notices", newUUID())
	send := func(old, event, next string) {
		t.Helper()
		if err := recordHookNotification(mux, pane, old, event, next); err != nil {
			t.Fatal(err)
		}
	}
	send(statusIdle, "SessionStart", statusIdle)
	if len(unreadList(t)) != 0 {
		t.Fatal("startup generated notice")
	}
	send(statusBusy, "PermissionRequest", statusWaiting)
	list := unreadList(t)
	if len(list) != 1 || list[0].Kind != "question" {
		t.Fatal(list)
	}
	firstID := list[0].ID
	send(statusWaiting, "Notification", statusWaiting)
	if list = unreadList(t); len(list) != 1 || list[0].ID != firstID {
		t.Fatal("duplicate question", list)
	}
	// Seen does not resolve the actual question or synthesize another unread item.
	if err := acknowledgeNotifications(firstID, "", "", false); err != nil {
		t.Fatal(err)
	}
	send(statusWaiting, "Notification", statusWaiting)
	if len(unreadList(t)) != 0 {
		t.Fatal("read question reappeared")
	}
	send(statusBusy, "PermissionRequest", statusWaiting)
	send(statusWaiting, "PostToolUse", statusBusy)
	if len(unreadList(t)) != 0 {
		t.Fatal("answered question remained unread")
	}
	send(statusBusy, "Stop", statusIdle)
	list = unreadList(t)
	if len(list) != 1 || list[0].Kind != "completion" {
		t.Fatal(list)
	}
	send(statusIdle, "Stop", statusIdle)
	if len(unreadList(t)) != 1 {
		t.Fatal("duplicate completion")
	}
	// Reload from disk, without any tmux state dependency.
	data, err := os.ReadFile(notificationsFile())
	if err != nil {
		t.Fatal(err)
	}
	var persisted []pendingNotification
	if err = json.Unmarshal(data, &persisted); err != nil || !reflect.DeepEqual(persisted, list) {
		t.Fatal(err, persisted)
	}
	send(statusIdle, "SessionStart", statusIdle)
	if len(unreadList(t)) != 1 {
		t.Fatal("restart erased unseen completion")
	}
	send(statusIdle, "UserPromptSubmit", statusBusy)
	if len(unreadList(t)) != 0 {
		t.Fatal("new work did not consume completion")
	}
	send(statusBusy, "StopFailure", statusIdle)
	if len(unreadList(t)) != 0 {
		t.Fatal("failure generated completion")
	}
}

func TestUnreadCountersAndAcknowledgementAreScoped(t *testing.T) {
	isolateNotifications(t)
	a, pa := notificationFixture(t, "alpha", "same-slot")
	b, pb := notificationFixture(t, "beta", "same-slot")
	if err := recordHookNotification(a, pa, statusBusy, "Stop", statusIdle); err != nil {
		t.Fatal(err)
	}
	if err := recordHookNotification(b, pb, statusBusy, "PreToolUse", statusWaiting); err != nil {
		t.Fatal(err)
	}
	list := unreadList(t)
	if len(list) != 2 {
		t.Fatal(list)
	}
	for _, mux := range []Tmux{a, b} {
		v, _ := mux.Run("show", "-gqv", "@cw_unread_count")
		if v != "2" {
			t.Fatal("global inbox count wrong", v)
		}
	}
	v, _ := a.Run("display", "-p", "-t", pa, "#{@cw_unread}")
	if v != "1" {
		t.Fatal("pane unread wrong", v)
	}
	if err := acknowledgeNotifications("", "alpha", "same-slot", false); err != nil {
		t.Fatal(err)
	}
	list = unreadList(t)
	if len(list) != 1 || list[0].Workspace != "beta" {
		t.Fatal("read affected another workspace", list)
	}
	v, _ = a.Run("display", "-p", "-t", pa, "#{@cw_unread}")
	if v != "0" {
		t.Fatal("read badge did not clear", v)
	}
	if err := acknowledgeNotifications("", "", "", false); err == nil {
		t.Fatal("ambiguous read cleared all")
	}
	if err := acknowledgeNotifications("", "", "", true); err != nil {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 0 {
		t.Fatal("mark all failed")
	}
}

func TestStaleReadCannotConsumeANewerNotice(t *testing.T) {
	isolateNotifications(t)
	mux, pane := notificationFixture(t, "read-race", newUUID())
	_ = recordHookNotification(mux, pane, statusBusy, "Stop", statusIdle)
	old := unreadList(t)[0].ID
	_ = recordHookNotification(mux, pane, statusBusy, "Stop", statusIdle)
	list := unreadList(t)
	if list[0].ID == old {
		t.Fatal("new event did not replace old ID")
	}
	if err := acknowledgeNotifications(old, "", "", false); err != nil {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 1 {
		t.Fatal("stale read erased new notification")
	}
}

func TestNotificationStoreSerializesConcurrentWriters(t *testing.T) {
	isolateNotifications(t)
	var wg sync.WaitGroup
	errs := make(chan error, 25)
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := changeNotifications(func(list []pendingNotification) []pendingNotification {
				return append(list, pendingNotification{ID: newUUID(), Workspace: "test", Slot: newUUID(), At: 1})
			})
			if err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 25 {
		t.Fatal("concurrent writes lost notifications")
	}
}

func TestTerminalNotificationEntryPoints(t *testing.T) {
	conf := tmuxConf("test", "/cw", true, true)
	for _, want := range []string{"_ui notifications", "@cw_unread_count", "_notification-read", "set-titles-string", "Agent Workspaces terminal", "bind -n M-b"} {
		if !strings.Contains(conf, want) {
			t.Fatal("missing terminal inbox integration", want)
		}
	}
	if strings.Contains(conf, "%!") {
		t.Fatal("broken tmux format")
	}
}

func TestNativeBadgeScriptCompilesOnWindowsWhenRequested(t *testing.T) {
	if os.Getenv("CW_TEST_NATIVE_BADGE") != "1" {
		t.Skip("opt-in Windows integration")
	}
	script := strings.ReplaceAll(windowsBadgeScript(0, false), "Agent Workspaces", "__CW_BADGE_TEST_NO_WINDOWS__")
	script = strings.ReplaceAll(script, `title.ToString()=="cw"`, `false`)
	script = strings.ReplaceAll(script, `title.ToString().StartsWith("cw · ")`, `false`)
	cmd := exec.Command("/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe", "-NoProfile", "-NonInteractive", "-EncodedCommand", powershellEncodedCommand(script))
	cmd.Env = windowsSoundEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("native badge failed: %v: %s", err, output)
	}
}

func TestNotificationPathsStayInsideDataDir(t *testing.T) {
	isolateNotifications(t)
	if filepath.Dir(notificationsFile()) != dataDir() {
		t.Fatal(notificationsFile())
	}
}

func TestClosingTerminalTabDropsItsUnreadNotices(t *testing.T) {
	isolateNotifications(t)
	mux, pane := notificationFixture(t, "close-notice", newUUID())
	_, err := mux.createWindow(windowSpec{session: "other", first: true, slot: Slot{ID: newUUID(), Kind: kindShell, Cwd: t.TempDir()}, name: "other", argv: []string{"sleep", "300"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := recordHookNotification(mux, pane, statusBusy, "Stop", statusIdle); err != nil {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 1 {
		t.Fatal("missing unread")
	}
	if err := closeTab(mux.WS, "test", ""); err != nil {
		t.Fatal(err)
	}
	if len(unreadList(t)) != 0 {
		t.Fatal("closed tab left unread notices")
	}
}
