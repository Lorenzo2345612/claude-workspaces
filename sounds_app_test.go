//go:build !terminal

package main

import (
	"encoding/json"
	"testing"
)

func TestSoundSettingsPatchValidatesAndPreservesMutes(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	var req doReq
	if err := json.Unmarshal([]byte(`{"config":{"question_sound":"16","completion_sound":"none","sound_volume":0}}`), &req); err != nil {
		t.Fatal(err)
	}
	if err := applyConfigPatch(&req); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.QuestionSound != "16" || cfg.CompletionSound != "none" || cfg.SoundVolume != 0 {
		t.Fatal(cfg)
	}
	for _, patch := range []string{`{"config":{"question_sound":"17"}}`, `{"config":{"completion_sound":"../x"}}`, `{"config":{"sound_volume":101}}`, `{"config":{"sound_volume":-1}}`} {
		var bad doReq
		if err := json.Unmarshal([]byte(patch), &bad); err != nil {
			t.Fatal(err)
		}
		if err := applyConfigPatch(&bad); err == nil {
			t.Fatal("invalid setting accepted", patch)
		}
	}
	cfg, _ = loadConfig()
	if cfg.QuestionSound != "16" || cfg.SoundVolume != 0 {
		t.Fatal("failed patch changed config", cfg)
	}
}

func TestAppSoundPreviewUsesHostPlayer(t *testing.T) {
	t.Setenv("CW_CONFIG_DIR", t.TempDir())
	original := playNotification
	defer func() { playNotification = original }()
	calls := 0
	playNotification = func(id string, volume int) error {
		calls++
		if id != "10" || volume != 25 {
			t.Fatal(id, volume)
		}
		return nil
	}
	var req doReq
	if err := json.Unmarshal([]byte(`{"action":"sound.preview","sound":"10","volume":25}`), &req); err != nil {
		t.Fatal(err)
	}
	if _, err := (&appServer{}).do(&req); err != nil {
		t.Fatal(err)
	}
	req.Sound = "bad"
	if _, err := (&appServer{}).do(&req); err == nil {
		t.Fatal("invalid preview accepted")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
