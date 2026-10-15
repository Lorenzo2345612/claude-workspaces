//go:build !terminal

package main

import (
	"io"
	"net/http"
	"strings"
)

// handleSkynet passes /api/skynet/<path> to Skynet's service (starting it when it is on), so the
// window talks to it with its own cookie: status, events (a stream), send, reset.
func (s *appServer) handleSkynet(w http.ResponseWriter, r *http.Request) {
	path := "/" + strings.TrimPrefix(r.URL.Path, "/api/skynet/")
	switch path {
	case "/status", "/events", "/send", "/reset":
	default:
		http.NotFound(w, r)
		return
	}
	cfg, _ := loadConfig()
	if !cfg.SkynetEnabled {
		http.Error(w, "Skynet está apagado", http.StatusConflict)
		return
	}
	if err := ensureSkynet(); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), r.Method, "http://skynet"+path, r.Body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := skynetHTTP(0).Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for _, k := range []string{"Content-Type"} {
		w.Header().Set(k, resp.Header.Get(k))
	}
	w.WriteHeader(resp.StatusCode)
	fl, _ := w.(http.Flusher)
	buf := make([]byte, 32<<10)
	for {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if fl != nil {
				fl.Flush()
			}
		}
		if err != nil {
			if err != io.EOF {
				return
			}
			return
		}
	}
}
