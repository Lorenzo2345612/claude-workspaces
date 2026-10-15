# Cómo instalar cw

> **Dos paquetes.** «Claude Workspaces» (`claude-workspaces-<versión>-…`) trae la app de escritorio
> y también la versión de terminal. «cw» solo terminal (`cw-terminal-<versión>-…`) es más chico y no
> trae la app ni crea accesos directos. Los dos se instalan igual y guardan los workspaces en el
> mismo formato.
>
> **La app de escritorio:** después de instalar, `cw app` abre una ventana propia con tus
> workspaces (pestañas a la izquierda, sesiones arriba, terminales reales). Usa Edge o Chrome en
> modo aplicación, así que no instala ningún navegador extra. Si cierras la ventana, las sesiones
> siguen corriendo y vuelven a aparecer al abrirla.

`cw` organiza tus sesiones de Claude Code en pestañas dentro de la terminal y guarda y restaura
todo con un comando. Funciona en **macOS**, **Linux** y **Windows (a través de WSL)**.

## Qué necesitas antes

| Sistema | Requisito |
|---|---|
| Windows | Windows 10 versión 2004 o mayor, o Windows 11. Se recomienda Windows Terminal (viene en Windows 11). |
| macOS | macOS 12 o mayor. Homebrew (el instalador lo ofrece si no lo tienes). |
| Linux | Una distribución con `apt`, `dnf`, `pacman`, `zypper` o `apk`, y permisos de `sudo`. |

Además, una cuenta de Claude para usar Claude Code.

El instalador se encarga de lo demás y **pregunta antes de instalar cada cosa**:

- **tmux 3.3 o mayor**. Si tu sistema trae uno más viejo (por ejemplo Ubuntu 22.04), lo compila.
- **cw**, en `~/.local/bin`.
- **Claude Code**, con su instalador oficial, si todavía no lo tienes.

---

## Windows

1. Descomprime `claude-workspaces-0.12.3-windows.zip` (clic derecho → *Extraer todo…*).
2. Entra a la carpeta y haz **doble clic en `install.cmd`**.
3. Si Windows avisa que el archivo viene de internet, elige *Más información → Ejecutar de todas formas*.

Según cómo esté tu equipo:

- **Si no tienes WSL**: el instalador ofrece instalar Ubuntu. Acepta el permiso de administrador,
  **reinicia** si te lo pide, abre **Ubuntu** una vez desde el menú Inicio para crear tu usuario y
  contraseña, y vuelve a hacer doble clic en `install.cmd`.
- **Si ya tienes WSL**: instala todo dentro de tu distribución por defecto. Te puede pedir tu
  contraseña de Linux (la de `sudo`) para instalar tmux.

Al terminar, abre **Claude Workspaces** desde el menú Inicio o el escritorio: es la app, con su
propia ventana. La versión de terminal sigue disponible: abre **Windows Terminal**, haz clic en
la flecha **˅** junto a la pestaña nueva y elige el perfil **cw**, o escribe `cw` en cualquier
terminal de Ubuntu.

---

## macOS

1. Abre **Terminal** y ve a la carpeta donde descargaste el paquete:

   ```sh
   cd ~/Downloads
   tar xzf claude-workspaces-0.12.3-macos.tar.gz
   cd claude-workspaces-0.12.3-macos
   sh install.sh
   ```

2. Si no tienes Homebrew, el instalador te ofrece instalarlo (te pedirá tu contraseña de la Mac).
3. Abre una terminal nueva y escribe `cw`.

4. **Activa la tecla Option como Alt.** Los atajos de `cw` usan `Alt`, que en la Mac es la tecla
   **Option (⌥)**. De fábrica, Option escribe símbolos (⌥+m escribe `µ`), así que hay que decirle a
   la terminal que la use como Alt:

   | Terminal | Dónde |
   |---|---|
   | Terminal (la de Apple) | *Terminal → Configuración → Perfiles → Teclado* → marca **«Usar Opción como tecla Meta»** |
   | iTerm2 | *Settings → Profiles → Keys* → **Left Option key: Esc+** (y la derecha si quieres) |
   | Ghostty | en su configuración: `macos-option-as-alt = true` |
   | WezTerm | en `wezterm.lua`: `send_composed_key_when_left_alt_is_pressed = false` |

   Sin este ajuste los atajos no responden, pero todo se puede usar con clics en la barra de arriba.

> Si macOS dice que no puede verificar el desarrollador de `cw`: el instalador ya le quita esa
> marca, pero si aparece, ve a *Configuración del Sistema → Privacidad y seguridad* y pulsa
> *Abrir de todas formas*.

---

## Linux

```sh
tar xzf claude-workspaces-0.12.3-linux.tar.gz
cd claude-workspaces-0.12.3-linux
sh install.sh
```

Te pedirá tu contraseña de `sudo` para instalar tmux. Al terminar, abre una terminal nueva y
escribe `cw`.

---

## Opciones del instalador

| Opción | Qué hace |
|---|---|
| `--yes` | Acepta todas las preguntas (instalación desatendida). |
| `--no-claude` | No instala Claude Code (por ejemplo, si lo vas a instalar por tu cuenta). |

En Windows se pasan igual: `install.cmd -Yes -NoClaude`.

## Después de instalar

1. **Inicia sesión en Claude Code una vez**: escribe `claude` en la terminal y sigue los pasos.
2. Escribe **`cw`**: aparece la pantalla de inicio para crear un workspace en la carpeta actual o
   retomar uno guardado.
3. Dentro de un workspace (en Mac, `Alt` es la tecla **Option ⌥**; ver la sección de macOS):

   | Tecla | Acción |
   |---|---|
   | `Alt+m` | menú |
   | `Alt+t` | nueva pestaña |
   | `Alt+n` | nueva sesión (Claude o terminal) |
   | `Alt+e` | todas las sesiones (editar, cerrar) |
   | `Alt+p` | cambiar el alias de Claude de la sesión |
   | `Alt+←/→`, `Alt+↑/↓` | moverse entre sesiones y pestañas |
   | `Alt+<` / `Alt+>` | mover la pestaña |
   | `Alt+Shift+←/→` | mover la sesión |
   | `Alt+h` | ayuda |

   Todo también se hace con el ratón: clic en botones y listas, clic derecho sobre una pestaña o
   una sesión para su menú (mover, renombrar, cambiar alias de claude, cerrar), arrastrar
   pestañas y sesiones en la barra para reordenarlas, la ✕ de cada pestaña o sesión para
   cerrarla y ⏻ (arriba a la derecha) para apagar y guardar todo. El color se cambia en
   *Alt+m → Configuración → Tema de color*.

   **Empezar un workspace desde una conversación:** en la pantalla de inicio de `cw`,
   *Nuevo desde una conversación (ID)…*, o `cw new --id <ID>`. **Renombrar un workspace:**
   *Alt+m → Renombrar workspace…*, o *Renombrar* en la pantalla de inicio; los comandos aceptan
   el nombre nuevo.

   **Traer una conversación que abriste fuera de cw:** *Alt+n → Claude por ID…* (o en una
   pestaña vacía, *Retomar una conversación por su ID…*): pega el ID completo o sus primeros
   caracteres y se abre en la carpeta donde empezó.

   **Ver varias sesiones a la vez:** clic derecho sobre una sesión de la barra → *Ver junto a …*,
   o *Alt+m → Combinar sesiones en esta vista* para elegir varias y cómo acomodarlas (lado a
   lado, arriba y abajo, cuadrícula). El panel del clic derecho se queda abierto: puedes hacer
   varias acciones seguidas y lo cierras con *Cerrar* o Esc. En una vista combinada, cada
   sesión se maneja como si estuviera sola: clic en su nombre para ir a ella, su propia ✕ para
   cerrarla, y clic derecho sobre su nombre para renombrarla, editarla, cambiar su alias de
   claude o separarla. El clic derecho sobre «⧉» ofrece «Cambiar alias de toda la vista».

4. Si usas varios perfiles de Claude como alias del shell (`yolo`, `claude-2`…), regístralos en
   *Alt+m → Configuración → 7 → i* (los importa de tu `~/.bashrc`).

Comandos útiles: `cw ls` (lista workspaces), `cw stop NOMBRE` (guarda y apaga), `cw help`.

## Actualizar

```sh
cw update            # descarga e instala la última versión publicada
cw update --check    # solo avisa si hay una versión nueva
```

Verifica la descarga con su suma SHA-256 antes de reemplazar `cw`, y conserva tus workspaces y
tu configuración. Si tienes workspaces abiertos, desconéctate y vuelve a entrar para usar la
versión nueva. (Desde la versión 0.2.1; en versiones anteriores, descarga el paquete nuevo y
vuelve a correr el instalador.)

## Desinstalar

```sh
rm ~/.local/bin/cw
rm -rf ~/.local/share/cw ~/.config/cw      # workspaces guardados y configuración
```

En Windows, además, borra la carpeta
`%LOCALAPPDATA%\Microsoft\Windows Terminal\Fragments\cw` para quitar el perfil de Windows Terminal.
tmux y Claude Code se quedan instalados; quítalos con tu gestor de paquetes si ya no los usas.

## Problemas comunes

| Síntoma | Solución |
|---|---|
| `cw: command not found` | Abre una terminal nueva (el instalador agregó `~/.local/bin` al PATH). |
| «cw necesita tmux 3.3 o mayor» | Vuelve a correr el instalador: instala o compila uno reciente. |
| «no encuentro "claude" en el PATH» | Instala Claude Code (corre el instalador sin `--no-claude`) y abre una terminal nueva. |
| La primera sesión en una carpeta pregunta «Do you trust this folder?» | Es Claude Code pidiendo permiso para esa carpeta: elige *Yes*. Solo pasa una vez por carpeta. |
| Un menú o recuadro se ve corrido | Usa una fuente monoespaciada moderna (Cascadia Mono en Windows, SF Mono o Menlo en Mac). |
