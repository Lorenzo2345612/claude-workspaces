package main

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Resource use of the sessions: memory and CPU of every process under each session's pane.

type procInfo struct {
	pid, ppid int
	rssKB     int64
	cpu       float64 // percent of one core
	comm      string
}

// readProcs returns every process with its memory and current CPU use. On Linux CPU comes from
// two /proc samples taken interval apart; on macOS from ps, whose %cpu is already recent.
func readProcs(interval time.Duration) map[int]*procInfo {
	if runtime.GOOS == "linux" {
		return readProcsLinux(interval)
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=,rss=,pcpu=,comm=").Output()
	if err != nil {
		return nil
	}
	procs := map[int]*procInfo{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 5 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		rss, _ := strconv.ParseInt(f[2], 10, 64)
		cpu, _ := strconv.ParseFloat(strings.ReplaceAll(f[3], ",", "."), 64)
		procs[pid] = &procInfo{pid: pid, ppid: ppid, rssKB: rss, cpu: cpu, comm: filepath.Base(strings.Join(f[4:], " "))}
	}
	return procs
}

func readProcsLinux(interval time.Duration) map[int]*procInfo {
	pageKB := int64(os.Getpagesize() / 1024)
	sample := func() map[int]*procInfo {
		entries, _ := os.ReadDir("/proc")
		procs := map[int]*procInfo{}
		for _, e := range entries {
			pid, err := strconv.Atoi(e.Name())
			if err != nil {
				continue
			}
			data, err := os.ReadFile("/proc/" + e.Name() + "/stat")
			if err != nil {
				continue
			}
			// comm is in parentheses and may hold spaces: parse after the last ')'.
			s := string(data)
			open, close := strings.IndexByte(s, '('), strings.LastIndexByte(s, ')')
			if open < 0 || close < open {
				continue
			}
			f := strings.Fields(s[close+1:])
			if len(f) < 22 {
				continue
			}
			ppid, _ := strconv.Atoi(f[1])
			utime, _ := strconv.ParseFloat(f[11], 64)
			stime, _ := strconv.ParseFloat(f[12], 64)
			rss, _ := strconv.ParseInt(f[21], 10, 64)
			procs[pid] = &procInfo{pid: pid, ppid: ppid, rssKB: rss * pageKB, cpu: utime + stime, comm: s[open+1 : close]}
		}
		return procs
	}
	a := sample()
	time.Sleep(interval)
	b := sample()
	const ticks = 100.0 // USER_HZ on every Linux we run on
	for pid, p := range b {
		if prev, ok := a[pid]; ok {
			p.cpu = (p.cpu - prev.cpu) / ticks / interval.Seconds() * 100
		} else {
			p.cpu = 0
		}
	}
	return b
}

// treeOf sums a process and all its descendants.
func treeOf(root int, procs map[int]*procInfo, children map[int][]int) (rssKB int64, cpu float64, n int, top string) {
	var topRSS int64
	var walk func(int)
	walk = func(pid int) {
		p := procs[pid]
		if p == nil {
			return
		}
		rssKB += p.rssKB
		cpu += p.cpu
		n++
		if p.rssKB > topRSS {
			topRSS, top = p.rssKB, p.comm
		}
		for _, c := range children[pid] {
			walk(c)
		}
	}
	walk(root)
	return
}

type sessionUse struct {
	Slot  string  `json:"slot"`
	Name  string  `json:"name"`
	Tab   string  `json:"tab"`
	Kind  string  `json:"kind"`
	RSSMB float64 `json:"rssMB"`
	CPU   float64 `json:"cpu"`
	Procs int     `json:"procs"`
	Top   string  `json:"top"`
}

type resourceReport struct {
	Sessions     []sessionUse `json:"sessions"`
	ClaudeMB     float64      `json:"claudeMB"`
	ClaudeCPU    float64      `json:"claudeCPU"`
	ShellMB      float64      `json:"shellMB"`
	ShellCPU     float64      `json:"shellCPU"`
	OutsideN     int          `json:"outsideN"` // claude processes on this machine outside the workspace
	OutsideMB    float64      `json:"outsideMB"`
	MachineMB    float64      `json:"machineMB"` // total and available memory, when known
	MachineFreeM float64      `json:"machineFreeMB"`
}

// workspaceResources measures every session of a running workspace. tabOf names the tab of a
// pane's session (the app keeps tabs in its layout, the terminal version on the tmux session).
func workspaceResources(ws string, tabOf func(session, title string) string) resourceReport {
	t := Tmux{WS: ws}
	var rep resourceReport
	rows, err := t.Lines("list-panes", "-a", "-F", strings.Join([]string{
		"#{session_name}", "#{pane_pid}", "#{@cw_slot}", "#{@cw_alias}", "#{@cw_kind}", "#{@cw_title}"}, sep))
	if err != nil {
		return rep
	}
	procs := readProcs(500 * time.Millisecond)
	children := map[int][]int{}
	for pid, p := range procs {
		children[p.ppid] = append(children[p.ppid], pid)
	}
	inside := map[int]bool{}
	var mark func(int)
	mark = func(pid int) {
		inside[pid] = true
		for _, c := range children[pid] {
			mark(c)
		}
	}
	for _, r := range rows {
		if len(r) != 6 || notSession(r[4]) || r[0] == appKeeper {
			continue
		}
		pid, _ := strconv.Atoi(r[1])
		mark(pid)
		rss, cpu, n, top := treeOf(pid, procs, children)
		kind := r[4]
		if kind == "" {
			kind = kindShell
		}
		name := r[3]
		if name == "" {
			name = "terminal"
		}
		u := sessionUse{Slot: r[2], Name: name, Tab: tabOf(r[0], r[5]), Kind: kind, RSSMB: float64(rss) / 1024, CPU: cpu, Procs: n, Top: top}
		rep.Sessions = append(rep.Sessions, u)
		if kind == kindClaude {
			rep.ClaudeMB += u.RSSMB
			rep.ClaudeCPU += u.CPU
		} else {
			rep.ShellMB += u.RSSMB
			rep.ShellCPU += u.CPU
		}
	}
	for pid, p := range procs {
		if !inside[pid] && strings.HasPrefix(strings.ToLower(p.comm), "claude") {
			rep.OutsideN++
			rep.OutsideMB += float64(p.rssKB) / 1024
		}
	}
	sort.SliceStable(rep.Sessions, func(i, j int) bool { return rep.Sessions[i].RSSMB > rep.Sessions[j].RSSMB })
	rep.MachineMB, rep.MachineFreeM = machineMemory()
	return rep
}

// machineMemory returns total and available memory in MB (Linux /proc/meminfo; macOS sysctl
// and vm_stat).
func machineMemory() (total, free float64) {
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, l := range strings.Split(string(data), "\n") {
			f := strings.Fields(l)
			if len(f) < 2 {
				continue
			}
			v, _ := strconv.ParseFloat(f[1], 64)
			switch f[0] {
			case "MemTotal:":
				total = v / 1024
			case "MemAvailable:":
				free = v / 1024
			}
		}
		return
	}
	if out, err := exec.Command("sysctl", "-n", "hw.memsize").Output(); err == nil {
		v, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		total = v / 1024 / 1024
	}
	if out, err := exec.Command("vm_stat").Output(); err == nil {
		page := 4096.0
		var pages float64
		for _, l := range strings.Split(string(out), "\n") {
			if strings.Contains(l, "page size of") {
				for _, w := range strings.Fields(l) {
					if v, err := strconv.ParseFloat(w, 64); err == nil {
						page = v
					}
				}
			}
			for _, k := range []string{"Pages free:", "Pages inactive:", "Pages speculative:"} {
				if strings.HasPrefix(l, k) {
					v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(strings.TrimPrefix(l, k)), "."), 64)
					pages += v
				}
			}
		}
		free = pages * page / 1024 / 1024
	}
	return
}

// fmtMB renders megabytes as "512 MB" or "1.4 GB".
func fmtMB(mb float64) string {
	if mb >= 1024 {
		return strconv.FormatFloat(mb/1024, 'f', 1, 64) + " GB"
	}
	return strconv.Itoa(int(mb+0.5)) + " MB"
}
