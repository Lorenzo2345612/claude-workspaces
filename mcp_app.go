//go:build !terminal

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// appDo runs an action through the app's server, starting it when it is not running: the server
// holds each workspace's lock and redraws the window, so changes never race with the window.
func appDo(q map[string]any) (map[string]any, error) {
	info := runningApp()
	if info == nil {
		var err error
		if info, err = startAppServer(homeDir()); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(q)
	req, _ := http.NewRequest("POST", info.base()+"/api/do", bytes.NewReader(body))
	req.AddCookie(&http.Cookie{Name: "cw_k", Value: info.Token})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CW", "1")
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out map[string]any
	_ = json.Unmarshal(data, &out)
	if resp.StatusCode != http.StatusOK {
		if msg, ok := out["error"].(string); ok {
			return nil, errors.New(msg)
		}
		return nil, errors.New("la app respondió " + resp.Status)
	}
	if out == nil {
		out = map[string]any{"ok": true}
	}
	return out, nil
}
