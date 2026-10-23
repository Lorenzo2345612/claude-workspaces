package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNotificationDefaultsMigrateExistingConfig(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	if err := writeFileAtomic(configFile(), []byte(`{"continue_message":"sigue","lazy_start":true}`)); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuestionSound != "10" || cfg.CompletionSound != "11" || cfg.SoundVolume != 40 || cfg.ContinueMessage != "sigue" || !cfg.LazyStart {
		t.Fatal(cfg)
	}
	cfg.QuestionSound = "16"
	cfg.CompletionSound = "none"
	cfg.SoundVolume = 0
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.QuestionSound != "16" || got.CompletionSound != "none" || got.SoundVolume != 0 {
		t.Fatal(got)
	}
}

func TestAllApprovedSoundsGeneratePlayableWAV(t *testing.T) {
	if len(notificationTones) != 16 {
		t.Fatal(len(notificationTones))
	}
	seen := map[string]bool{}
	for _, tone := range notificationTones {
		if seen[tone.ID] || tone.Name == "" {
			t.Fatal(tone)
		}
		seen[tone.ID] = true
		data, err := soundWAV(tone.ID, 40)
		if err != nil {
			t.Fatal(err)
		}
		if string(data[:4]) != "RIFF" || string(data[8:16]) != "WAVEfmt " || string(data[36:40]) != "data" {
			t.Fatal("invalid WAV", tone.ID)
		}
		if binary.LittleEndian.Uint32(data[4:]) != uint32(len(data)-8) || binary.LittleEndian.Uint32(data[24:]) != 44100 || binary.LittleEndian.Uint16(data[22:]) != 1 || binary.LittleEndian.Uint16(data[34:]) != 16 {
			t.Fatal("invalid WAV format", tone.ID)
		}
		if len(data) != 44+int(math.Ceil(tone.Duration*44100))*2 {
			t.Fatal("wrong duration", tone.ID)
		}
		audible := false
		for i := 44; i < len(data); i += 2 {
			if v := int16(binary.LittleEndian.Uint16(data[i:])); v != 0 {
				audible = true
				if math.Abs(float64(v)) > 32768*.4 {
					t.Fatal("volume exceeded", tone.ID)
				}
			}
		}
		if !audible {
			t.Fatal("silent sound", tone.ID)
		}
		muted, err := soundWAV(tone.ID, 0)
		if err != nil || !bytes.Equal(muted[44:], make([]byte, len(muted)-44)) {
			t.Fatal("mute broken", tone.ID, err)
		}
	}
	for _, id := range []string{"none", "missing", "../escape"} {
		if _, err := soundWAV(id, 40); err == nil {
			t.Fatal("unknown sound accepted", id)
		}
	}
	if _, err := soundWAV("10", 101); err == nil {
		t.Fatal("invalid volume accepted")
	}
}

func TestSoundTransitionsDoNotDuplicateOrAnnounceStartup(t *testing.T) {
	cfg := defaultConfig()
	for _, c := range []struct{ old, event, next, want string }{
		{"", "SessionStart", statusIdle, ""}, {statusBusy, "SessionStart", statusIdle, ""},
		{statusBusy, "PreToolUse", statusWaiting, "10"}, {statusIdle, "PermissionRequest", statusWaiting, "10"},
		{statusWaiting, "Notification", statusWaiting, ""}, {statusWaiting, "PermissionRequest", statusWaiting, ""},
		{statusBusy, "Stop", statusIdle, "11"}, {statusIdle, "Stop", statusIdle, ""},
		{statusWaiting, "Stop", statusIdle, ""}, {statusBusy, "StopFailure", statusIdle, ""},
		{statusWaiting, "PostToolUse", statusBusy, ""},
	} {
		if got := soundForHook(cfg, c.old, c.event, c.next); got != c.want {
			t.Fatalf("%+v got %q", c, got)
		}
	}
	cfg.QuestionSound = "none"
	if soundForHook(cfg, statusBusy, "PreToolUse", statusWaiting) != "" {
		t.Fatal("muted question sounded")
	}
	cfg.SoundVolume = 0
	if soundForHook(cfg, statusBusy, "Stop", statusIdle) != "" {
		t.Fatal("muted volume sounded")
	}
}

func TestHookNotificationsOnceForTerminalAndApp(t *testing.T) {
	oldIndicators := updateNotificationIndicators
	updateNotificationIndicators = func() error { return nil }
	defer func() { updateNotificationIndicators = oldIndicators }()
	oldBadge := requestNativeBadge
	requestNativeBadge = func() error { return nil }
	defer func() { requestNativeBadge = oldBadge }()
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux unavailable")
	}
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	t.Setenv("CW_DATA_DIR", t.TempDir())
	t.Setenv("SHELL", "")
	cfg := defaultConfig()
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	originalPlayer := playNotification
	defer func() { playNotification = originalPlayer }()
	var played []string
	playNotification = func(id string, volume int) error {
		if volume != 40 {
			t.Fatal(volume)
		}
		played = append(played, id)
		return nil
	}
	oldStdin := os.Stdin
	defer func() { os.Stdin = oldStdin }()
	for _, mode := range []string{"terminal", "app"} {
		t.Run(mode, func(t *testing.T) {
			mux := Tmux{WS: "sounds-" + mode + "-" + shortID()}
			t.Cleanup(func() { _, _ = mux.Run("kill-server") })
			slot := Slot{ID: newUUID(), Kind: kindClaude, Alias: "test", Cwd: t.TempDir(), ClaudeAlias: "codex"}
			_, err := mux.createWindow(windowSpec{session: "test", first: true, slot: slot, name: "test", display: statusIdle, argv: []string{"sleep", "300"}})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "app" {
				if _, err := mux.Run("set", "-g", "@cw_app", "1"); err != nil {
					t.Fatal(err)
				}
			}
			t.Setenv("CW_WS", mux.WS)
			t.Setenv("CW_SLOT", slot.ID)
			t.Setenv("TMUX_PANE", "")
			played = nil
			send := func(event string) {
				t.Helper()
				f, err := os.CreateTemp(t.TempDir(), "hook")
				if err != nil {
					t.Fatal(err)
				}
				defer f.Close()
				if err := json.NewEncoder(f).Encode(hookInput{SessionID: "id", HookEventName: event}); err != nil {
					t.Fatal(err)
				}
				f.Seek(0, 0)
				os.Stdin = f
				if err := cmdHook(); err != nil {
					t.Fatal(err)
				}
			}
			for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "Notification", "PostToolUse", "Stop", "Stop", "UserPromptSubmit", "StopFailure"} {
				send(event)
			}
			if !reflect.DeepEqual(played, []string{"10", "11"}) {
				t.Fatal("duplicate or incorrect notices", played)
			}
		})
	}
}

func TestWindowsSoundEncodingAndStaleInterop(t *testing.T) {
	if powershellEncodedCommand("A😀") != "QQA92ADe" {
		t.Fatal("UTF-16 encoding broken", powershellEncodedCommand("A😀"))
	}
	paths, _ := filepath.Glob("/run/WSL/*_interop")
	if len(paths) == 0 {
		return
	}
	t.Setenv("WSL_INTEROP", filepath.Join(t.TempDir(), "missing"))
	for _, kv := range windowsSoundEnv() {
		if kv == "WSL_INTEROP="+os.Getenv("WSL_INTEROP") {
			t.Fatal("stale interop reused")
		}
	}
}
