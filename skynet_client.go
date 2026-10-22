package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// skynetPrompt is Skynet's whole system prompt: who it is, what it can do and how it answers.
func skynetPrompt(cfg Config) string {
	name := cfg.skynetName()
	approve := "Un permiso (la sesión pide autorización para ejecutar algo) NUNCA lo respondas por tu cuenta: dile al usuario qué pide y espera " +
		"a que lo autorice o lo niegue de forma explícita; solo entonces usa responder_pregunta con confirmado_por_usuario=true."
	if cfg.SkynetAutoApprove {
		approve = "El usuario te autorizó a responder permisos sin preguntarle, pero si un permiso parece destructivo (borrar datos, " +
			"forzar un push, tocar producción) pregúntale antes."
	}
	return strings.Join([]string{
		"Eres " + name + ", el orquestador de Agent Workspaces (cw). El usuario trabaja con muchas sesiones de Claude Code, Codex y de terminal " +
			"repartidas en workspaces, pestañas y vistas. Tú no trabajas en ningún proyecto ni tienes su código: solo ves y manejas esas sesiones " +
			"con las herramientas de cw (listar, leer su pantalla, enviarles instrucciones, responder sus preguntas, abrir, cerrar, suspender…).",
		"Habla en español neutro latinoamericano, de tú. Sé breve: el usuario a menudo te habla por voz y lee tus respuestas de un vistazo. " +
			"Una o dos frases, sin encabezados ni listas largas, salvo que te pida detalle.",
		"Las instrucciones llegan transcritas por voz y pueden traer errores: interpreta nombres de pestañas y sesiones de forma aproximada " +
			"(comparando con listar_sesiones) y, si hay ambigüedad real, pregunta cuál.",
		"Cuando el usuario te pida que una sesión haga algo, envíale la instrucción con enviar_a_sesion tal como la diría el usuario, " +
			"en primera persona y con el detalle que dio; no inventes contexto del proyecto. Antes de actuar sobre una sesión, ubícala con " +
			"listar_sesiones si no tienes su id.",
		"Los mensajes que empiezan con [Aviso de cw] no son del usuario: cw te avisa que una sesión espera respuesta. El usuario ya ve una " +
			"tarjeta con la pregunta y sus opciones; dile en una frase qué sesión pregunta y qué decide, sin repetir todas las opciones, y espera.",
		approve,
		"Acciones que cierran o reinician sesiones, cierran pestañas o apagan workspaces: hazlas solo si el usuario lo pidió claramente.",
		"Si una herramienta falla, explica en una frase qué pasó y qué puede hacer el usuario.",
	}, "\n\n")
}

// prepareSkynetDir creates Skynet's working folder: empty of any project, with its own skills.
func prepareSkynetDir() error {
	skills := map[string]string{
		"resumen": `---
name: resumen
description: Resumen del estado de todas las sesiones. Úsalo cuando el usuario pregunte cómo va todo, qué están haciendo las sesiones o qué falta.
---

1. Llama a listar_sesiones sin filtros y a preguntas_pendientes.
2. Si hay sesiones trabajando y el usuario quiere detalle, lee la pantalla (leer_sesion) solo de las que nombre.
3. Responde en dos o tres frases: cuántas trabajan, cuáles esperan una respuesta (primero estas, con lo que preguntan) y cuáles están listas.
`,
		"atender-preguntas": `---
name: atender-preguntas
description: Revisar y responder las preguntas pendientes de las sesiones, una por una, con el usuario.
---

1. Llama a preguntas_pendientes.
2. Para cada una, dile al usuario en una frase qué sesión pregunta, qué pregunta y las opciones resumidas.
3. Espera su decisión y respóndela con responder_pregunta. Un permiso solo con su autorización explícita.
4. Pasa a la siguiente hasta terminar.
`,
	}
	for name, body := range skills {
		dir := filepath.Join(skynetDir(), ".claude", "skills", name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := writeFileAtomic(filepath.Join(dir, "SKILL.md"), []byte(body)); err != nil {
			return err
		}
	}
	return nil
}

// skynetHTTP talks to the service over its Unix socket.
func skynetHTTP(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", skynetSocket())
		},
	}}
}

func skynetCall(method, path string, body any) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rd = bytes.NewReader(data)
	}
	req, _ := http.NewRequest(method, "http://skynet"+path, rd)
	resp, err := skynetHTTP(10 * time.Second).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New(strings.TrimSpace(string(data)))
	}
	return data, nil
}

func skynetRunning() bool {
	_, err := skynetCall("GET", "/status", nil)
	return err == nil
}

// ensureSkynet starts the service when Skynet is on and it is not running.
func ensureSkynet() error {
	if skynetRunning() {
		return nil
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(runDir(), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(filepath.Join(runDir(), "skynet.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	cmd := exec.Command(self, "skynet", "--servicio")
	cmd.Dir = homeDir()
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	for i := 0; i < 80; i++ {
		time.Sleep(75 * time.Millisecond)
		if skynetRunning() {
			return nil
		}
	}
	data, _ := os.ReadFile(filepath.Join(runDir(), "skynet.log"))
	return fmt.Errorf("Skynet no arrancó: %s", strings.TrimSpace(string(data)))
}

// skynetTick keeps the service in step with the setting; the autosave of every workspace and the
// app's server call it.
func skynetTick() {
	cfg, err := loadConfig()
	if err != nil {
		return
	}
	if cfg.SkynetEnabled {
		_ = ensureSkynet()
	}
	// Turned off: the service notices the setting by itself and stops.
}

// cmdSkynet: cw skynet (chat in the terminal) | --servicio | --detener | --estado | --nueva.
func cmdSkynet(args []string) error {
	flag := ""
	if len(args) > 0 {
		flag = args[0]
	}
	switch flag {
	case "--servicio":
		return cmdSkynetService()
	case "--detener", "--stop":
		_, err := skynetCall("POST", "/stop", map[string]any{})
		if err != nil {
			fmt.Println("Skynet no está corriendo")
		}
		return nil
	case "--estado", "--status":
		data, err := skynetCall("GET", "/status", nil)
		if err != nil {
			fmt.Println("Skynet no está corriendo")
			return nil
		}
		fmt.Println(string(data))
		return nil
	case "--nueva", "--reset":
		_, err := skynetCall("POST", "/reset", map[string]any{})
		return err
	case "":
	default:
		return errors.New("uso: cw skynet [--estado|--detener|--nueva]")
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if !cfg.SkynetEnabled {
		msg := "Skynet está apagado: actívalo en Alt+m → Configuración (o Ajustes → Skynet en la app)."
		if os.Getenv("TMUX") != "" { // a popup closes at once: leave the message readable
			pause(msg)
			return nil
		}
		return errors.New(msg)
	}
	if err := ensureSkynet(); err != nil {
		return err
	}
	return skynetChat(cfg)
}

// skynetChat is the terminal's chat: it prints the conversation as it happens and sends each
// line typed. An empty line or Ctrl+D leaves (Skynet keeps running).
func skynetChat(cfg Config) error {
	req, _ := http.NewRequest("GET", "http://skynet/events", nil)
	resp, err := skynetHTTP(0).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	name := cfg.skynetName()
	fmt.Printf("%s%s✦ %s%s%s   escribe y pulsa Enter · línea vacía para salir%s\n\n", sgrAccent, sgrBold, name, sgrReset, sgrDim, sgrReset)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 16<<20)
		for sc.Scan() {
			line, ok := strings.CutPrefix(sc.Text(), "data: ")
			if !ok {
				continue
			}
			var ev struct {
				Type    string     `json:"type"`
				Entry   skyEntry   `json:"entry"`
				Entries []skyEntry `json:"entries"`
			}
			if json.Unmarshal([]byte(line), &ev) != nil {
				continue
			}
			switch ev.Type {
			case "history":
				for _, e := range ev.Entries[max(0, len(ev.Entries)-20):] {
					printSkyEntry(name, e)
				}
			case "entry":
				printSkyEntry(name, ev.Entry)
			}
		}
	}()
	in := bufio.NewReader(os.Stdin)
	for {
		line, err := in.ReadString('\n')
		line = strings.TrimSpace(line)
		if err != nil || line == "" {
			return nil
		}
		fmt.Print("\033[1A\033[2K") // the line comes back as an entry
		if _, err := skynetCall("POST", "/send", map[string]any{"text": line, "source": "texto"}); err != nil {
			fmt.Println(sgrRed + err.Error() + sgrReset)
		}
	}
}

func printSkyEntry(name string, e skyEntry) {
	switch e.Kind {
	case "user":
		fmt.Printf("%s› %s%s\n", sgrBold, e.Text, sgrReset)
	case "assistant":
		fmt.Printf("%s✦ %s:%s %s\n\n", sgrAccent, name, sgrReset, e.Text)
	case "tool":
		fmt.Printf("%s  ⚙ %s %s%s\n", sgrDim, e.Tool, e.Text, sgrReset)
	case "notice":
		ws := e.WSTitle
		if ws == "" {
			ws = e.WS
		}
		fmt.Printf("%s? %s › %s › %s pregunta:%s %s\n", sgrRed, ws, e.Tab, e.Name, sgrReset, e.Text)
		for _, o := range e.Options {
			fmt.Printf("    %v. %v\n", o["n"], o["texto"])
		}
	case "error":
		fmt.Printf("%s%s%s\n", sgrRed, e.Text, sgrReset)
	default:
		fmt.Printf("%s%s%s\n", sgrMuted, e.Text, sgrReset)
	}
}
