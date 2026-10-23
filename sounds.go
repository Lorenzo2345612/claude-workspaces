package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
)

//go:embed sounds/catalog.json
var soundCatalogJSON []byte

type notificationTone struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Duration float64 `json:"duration"`
	Notes    [][]any `json:"notes,omitempty"`
}

var notificationTones = func() []notificationTone {
	var tones []notificationTone
	if err := json.Unmarshal(soundCatalogJSON, &tones); err != nil {
		panic(err)
	}
	return tones
}()

func validSound(id string) bool {
	if id == "none" {
		return true
	}
	for _, tone := range notificationTones {
		if tone.ID == id {
			return true
		}
	}
	return false
}

func soundLabel(id string) string {
	if id == "none" {
		return "Sin sonido"
	}
	for _, tone := range notificationTones {
		if tone.ID == id {
			return tone.ID + " · " + tone.Name
		}
	}
	return id
}

// Sound data and envelopes match the approved HTML samples exactly.
func soundWAV(id string, volume int) ([]byte, error) {
	if volume < 0 || volume > 100 {
		return nil, errors.New("el volumen debe estar entre 0 y 100")
	}
	var tone *notificationTone
	for i := range notificationTones {
		if notificationTones[i].ID == id {
			tone = &notificationTones[i]
			break
		}
	}
	if tone == nil {
		return nil, errors.New("sonido desconocido")
	}
	const rate = 44100
	pcm := make([]float64, int(math.Ceil(tone.Duration*rate)))
	for _, note := range tone.Notes {
		start, hz, duration, kind, level := note[0].(float64), note[1].(float64), note[2].(float64), note[3].(string), note[4].(float64)
		offset := int(math.Round(start * rate))
		for i := 0; i < int(math.Round(duration*rate)) && offset+i < len(pcm); i++ {
			t := float64(i) / rate
			attack, release := math.Min(1, t/.009), math.Min(1, (duration-t)/.055)
			decayRate := 2.0
			if kind == "wood" {
				decayRate = 7
			} else if kind == "bell" {
				decayRate = 4
			}
			phase := 2 * math.Pi * hz * t
			if kind == "drop" {
				phase = 2 * math.Pi * (hz*t - 650*t*t)
			}
			wave := math.Sin(phase)
			switch kind {
			case "bell":
				wave = (wave + .23*math.Sin(phase*2) + .08*math.Sin(phase*3)) / 1.31
			case "wood":
				wave = (wave + .32*math.Sin(phase*2.76)) / 1.32
			case "soft":
				wave = (wave + .12*math.Sin(phase*2)) / 1.12
			}
			pcm[offset+i] += wave * attack * release * math.Exp(-decayRate*t/duration) * level
		}
	}
	data := make([]byte, 44+len(pcm)*2)
	copy(data, "RIFF")
	binary.LittleEndian.PutUint32(data[4:], uint32(len(data)-8))
	copy(data[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(data[16:], 16)
	binary.LittleEndian.PutUint16(data[20:], 1)
	binary.LittleEndian.PutUint16(data[22:], 1)
	binary.LittleEndian.PutUint32(data[24:], rate)
	binary.LittleEndian.PutUint32(data[28:], rate*2)
	binary.LittleEndian.PutUint16(data[32:], 2)
	binary.LittleEndian.PutUint16(data[34:], 16)
	copy(data[36:], "data")
	binary.LittleEndian.PutUint32(data[40:], uint32(len(pcm)*2))
	for i, v := range pcm {
		v = math.Max(-.95, math.Min(.95, v)) * float64(volume) / 100
		scale := 32767.0
		if v < 0 {
			scale = 32768
		}
		// JavaScript's Math.round ties toward positive infinity.
		binary.LittleEndian.PutUint16(data[44+i*2:], uint16(int16(math.Floor(v*scale+.5))))
	}
	return data, nil
}

func soundForHook(cfg Config, previous, event, next string) string {
	if cfg.SoundVolume == 0 {
		return ""
	}
	id := ""
	switch notificationEvent(previous, event, next) {
	case "question":
		id = cfg.QuestionSound
	case "completion":
		id = cfg.CompletionSound
	}
	if id == "none" {
		return ""
	}
	return id
}

func soundHookLock(ws, pane string) (*os.File, error) {
	key := sha256.Sum256([]byte(ws + ":" + pane))
	return usageLock(fmt.Sprintf("sound-hook-%x.lock", key[:12]), false)
}

// Both interfaces use this single host player, so an open app never doubles a hook's alert.
var playNotification = startNotificationSound

func startNotificationSound(id string, volume int) error {
	if id == "" || id == "none" || volume == 0 {
		return nil
	}
	if !validSound(id) || volume < 0 || volume > 100 {
		return errors.New("sonido o volumen inválido")
	}
	bin, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "_sound", id, strconv.Itoa(volume))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func cmdSound(args []string) error {
	if len(args) != 2 {
		return errors.New("uso: _sound <01..16|none> <0..100>")
	}
	volume, err := strconv.Atoi(args[1])
	if err != nil {
		return err
	}
	if !validSound(args[0]) || volume < 0 || volume > 100 {
		return errors.New("sonido o volumen inválido")
	}
	if args[0] == "none" || volume == 0 {
		return nil
	}
	data, err := soundWAV(args[0], volume)
	if err != nil {
		return err
	}
	dir := filepath.Join(runDir(), "sounds")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, args[0]+"-"+strconv.Itoa(volume)+".wav")
	if _, err = os.Stat(path); os.IsNotExist(err) {
		if err = writeFileAtomic(path, data); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	err = playHostSound(ctx, path)
	if err != nil {
		_ = writeFileAtomic(filepath.Join(runDir(), "sound-error.log"), []byte(time.Now().Format(time.RFC3339)+" "+err.Error()+"\n"))
	}
	return err
}

func playHostSound(ctx context.Context, path string) error {
	if runtime.GOOS == "darwin" {
		return exec.CommandContext(ctx, "afplay", path).Run()
	}
	// Prefer native Windows playback under WSL, even when Linux audio tools are installed.
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		powershell := "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
		if _, err := os.Stat(powershell); err == nil {
			winPath, err := exec.CommandContext(ctx, "wslpath", "-w", path).Output()
			if err == nil {
				script := "$ErrorActionPreference='Stop'; $p=New-Object System.Media.SoundPlayer; $p.SoundLocation='" + strings.ReplaceAll(strings.TrimSpace(string(winPath)), "'", "''") + "'; $p.Load(); $p.PlaySync(); $p.Dispose()"
				encoded := powershellEncodedCommand(script)
				cmd := exec.CommandContext(ctx, powershell, "-NoProfile", "-NonInteractive", "-EncodedCommand", encoded)
				cmd.Env = windowsSoundEnv()
				if err = cmd.Run(); err == nil {
					return nil
				}
			}
		}
	}
	for _, player := range []struct {
		name string
		args []string
	}{
		{"paplay", nil}, {"pw-play", nil}, {"aplay", []string{"-q"}}, {"ffplay", []string{"-nodisp", "-autoexit", "-loglevel", "quiet"}}, {"play", []string{"-q"}},
	} {
		if binary, err := exec.LookPath(player.name); err == nil {
			if err = exec.CommandContext(ctx, binary, append(player.args, path)...).Run(); err == nil {
				return nil
			}
		}
	}
	return errors.New("no se pudo reproducir audio; instala paplay, pw-play, aplay o ffplay y revisa la salida de audio del sistema")
}

func powershellEncodedCommand(script string) string {
	// PowerShell -EncodedCommand requires UTF-16LE, including non-ASCII user paths.
	units := utf16.Encode([]rune(script))
	data := make([]byte, len(units)*2)
	for i, v := range units {
		binary.LittleEndian.PutUint16(data[i*2:], v)
	}
	return base64.StdEncoding.EncodeToString(data)
}

func windowsSoundEnv() []string {
	env := os.Environ()
	current := os.Getenv("WSL_INTEROP")
	if current != "" {
		if _, err := os.Stat(current); err == nil {
			return env
		}
	}
	paths, _ := filepath.Glob("/run/WSL/*_interop")
	if len(paths) > 0 {
		var next []string
		for _, kv := range env {
			if !strings.HasPrefix(kv, "WSL_INTEROP=") {
				next = append(next, kv)
			}
		}
		return append(next, "WSL_INTEROP="+paths[0])
	}
	return env
}
