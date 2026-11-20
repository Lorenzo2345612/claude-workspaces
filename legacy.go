package main

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Skynet, the orchestrator service of 0.22 and earlier, was removed: any session can coordinate
// others now. A service started by an older cw keeps running after an update, so the periodic
// jobs stop it once and drop its files.
func stopLegacySkynet() {
	sock := filepath.Join(runDir(), "skynet.sock")
	if _, err := os.Stat(sock); err != nil {
		return
	}
	client := &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}}
	if resp, err := client.Post("http://skynet/stop", "application/json", nil); err == nil {
		resp.Body.Close()
	}
	for _, f := range []string{"skynet.sock", "skynet.json", "skynet-log.jsonl", "skynet.log", "skynet-claude.log"} {
		_ = os.Remove(filepath.Join(runDir(), f))
	}
}
