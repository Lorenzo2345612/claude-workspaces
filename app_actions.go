package main

import (
	"errors"
	"fmt"
)

// wsAction runs an action on a workspace open in the app; the caller holds its lock.
func wsAction(t Tmux, q *doReq) (map[string]any, error) {
	l := t.loadLayout()
	save := func() error { return t.storeLayout(l) }
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	sizeHint := func() {
		if q.Cols >= 20 && q.Rows >= 5 {
			_, _ = t.Run("set", "-g", "default-size", fmt.Sprintf("%dx%d", q.Cols, q.Rows))
		}
	}
	tabOf := func(id string) (*appTab, error) {
		if at := l.tab(id); at != nil {
			return at, nil
		}
		return nil, errors.New("esa pestaña ya no existe")
	}
	paneOf := func(slot string) (string, error) {
		p, err := t.Run("display", "-p", "-t", "="+slotSession(slot)+":", "#{pane_id}")
		if err != nil {
			return "", errors.New("esa sesión ya no existe")
		}
		return p, nil
	}
	launcher := func(lazy bool) (*Launcher, error) {
		c := cfg
		c.LazyStart = lazy
		return newLauncher(t.WS, c)
	}

	switch q.Action {
	case "tab.new":
		dir, err := expandPath(q.Dir)
		if err != nil || !isDir(dir) {
			return nil, fmt.Errorf("la carpeta %q no existe", q.Dir)
		}
		title := cleanLabel(q.Title)
		if title == "" {
			title = baseName(dir)
		}
		at := &appTab{ID: shortID(), Title: title, Dir: dir}
		l.Tabs = append(l.Tabs, at)
		l.Active = at.ID
		if q.Start == kindClaude || q.Start == kindShell {
			sizeHint()
			ln, err := launcher(false)
			if err != nil {
				return nil, err
			}
			s := Slot{Kind: q.Start, Alias: "shell", Cwd: dir}
			if q.Start == kindClaude {
				s.Alias = cleanLabel(q.Name)
				if s.Alias == "" {
					s.Alias = title
				}
				s.ClaudeAlias = q.Alias
				s.Args = splitArgs(q.Args)
				recordAlias(s.Alias, title)
			}
			s.Status = statusIdle
			if err := appOpenSlot(t, ln, &s, false); err != nil {
				return nil, err
			}
			v := &appView{ID: shortID(), Layout: layoutCols, Slots: []string{s.ID}, Focus: s.ID}
			at.Views, at.Active = []*appView{v}, v.ID
		}
		return map[string]any{"tab": at.ID}, save()
	case "tab.select":
		if _, err := tabOf(q.Tab); err != nil {
			return nil, err
		}
		l.Active = q.Tab
		return nil, save()
	case "tab.rename":
		at, err := tabOf(q.Tab)
		if err != nil {
			return nil, err
		}
		if title := cleanLabel(q.Title); title != "" {
			at.Title = title
		}
		return nil, save()
	case "tab.move":
		i := l.tabIndex(q.Tab)
		if i < 0 || q.To < 0 || q.To >= len(l.Tabs) {
			return nil, nil
		}
		at := l.Tabs[i]
		l.Tabs = append(l.Tabs[:i], l.Tabs[i+1:]...)
		l.Tabs = append(l.Tabs[:q.To], append([]*appTab{at}, l.Tabs[q.To:]...)...)
		return nil, save()
	case "tab.close":
		i := l.tabIndex(q.Tab)
		if i < 0 {
			return nil, nil
		}
		at := l.Tabs[i]
		for _, v := range at.Views {
			for _, s := range v.Slots {
				closeAppSlot(t, s, "")
			}
		}
		l.Tabs = append(l.Tabs[:i], l.Tabs[i+1:]...)
		if len(l.Tabs) == 0 {
			// The workspace keeps running with one empty tab (⏻ stops it).
			l.Tabs = []*appTab{{ID: shortID(), Title: baseName(at.Dir), Dir: at.Dir}}
		}
		if l.Active == at.ID {
			l.Active = l.Tabs[min(i, len(l.Tabs)-1)].ID
		}
		return nil, save()
	case "view.select":
		at, v := l.findView(q.View)
		if v == nil {
			return nil, nil
		}
		at.Active, l.Active = v.ID, at.ID
		return nil, save()
	case "view.move":
		at, v := l.findView(q.View)
		if v == nil || q.To < 0 || q.To >= len(at.Views) {
			return nil, nil
		}
		i := at.viewIndex(v.ID)
		at.Views = append(at.Views[:i], at.Views[i+1:]...)
		at.Views = append(at.Views[:q.To], append([]*appView{v}, at.Views[q.To:]...)...)
		return nil, save()
	case "view.join":
		// Shows the sessions of view q.View next to those of q.Target, in q.Target's place.
		_, src := l.findView(q.View)
		dt, dst := l.findView(q.Target)
		if src == nil || dst == nil || src == dst {
			return nil, nil
		}
		ids := append([]string{}, src.Slots...)
		for _, id := range ids {
			l.removeSlot(id)
		}
		dst.Slots = append(dst.Slots, ids...)
		dt.Active, l.Active = dst.ID, dt.ID
		if len(ids) > 0 {
			dst.Focus = ids[0]
		}
		return nil, save()
	case "view.layout":
		_, v := l.findView(q.View)
		if v == nil {
			return nil, nil
		}
		switch q.Layout {
		case layoutCols, layoutRows, layoutGrid:
			v.Layout = q.Layout
		default:
			return nil, fmt.Errorf("distribución desconocida %q", q.Layout)
		}
		return nil, save()
	case "view.totab":
		at, v := l.findView(q.View)
		dst, err := tabOf(q.Tab)
		if v == nil || err != nil || at == dst {
			return nil, err
		}
		at.Views = append(at.Views[:at.viewIndex(v.ID)], at.Views[at.viewIndex(v.ID)+1:]...)
		if at.Active == v.ID {
			at.Active = ""
		}
		dst.Views = append(dst.Views, v)
		dst.Active, l.Active = v.ID, dst.ID
		return nil, save()
	case "view.close":
		_, v := l.findView(q.View)
		if v == nil {
			return nil, nil
		}
		at, _ := l.findView(q.View)
		for _, s := range append([]string{}, v.Slots...) {
			closeAppSlot(t, s, at.ID)
			l.removeSlot(s)
		}
		return nil, save()
	case "slot.new":
		at, err := tabOf(q.Tab)
		if err != nil {
			return nil, err
		}
		cwd := at.Dir
		if q.Dir != "" {
			if d, err := expandPath(q.Dir); err == nil && isDir(d) {
				cwd = d
			}
		}
		sizeHint()
		ln, err := launcher(false)
		if err != nil {
			return nil, err
		}
		s := Slot{Kind: q.Kind, Cwd: cwd, Status: statusIdle}
		resuming := false
		switch q.Kind {
		case kindClaude:
			s.Alias = cleanLabel(q.Name)
			if s.Alias == "" {
				return nil, errors.New("ponle un nombre a la sesión")
			}
			s.ClaudeAlias, s.Args = q.Alias, splitArgs(q.Args)
			if q.Resume != "" {
				found := findConversations(q.Resume, cfg)
				if len(found) == 0 {
					return nil, errors.New("no encontré esa conversación")
				}
				s.SessionID, resuming = found[0].conv.ID, true
				if q.Dir == "" && isDir(found[0].cwd) {
					s.Cwd = found[0].cwd // a conversation reopens where it was started
				}
			}
			recordAlias(s.Alias, at.Title)
		case kindShell:
			s.Alias = cleanLabel(q.Name)
			if s.Alias == "" {
				s.Alias = "terminal"
			}
		default:
			return nil, fmt.Errorf("tipo de sesión desconocido %q", q.Kind)
		}
		if err := appOpenSlot(t, ln, &s, resuming); err != nil {
			return nil, err
		}
		if _, v := l.findView(q.View); v != nil && q.View != "" {
			v.Slots = append(v.Slots, s.ID)
			v.Focus = s.ID
		} else {
			v := &appView{ID: shortID(), Layout: layoutCols, Slots: []string{s.ID}, Focus: s.ID}
			at.Views = append(at.Views, v)
			at.Active = v.ID
		}
		l.Active = at.ID
		return map[string]any{"slot": s.ID}, save()
	case "slot.focus":
		at, v := l.findSlot(q.Slot)
		if v == nil {
			return nil, nil
		}
		v.Focus, at.Active, l.Active = q.Slot, v.ID, at.ID
		return nil, save()
	case "slot.close":
		tabID := ""
		if at, _ := l.findSlot(q.Slot); at != nil {
			tabID = at.ID
		}
		closeAppSlot(t, q.Slot, tabID)
		l.removeSlot(q.Slot)
		return nil, save()
	case "slot.rename":
		pane, err := paneOf(q.Slot)
		if err != nil {
			return nil, err
		}
		name := cleanLabel(q.Name)
		if name == "" {
			return nil, errors.New("el nombre no puede quedar vacío")
		}
		return nil, setSlotName(t, pane, name)
	case "slot.edit":
		pane, err := paneOf(q.Slot)
		if err != nil {
			return nil, err
		}
		name := cleanLabel(q.Name)
		if name == "" {
			return nil, errors.New("el nombre no puede quedar vacío")
		}
		kind, _ := t.Run("display", "-p", "-t", pane, "#{@cw_kind}")
		oldAlias, _ := t.Run("display", "-p", "-t", pane, "#{@cw_claude_alias}")
		oldAlias = cfg.claudeAlias(oldAlias).Name
		if err := setSlotName(t, pane, name); err != nil {
			return nil, err
		}
		if kind != kindClaude {
			return nil, nil
		}
		newAlias := cfg.claudeAlias(q.Alias).Name
		if _, err := t.Run("set", "-p", "-t", pane, "@cw_args", argsToOption(splitArgs(q.Args)), ";",
			"set", "-p", "-t", pane, "@cw_claude_alias", newAlias); err != nil {
			return nil, err
		}
		if newAlias != oldAlias {
			return nil, respawnSlot(t.WS, pane, q.SendContinue)
		}
		if q.Restart {
			return nil, respawnSlot(t.WS, pane, false)
		}
		return nil, nil
	case "slot.respawn":
		pane, err := paneOf(q.Slot)
		if err != nil {
			return nil, err
		}
		return nil, respawnSlot(t.WS, pane, false)
	case "slot.split":
		at, v := l.findSlot(q.Slot)
		if v == nil || len(v.Slots) < 2 {
			return nil, nil
		}
		l.removeSlot(q.Slot)
		nv := &appView{ID: shortID(), Layout: layoutCols, Slots: []string{q.Slot}, Focus: q.Slot}
		i := at.viewIndex(v.ID)
		at.Views = append(at.Views[:i+1], append([]*appView{nv}, at.Views[i+1:]...)...)
		return nil, save()
	case "slot.suspend", "slot.wake", "tab.suspend", "tab.wake":
		var ids []string
		if q.Action[:4] == "slot" {
			ids = []string{q.Slot}
		} else if at := l.tab(q.Tab); at != nil {
			for _, v := range at.Views {
				ids = append(ids, v.Slots...)
			}
		}
		for _, id := range ids {
			pane, err := paneOf(id)
			if err != nil {
				continue
			}
			if q.Action[len(q.Action)-4:] == "wake" {
				err = wakePane(t.WS, pane)
			} else {
				err = suspendPane(t.WS, pane)
			}
			if err != nil {
				return nil, err
			}
		}
		return nil, nil
	case "slot.reopen":
		c, ok := popClosed(t)
		if !ok {
			return nil, errors.New("no hay sesiones cerradas para reabrir")
		}
		at := l.tab(c.Tab)
		if at == nil {
			at = l.tab(q.Tab)
		}
		if at == nil {
			at = l.activeTab()
		}
		if at == nil {
			return nil, errors.New("no hay ninguna pestaña")
		}
		sizeHint()
		ln, err := launcher(false)
		if err != nil {
			return nil, err
		}
		s := c.Slot
		if err := appOpenSlot(t, ln, &s, s.Kind == kindClaude && s.SessionID != ""); err != nil {
			return nil, err
		}
		v := &appView{ID: shortID(), Layout: layoutCols, Slots: []string{s.ID}, Focus: s.ID}
		at.Views = append(at.Views, v)
		at.Active, l.Active = v.ID, at.ID
		return map[string]any{"slot": s.ID}, save()
	case "tab.dir":
		at, err := tabOf(q.Tab)
		if err != nil {
			return nil, err
		}
		dir, err := checkDir(q.Dir)
		if err != nil {
			return nil, err
		}
		at.Dir = dir
		return nil, save()
	case "ws.dir":
		dir, err := checkDir(q.Dir)
		if err != nil {
			return nil, err
		}
		_, err = t.Run("set", "-g", optWSDir, dir)
		return nil, err
	case "alias.bulk":
		var targets []string
		switch q.Scope {
		case "slot":
			targets = []string{q.Slot}
		case "view":
			if _, v := l.findView(q.View); v != nil {
				targets = v.Slots
			}
		case "tab":
			if at := l.tab(q.Tab); at != nil {
				for _, v := range at.Views {
					targets = append(targets, v.Slots...)
				}
			}
		case "all":
			for _, at := range l.Tabs {
				for _, v := range at.Views {
					targets = append(targets, v.Slots...)
				}
			}
		}
		name := cfg.claudeAlias(q.Alias).Name
		changed := 0
		for _, id := range targets {
			pane, err := paneOf(id)
			if err != nil {
				continue
			}
			kind, _ := t.Run("display", "-p", "-t", pane, "#{@cw_kind}")
			cur, _ := t.Run("display", "-p", "-t", pane, "#{@cw_claude_alias}")
			if kind != kindClaude || cfg.claudeAlias(cur).Name == name {
				continue
			}
			if _, err := t.Run("set", "-p", "-t", pane, "@cw_claude_alias", name); err != nil {
				return nil, err
			}
			if err := respawnSlot(t.WS, pane, q.SendContinue); err != nil {
				return nil, err
			}
			changed++
		}
		return map[string]any{"changed": changed}, nil
	}
	return nil, fmt.Errorf("acción desconocida %q", q.Action)
}

// closeAppSlot ends a session of the app, remembering it so it can be reopened.
func closeAppSlot(t Tmux, id, tabID string) {
	if pane, err := t.Run("display", "-p", "-t", "="+slotSession(id)+":", "#{pane_id}"); err == nil {
		if s, err := paneSlot(t, pane); err == nil {
			pushClosed(t, s, tabID)
		}
	}
	_, _ = t.Run("kill-session", "-t", "="+slotSession(id))
}
