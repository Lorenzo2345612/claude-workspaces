package main

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// bar draws a usage bar of width cells for a percentage, coloured by how close it is to the limit.
func bar(pct float64, width int) string {
	full := int(pct/100*float64(width) + 0.5)
	full = max(0, min(width, full))
	col := sgrGreen
	switch {
	case pct >= 90:
		col = sgrRed
	case pct >= 70:
		col = sgrBusy
	}
	return col + strings.Repeat("█", full) + sgrDim + strings.Repeat("░", width-full) + sgrReset
}

// usageLines renders the usage of every account as terminal lines.
func usageLines(accounts []accountUsage, width int) []string {
	var out []string
	pad := func(s string) string { return "  " + s }
	for _, a := range accounts {
		head := sgrBold + strings.Join(a.Aliases, " · ") + sgrReset
		meta := []string{a.Short}
		if a.Plan != "" {
			meta = append([]string{a.Plan}, meta...)
		}
		out = append(out, pad(head+"   "+sgrDim+strings.Join(meta, " · ")+sgrReset))
		switch {
		case a.External != "":
			out = append(out, pad(sgrMuted+"Proveedor externo ("+shorten(a.External, 40)+"): sin límites de Claude."+sgrReset))
		case a.Err != "":
			out = append(out, pad(sgrBusy+a.Err+sgrReset))
		default:
			row := func(label string, w *usageWindow) {
				if w == nil {
					return
				}
				out = append(out, pad(fmt.Sprintf("%-13s %s %3.0f%%  %s", label, bar(w.Pct, 24), w.Pct, sgrDim+"se reinicia "+fmtReset(w.ResetsAt)+sgrReset)))
			}
			row("Sesión (5 h)", a.Session)
			row("Semana", a.Week)
			row("Semana Opus", a.WeekOpus)
			row("Semana Sonnet", a.WeekSonnet)
		}
		if a.External == "" {
			tok := func(label string, t tokTotals) string {
				return fmt.Sprintf("%s %s %s(entrada %s · salida %s · caché %s)%s", label, sgrBold+fmtTokens(t.Sum())+sgrReset, sgrDim,
					fmtTokens(t.In), fmtTokens(t.Out), fmtTokens(t.CacheW+t.CacheR), sgrReset)
			}
			out = append(out, pad("Tokens  "+tok("sesión:", a.TokSession)))
			out = append(out, pad("        "+tok("semana:", a.TokWeek)))
			if len(a.SharedWith) > 0 {
				out = append(out, pad(sgrDim+"(tokens sumados con "+strings.Join(a.SharedWith, ", ")+": comparten carpeta de conversaciones)"+sgrReset))
			}
		}
		out = append(out, "")
	}
	_ = width
	return out
}

// cmdUso prints the usage of every Claude account: cw uso.
func cmdUso() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	fmt.Println(sgrMuted + "Consultando el uso de cada cuenta…" + sgrReset)
	accounts := claudeUsage(cfg)
	sortAccounts(accounts)
	fmt.Print("\033[1A\033[2K")
	fmt.Println(sgrAccent + sgrBold + "Uso de Claude" + sgrReset + sgrDim + "   " + time.Now().Format("15:04") + sgrReset)
	fmt.Println()
	for _, l := range usageLines(accounts, 100) {
		fmt.Println(l)
	}
	return nil
}

// uiUsage shows the usage in a popup until a key or a click: _ui uso <ws>.
func uiUsage(ws string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	os.Stdout.WriteString("\033[?25l\033[H\033[2J\r\n  " + sgrMuted + "Consultando el uso de cada cuenta…" + sgrReset)
	accounts := claudeUsage(cfg)
	sortAccounts(accounts)
	var s screen
	s.at(2, 3, sgrBold+"Uso de Claude"+sgrReset+sgrDim+"   "+time.Now().Format("15:04")+"   r: actualizar"+sgrReset)
	for i, l := range usageLines(accounts, 100) {
		s.at(4+i, 1, l)
	}
	s.closeX("close")
	s.b.WriteString("\033[?25l")
	hits := s.hits
	s.flush(true)
	setRaw(true)
	defer setRaw(false)
	for {
		keys, err := readKeys()
		if err != nil {
			return nil
		}
		for _, k := range keys {
			if k.kind == keyRune && k.r == 'r' {
				setRaw(false)
				return uiUsage(ws)
			}
			if k.kind != keyClick || hitAt(hits, k.x, k.y) == "close" {
				return nil
			}
		}
	}
}

// uiResources shows memory and CPU of every session, refreshed every 2 s: _ui recursos <ws>.
func uiResources(ws string) error {
	t := Tmux{WS: ws}
	tabOf := func(session, title string) string {
		if title != "" {
			return title
		}
		v, _ := t.Run("show", "-t", session, "-qv", "@cw_title")
		return v
	}
	keys := make(chan key, 8)
	setRaw(true)
	defer setRaw(false)
	go func() {
		for {
			ks, err := readKeys()
			if err != nil {
				close(keys)
				return
			}
			for _, k := range ks {
				keys <- k
			}
		}
	}()
	var hits []hit
	for {
		rep := workspaceResources(ws, tabOf)
		var s screen
		s.at(2, 3, sgrBold+"Recursos"+sgrReset+sgrDim+"   se actualiza cada 2 s · cualquier tecla cierra"+sgrReset)
		s.at(4, 3, sgrMuted+fit("PESTAÑA", 16)+fit("SESIÓN", 26)+fit("MEMORIA", 11)+fit("CPU", 8)+"PROCESOS"+sgrReset)
		row := 5
		_, h := termSize()
		for _, u := range rep.Sessions {
			if row >= h-6 {
				s.at(row, 3, sgrDim+fmt.Sprintf("… y %d sesiones más", len(rep.Sessions)-(row-5))+sgrReset)
				row++
				break
			}
			name := u.Name
			if u.Kind == kindClaude {
				name = "✦ " + name
			} else {
				name = "❯ " + name
			}
			cpu := fmt.Sprintf("%.0f%%", u.CPU)
			if u.CPU >= 50 {
				cpu = sgrBusy + cpu + sgrReset
			}
			s.at(row, 3, fit(u.Tab, 15)+" "+fit(name, 25)+" "+fit(fmtMB(u.RSSMB), 10)+" "+fit(cpu, 7)+" "+sgrDim+fmt.Sprint(u.Procs)+sgrReset)
			row++
		}
		row++
		s.at(row, 3, sgrBold+"Claude"+sgrReset+"  "+fmtMB(rep.ClaudeMB)+sgrDim+fmt.Sprintf(" · %.0f%% CPU", rep.ClaudeCPU)+sgrReset+
			"    "+sgrBold+"Terminales"+sgrReset+"  "+fmtMB(rep.ShellMB)+sgrDim+fmt.Sprintf(" · %.0f%% CPU", rep.ShellCPU)+sgrReset)
		if rep.OutsideN > 0 {
			s.at(row+1, 3, sgrDim+fmt.Sprintf("Fuera de este workspace: %d procesos de Claude · %s", rep.OutsideN, fmtMB(rep.OutsideMB))+sgrReset)
		}
		if rep.MachineMB > 0 {
			s.at(row+2, 3, sgrDim+"Memoria del equipo: "+fmtMB(rep.MachineMB-rep.MachineFreeM)+" en uso de "+fmtMB(rep.MachineMB)+sgrReset)
		}
		s.closeX("close")
		s.b.WriteString("\033[?25l")
		hits = s.hits
		s.flush(true)
		select {
		case <-keys: // any key or click closes it
			_ = hits
			return nil
		case <-time.After(1500 * time.Millisecond):
		}
	}
}
