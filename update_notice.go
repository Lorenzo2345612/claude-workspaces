package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Telling about new versions when cw or the app starts. GitHub is asked at most every
// updateCheckEvery, with a short timeout, and a version the user declined is not offered again.

const updateCheckEvery = 6 * time.Hour

type updateState struct {
	CheckedAt time.Time `json:"checked_at"`
	Latest    string    `json:"latest"`
	Dismissed string    `json:"dismissed,omitempty"`
}

func updateStateFile() string { return filepath.Join(runDir(), "update.json") }

func loadUpdateState() updateState {
	var st updateState
	if data, err := os.ReadFile(updateStateFile()); err == nil {
		_ = json.Unmarshal(data, &st)
	}
	return st
}

func saveUpdateState(st updateState) {
	if data, err := json.Marshal(st); err == nil {
		_ = writeFileAtomic(updateStateFile(), data)
	}
}

// availableUpdate returns the newer published version to offer, or "" (none, declined, a
// development build, or GitHub unreachable).
func availableUpdate() string {
	if version == "dev" {
		return ""
	}
	st := loadUpdateState()
	if time.Since(st.CheckedAt) > updateCheckEvery {
		if latest, err := quickLatestVersion(); err == nil {
			st.Latest, st.CheckedAt = latest, time.Now()
			saveUpdateState(st)
		}
	}
	if st.Latest == "" || st.Latest == st.Dismissed || !newerVersion(st.Latest, version) {
		return ""
	}
	return st.Latest
}

// dismissUpdate stops offering one version.
func dismissUpdate(v string) {
	st := loadUpdateState()
	st.Dismissed = v
	saveUpdateState(st)
}

// rememberInstalled records that the latest version is in place (nothing left to offer).
func rememberInstalled(v string) {
	st := loadUpdateState()
	st.Latest, st.CheckedAt = v, time.Now()
	saveUpdateState(st)
}

func quickLatestVersion() (string, error) {
	req, err := http.NewRequest("GET", releasesAPI, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub respondió %s", resp.Status)
	}
	var rel githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	return strings.TrimPrefix(rel.TagName, "v"), nil
}

// updateNow installs the latest release, printing its progress.
func updateNow() (string, error) {
	rel, err := latestRelease()
	if err != nil {
		return "", fmt.Errorf("no pude consultar la última versión: %w", err)
	}
	if _, err := installRelease(rel, func(msg string) { fmt.Println(msg) }); err != nil {
		return "", err
	}
	v := strings.TrimPrefix(rel.TagName, "v")
	rememberInstalled(v)
	return v, nil
}

// noticeAfterAttach starts, in the background, the popup that offers a new version to the
// terminal that is about to attach to a workspace.
func noticeAfterAttach(ws string) {
	if version == "dev" || os.Getenv("CW_NO_UPDATE_NOTICE") != "" {
		return
	}
	bin, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(bin, "_updnotice", ws)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if cmd.Start() == nil {
		_ = cmd.Process.Release()
	}
}

// cmdUpdNotice waits for a client of the workspace and offers it the new version: _updnotice <ws>.
func cmdUpdNotice(args []string) error {
	if len(args) != 1 {
		return nil
	}
	ws := args[0]
	v := availableUpdate()
	if v == "" {
		return nil
	}
	t := Tmux{WS: ws}
	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		clients, _ := t.Lines("list-clients", "-F", "#{client_name}")
		if len(clients) > 0 {
			time.Sleep(1500 * time.Millisecond) // let the workspace draw first
			bin, err := os.Executable()
			if err != nil {
				return nil
			}
			_, _ = t.Run(popupCmd(clients[0][0], "Nueva versión de cw", 66, 13, bin, "_ui", "update", ws)...)
			return nil
		}
	}
	return nil
}

// uiUpdate is the popup that offers the new version: _ui update <ws>.
func uiUpdate(ws string) error {
	v := availableUpdate()
	if v == "" {
		return nil
	}
	focus := 1 // Actualizar
	var hits []hit
	draw := func() {
		var s screen
		s.at(2, 4, sgrBold+"Hay una versión nueva de cw: "+v+sgrReset)
		s.at(3, 4, sgrMuted+"Tienes la "+version+"."+sgrReset)
		s.at(5, 4, sgrMuted+"Se instala aquí mismo y tus sesiones siguen corriendo."+sgrReset)
		s.at(6, 4, sgrMuted+"Para usarla en este workspace, desconéctate y vuelve"+sgrReset)
		s.at(7, 4, sgrMuted+"a entrar (Alt+m → Desconectar)."+sgrReset)
		s.btn(9, 4, button("Ahora no", false, focus == 0), "no")
		s.btn(9, 18, button("Actualizar", true, focus == 1), "yes")
		s.closeX("no")
		s.b.WriteString("\033[?25l")
		hits = s.hits
		s.flush(true)
	}
	setRaw(true)
	draw()
	for {
		keys, err := readKeys()
		if err != nil {
			setRaw(false)
			return nil
		}
		for _, k := range keys {
			id := ""
			if k.kind == keyClick {
				id = hitAt(hits, k.x, k.y)
			}
			switch {
			case id == "no" || k.kind == keyEsc || (k.kind == keyRune && k.r == 'n') || (k.kind == keyEnter && focus == 0):
				setRaw(false)
				dismissUpdate(v)
				return nil
			case id == "yes" || (k.kind == keyRune && (k.r == 's' || k.r == 'u')) || (k.kind == keyEnter && focus == 1):
				setRaw(false)
				os.Stdout.WriteString("\033[H\033[2J\033[?25h\n")
				got, err := updateNow()
				if err != nil {
					pause("No se pudo actualizar: " + err.Error())
					return nil
				}
				pause("\ncw " + got + " instalado. Desconéctate (Alt+m → Desconectar) y vuelve a entrar para usarlo aquí.")
				return nil
			case k.kind == keyLeft || k.kind == keyRight || k.kind == keyTab:
				focus = 1 - focus
			}
		}
		draw()
	}
}
