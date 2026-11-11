---
name: cw
description: Detecta si esta sesión corre dentro de cw (Agent Workspaces) y en qué workspace, pestaña y subpestaña (vista). Úsala antes de crear pestañas, subpestañas o sesiones, abrir un documento Markdown para el usuario, listar o leer otras sesiones, o cuando el usuario mencione cw, workspaces, pestañas, subpestañas, vistas o sesiones.
---

# cw: workspace, pestañas, subpestañas y sesiones

## 1. ¿Corro dentro de cw?

Compruébalo así, sin adivinar:

```sh
[ -n "$CW_WS" ] && [ -n "$CW_SLOT" ] && echo "en cw" || echo "fuera de cw"
```

cw define estas variables en cada sesión que abre, y solo ahí:

- `CW_WS`: nombre interno del workspace.
- `CW_SLOT`: id de esta sesión.
- `CW_BIN`: ruta del ejecutable de cw. Úsala aunque `cw` no esté en el PATH.

Si no están definidas, no corres en cw: no uses estos comandos y díselo al usuario si te
pidió algo de cw.

## 2. ¿Dónde estoy ahora?

```sh
"$CW_BIN" yo
```

Devuelve un JSON con `workspace`, `workspace_title`, `app` (true en la app de escritorio,
false en la terminal), `tab_index` y `tab`, `view` (la subpestaña), `id` y `name` de esta
sesión, y `status`. La pestaña y la vista pueden cambiar si el usuario mueve la sesión:
vuelve a ejecutarlo antes de decidir dónde abrir algo. Sale con código 1 fuera de cw.

## 3. Ver lo que existe

```sh
"$CW_BIN" tool listar_pestanas                 # pestañas del workspace actual
"$CW_BIN" tool listar_vistas --pestana 2       # subpestañas (vistas) de una pestaña y sus sesiones
"$CW_BIN" tool listar_sesiones                 # sesiones y su estado: trabajando, pregunta, lista, suspendida
"$CW_BIN" tool leer_sesion --sesion beta --lineas 80
"$CW_BIN" tool                                 # todas las herramientas y sus opciones
```

Si no indicas `--workspace`, se usa el de esta sesión. Las pestañas se nombran por número
o título; las sesiones, por id (o sus primeros caracteres) o por nombre.

## 4. Crear y manejar

```sh
# Pestaña nueva con una sesión de Claude
"$CW_BIN" tool pestana --accion nueva --titulo API --carpeta ~/api --iniciar claude --nombre backend
# Sesión nueva en una pestaña: sin --vista abre una subpestaña nueva; con --vista N la agrega a esa vista (solo la app)
"$CW_BIN" tool sesion --accion nueva --pestana API --tipo claude --nombre tests
"$CW_BIN" tool sesion --accion nueva --pestana API --tipo terminal --vista 1
# Otras acciones
"$CW_BIN" tool sesion --accion renombrar --sesion tests --nombre e2e
"$CW_BIN" tool sesion --accion separar --sesion e2e      # la saca a una subpestaña propia
"$CW_BIN" tool vista --accion disposicion --pestana API --vista 1 --disposicion columnas
```

Cerrar pestañas, vistas o sesiones detiene lo que corre en ellas: hazlo solo si el
usuario lo pidió.

## 5. Abrir un documento para el usuario

```sh
"$CW_BIN" open -- docs/plan.md
```

Se abre en una subpestaña de solo lectura junto a esta sesión, en la app o en la
terminal. Úsalo en lugar de editores externos. Para abrirlo en otro sitio:
`"$CW_BIN" open --workspace W --tab 2 --view 1 -- ruta.md`.

## 6. Hablar con otras sesiones

Usa la skill `cw-coordinar` (`"$CW_BIN" send …`), no la mensajería propia del agente.
