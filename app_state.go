package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

// In the app, a workspace still runs on its own tmux server, but every session is a tmux
// session of its own ("s-<slot id>", one window, one pane), so each panel of the window can
// attach a terminal to exactly one session. Tabs and views exist only in the app: their
// structure is kept as JSON in the server option @cw_layout, which survives the app itself.

const (
	appKeeper   = "_cw" // a placeholder session that keeps the server alive while every tab is empty
	slotPrefix  = "s-"
	layoutCols  = "cols"
	layoutRows  = "rows"
	layoutGrid  = "grid"
	optLayout   = "@cw_layout"
	optAppMode  = "@cw_app"
	optWSTitle  = "@cw_ws_title"
	optLaunchAt = "@cw_launch_dir"
)

type appLayout struct {
	Tabs   []*appTab `json:"tabs"`
	Active string    `json:"active"`
}

type appTab struct {
	ID     string     `json:"id"`
	Title  string     `json:"title"`
	Dir    string     `json:"dir"`
	Views  []*appView `json:"views"`
	Active string     `json:"active"` // view id
}

type appView struct {
	ID     string   `json:"id"`
	Layout string   `json:"layout"`
	Slots  []string `json:"slots"`
	Focus  string   `json:"focus"`
}

func slotSession(id string) string { return slotPrefix + id }

func shortID() string { return newUUID()[:8] }

// appMode reports whether a running workspace is run by the app.
func appMode(t Tmux) bool {
	v, _ := t.Run("show", "-gqv", optAppMode)
	return v == "1"
}

func (t Tmux) loadLayout() *appLayout {
	l := &appLayout{}
	if v, _ := t.Run("show", "-gqv", optLayout); v != "" {
		_ = json.Unmarshal([]byte(v), l)
	}
	return l
}

func (t Tmux) storeLayout(l *appLayout) error {
	data, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = t.Run("set", "-g", optLayout, string(data))
	return err
}

func (l *appLayout) tab(id string) *appTab {
	for _, t := range l.Tabs {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (l *appLayout) tabIndex(id string) int {
	for i, t := range l.Tabs {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func (l *appLayout) activeTab() *appTab {
	if t := l.tab(l.Active); t != nil {
		return t
	}
	if len(l.Tabs) > 0 {
		l.Active = l.Tabs[0].ID
		return l.Tabs[0]
	}
	return nil
}

// findSlot returns the tab and view that hold a slot.
func (l *appLayout) findSlot(id string) (*appTab, *appView) {
	for _, t := range l.Tabs {
		for _, v := range t.Views {
			for _, s := range v.Slots {
				if s == id {
					return t, v
				}
			}
		}
	}
	return nil, nil
}

func (l *appLayout) findView(id string) (*appTab, *appView) {
	for _, t := range l.Tabs {
		for _, v := range t.Views {
			if v.ID == id {
				return t, v
			}
		}
	}
	return nil, nil
}

func (t *appTab) view(id string) *appView {
	for _, v := range t.Views {
		if v.ID == id {
			return v
		}
	}
	return nil
}

func (t *appTab) viewIndex(id string) int {
	for i, v := range t.Views {
		if v.ID == id {
			return i
		}
	}
	return -1
}

func (t *appTab) activeView() *appView {
	if v := t.view(t.Active); v != nil {
		return v
	}
	if len(t.Views) > 0 {
		t.Active = t.Views[0].ID
		return t.Views[0]
	}
	return nil
}

// removeSlot takes a slot out of its view, dropping the view when it empties.
func (l *appLayout) removeSlot(id string) {
	for _, t := range l.Tabs {
		for vi := 0; vi < len(t.Views); vi++ {
			v := t.Views[vi]
			kept := v.Slots[:0]
			for _, s := range v.Slots {
				if s != id {
					kept = append(kept, s)
				}
			}
			v.Slots = kept
			if v.Focus == id && len(kept) > 0 {
				v.Focus = kept[0]
			}
			if len(v.Slots) == 0 {
				t.Views = append(t.Views[:vi], t.Views[vi+1:]...)
				if t.Active == v.ID {
					t.Active = ""
					if len(t.Views) > 0 {
						t.Active = t.Views[min(vi, len(t.Views)-1)].ID
					}
				}
				vi--
			}
		}
	}
}

// paneInfo is what tmux knows about the pane of one session.
type paneInfo struct {
	session, pane string
	slot          Slot
	dead          bool
	deadStatus    string
	path          string
}

// listSlotPanes reads every session pane of an app workspace, by slot id.
func (t Tmux) listSlotPanes() (map[string]paneInfo, error) {
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{session_name}", "#{pane_id}", "#{@cw_slot}", "#{@cw_kind}", "#{@cw_alias}", "#{@cw_cwd}", "#{@cw_sid}",
		"#{@cw_status}", "#{@cw_args}", "#{@cw_claude_alias}", "#{pane_dead}", "#{pane_dead_status}", "#{pane_current_path}",
	}, sep))
	if err != nil {
		return nil, err
	}
	out := map[string]paneInfo{}
	for _, r := range rows {
		if len(r) != 13 || !strings.HasPrefix(r[0], slotPrefix) {
			continue
		}
		id := strings.TrimPrefix(r[0], slotPrefix)
		if _, seen := out[id]; seen {
			continue
		}
		kind := r[3]
		if kind == "" {
			kind = kindShell
		}
		out[id] = paneInfo{session: r[0], pane: r[1], dead: r[10] == "1", deadStatus: r[11], path: r[12],
			slot: Slot{ID: id, Kind: kind, Alias: r[4], Cwd: r[5], SessionID: r[6], Status: r[7],
				Args: argsFromOption(r[8]), ClaudeAlias: r[9]}}
	}
	return out, nil
}

// reconcile drops slots whose session ended and adopts sessions the layout does not know.
// It reports whether the layout changed.
func (l *appLayout) reconcile(panes map[string]paneInfo) bool {
	changed := false
	known := map[string]bool{}
	var gone []string
	for _, t := range l.Tabs {
		for _, v := range t.Views {
			for _, s := range v.Slots {
				known[s] = true
				if _, ok := panes[s]; !ok {
					gone = append(gone, s)
				}
			}
		}
	}
	for _, s := range gone {
		l.removeSlot(s)
		changed = true
	}
	for id := range panes {
		if !known[id] {
			if t := l.activeTab(); t != nil {
				v := &appView{ID: shortID(), Layout: layoutCols, Slots: []string{id}, Focus: id}
				t.Views = append(t.Views, v)
				changed = true
			}
		}
	}
	return changed
}

// appWorkspace serializes the operations on one running workspace.
type appWorkspace struct {
	mu sync.Mutex
	ws string
}

var (
	appWSMu sync.Mutex
	appWSs  = map[string]*appWorkspace{}
)

func appWS(ws string) *appWorkspace {
	appWSMu.Lock()
	defer appWSMu.Unlock()
	w := appWSs[ws]
	if w == nil {
		w = &appWorkspace{ws: ws}
		appWSs[ws] = w
	}
	return w
}

// tmuxLayoutName maps the app's arrangement of a view to the tmux layout the terminal version
// restores it with, and back.
func tmuxLayoutName(layout string) string {
	switch layout {
	case layoutRows:
		return "even-vertical"
	case layoutGrid:
		return "tiled"
	}
	return "even-horizontal"
}

func appLayoutName(tmuxLayout string) string {
	switch tmuxLayout {
	case "even-vertical":
		return layoutRows
	case "tiled":
		return layoutGrid
	}
	return layoutCols
}

// appCapture reads a running app workspace into the saved format, shared with the terminal
// version: tabs, their sessions in view order, and the arrangement of each view.
func appCapture(t Tmux) (*Workspace, error) {
	panes, err := t.listSlotPanes()
	if err != nil {
		return nil, err
	}
	l := t.loadLayout()
	w := &Workspace{Name: t.WS}
	if title, _ := t.Run("show", "-gqv", optWSTitle); title != t.WS {
		w.Title = title
	}
	for ti, at := range l.Tabs {
		tab := Tab{Title: at.Title, Dir: at.Dir}
		if at.ID == l.Active {
			w.ActiveTab = ti
		}
		view := 0
		for _, v := range at.Views {
			var slots []Slot
			for _, id := range v.Slots {
				p, ok := panes[id]
				if !ok {
					continue
				}
				s := p.slot
				s.View = view
				if s.Alias == "" {
					s.Alias = "shell"
				}
				if s.Kind == kindShell {
					s.Cwd = p.path
					s.SessionID, s.Status = "", ""
				} else if s.Status == statusSleeping || s.Status == "" {
					s.Status = statusIdle
				}
				if s.Cwd == "" {
					s.Cwd = at.Dir
				}
				if v.ID == at.Active && id == v.Focus {
					tab.ActiveSlot = len(tab.Slots) + len(slots)
				}
				slots = append(slots, s)
			}
			if len(slots) == 0 {
				continue
			}
			layout := ""
			if len(slots) > 1 {
				layout = tmuxLayoutName(v.Layout)
			}
			tab.Layouts = append(tab.Layouts, layout)
			tab.Slots = append(tab.Slots, slots...)
			view++
		}
		w.Tabs = append(w.Tabs, tab)
	}
	return w, nil
}

// appSave writes the state of a running app workspace.
func appSave(ws string) error {
	t := Tmux{WS: ws}
	w, err := appCapture(t)
	if err != nil {
		return err
	}
	if len(w.Tabs) == 0 {
		return nil
	}
	w.SavedAt = time.Now()
	return saveWorkspace(w)
}

// appStartServer starts the tmux server of a workspace in app mode, with its keeper session.
func appStartServer(t Tmux, bin string) error {
	if err := writeFileAtomic(tmuxConfFile(t.WS), []byte(appTmuxConf())); err != nil {
		return err
	}
	if _, err := t.Run("new-session", "-d", "-s", appKeeper, "-x", "80", "-y", "24", bin, "_idle"); err != nil {
		return err
	}
	_, err := t.Run("set", "-g", optAppMode, "1")
	return err
}

// appOpenSlot starts one session of a tab. resuming reopens its conversation.
func appOpenSlot(t Tmux, l *Launcher, s *Slot, resuming bool) error {
	if s.ID == "" {
		s.ID = newUUID()
	}
	spec := l.slotSpec(slotSession(s.ID), true, s, resuming)
	_, err := t.createWindow(spec)
	return err
}

// appRestore starts a saved workspace in app mode.
func appRestore(t Tmux, l *Launcher, w *Workspace) error {
	if err := appStartServer(t, l.Bin); err != nil {
		return err
	}
	title := w.Title
	if title == "" {
		title = w.Name
	}
	_, _ = t.Run("set", "-g", optWSTitle, title)
	lay := &appLayout{}
	if len(w.Tabs) == 0 {
		w.Tabs = []Tab{{Title: "inicio", Dir: homeDir()}}
	}
	for ti := range w.Tabs {
		tab := &w.Tabs[ti]
		if !isDir(tab.Dir) {
			tab.Dir = homeDir()
		}
		at := &appTab{ID: shortID(), Title: tab.Title, Dir: tab.Dir}
		// State saved before views existed has no layouts: each session had a view of its own.
		if len(tab.Layouts) == 0 {
			for i := range tab.Slots {
				tab.Slots[i].View = i
			}
		}
		views := map[int]*appView{}
		for i := range tab.Slots {
			s := &tab.Slots[i]
			if s.Kind == kindHome {
				continue
			}
			if s.Cwd == "" {
				s.Cwd = tab.Dir
			}
			if err := appOpenSlot(t, l, s, true); err != nil {
				return fmt.Errorf("pestaña %q: %w", tab.Title, err)
			}
			v := views[s.View]
			if v == nil {
				layout := layoutCols
				if s.View < len(tab.Layouts) {
					layout = appLayoutName(tab.Layouts[s.View])
				}
				v = &appView{ID: shortID(), Layout: layout}
				views[s.View] = v
				at.Views = append(at.Views, v)
			}
			v.Slots = append(v.Slots, s.ID)
			if v.Focus == "" {
				v.Focus = s.ID
			}
			if i == tab.ActiveSlot {
				v.Focus = s.ID
				at.Active = v.ID
			}
		}
		lay.Tabs = append(lay.Tabs, at)
		if ti == w.ActiveTab {
			lay.Active = at.ID
		}
	}
	return t.storeLayout(lay)
}

// appEnsure makes a workspace run in app mode: it restores a saved one, or creates it with one
// empty tab in dir. A workspace running in the terminal version is left alone (errInTerminal).
func appEnsure(ws, dir, title string, seed *Tab) error {
	if err := validWorkspaceName(ws); err != nil {
		return err
	}
	a := appWS(ws)
	a.mu.Lock()
	defer a.mu.Unlock()
	t := Tmux{WS: ws}
	if t.Running() {
		if !appMode(t) {
			return errInTerminal
		}
		return nil
	}
	if err := checkTmux(); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	l, err := newLauncher(ws, cfg)
	if err != nil {
		return err
	}
	saved, err := loadWorkspace(ws)
	switch {
	case err == nil:
		return appRestore(t, l, saved)
	case errors.Is(err, os.ErrNotExist):
		w := &Workspace{Name: ws, Title: title}
		if seed != nil {
			l.Cfg.LazyStart = false
			w.Tabs = []Tab{*seed}
		} else {
			w.Tabs = []Tab{{Title: baseName(dir), Dir: dir}}
		}
		if err := appRestore(t, l, w); err != nil {
			return err
		}
		if dir != "" {
			_, _ = t.Run("set", "-g", optLaunchAt, dir)
		}
		return appSave(ws)
	}
	return err
}

var errInTerminal = errors.New("este workspace está abierto en la versión de terminal")
