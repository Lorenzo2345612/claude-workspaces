---
name: cw-coordinar
description: Envía mensajes o tareas a otras sesiones de cw (Claude, Codex o terminal) y recibe su respuesta automáticamente, sin la mensajería propia del agente. Úsala cuando haya que delegar trabajo a otra sesión, preguntarle algo, repartir una tarea entre sesiones, o cuando llegue un mensaje que empiece con «[Respuesta de la sesión …]».
---

# Coordinarse con otras sesiones de cw

Solo funciona dentro de cw: comprueba que `CW_WS` y `CW_SLOT` estén definidas (skill `cw`).
Usa siempre `"$CW_BIN"`; no uses SendMessage ni otra mensajería del agente para esto.

## Enviar

```sh
"$CW_BIN" send backend -- "Revisa el endpoint /login y dime qué falla"
"$CW_BIN" send 3f2a9c1e -- "Corre los tests de integración"
printf '%s\n' "$PLAN" | "$CW_BIN" send tests -- -     # mensaje largo por stdin
"$CW_BIN" send --sin-respuesta backend -- "Para cuando termines"
```

- El destino es el id de la sesión (o sus primeros caracteres) o su nombre. Si el nombre
  se repite, agrega `--workspace W` o `--pestana T`. Para buscarlo: `"$CW_BIN" tool listar_sesiones`.
- El mensaje llega tal cual, sin cabecera, como si lo escribiera el usuario: escríbelo
  completo y claro, sin depender de que sepa quién lo envía.
- Si la sesión de destino está trabajando, lo recibe al terminar el paso actual. Si espera
  una pregunta o está suspendida, el envío falla: avísale al usuario.

## Recibir la respuesta

No la esperes ni la sondees con `leer_sesion` o `sleep`. Cuando la otra sesión termina su
turno, cw escribe su respuesta final en esta sesión como un mensaje nuevo:

```
[Respuesta de la sesión «backend» (id …) a tu mensaje enviado con cw send.]

…
```

Después de enviar, sigue con otra cosa o termina tu turno. Si delegaste a varias sesiones,
llegará una respuesta por cada una.

## Si recibes un mensaje de otra sesión

Llega como cualquier mensaje del usuario, sin cabecera. Respóndelo normalmente y termina tu
turno con la respuesta completa como último mensaje: cw se la entrega sola a quien lo envió.
No respondas con `"$CW_BIN" send`, salvo que el mensaje te lo pida, o le llegaría dos veces.
