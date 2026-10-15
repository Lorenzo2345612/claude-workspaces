#!/usr/bin/env python3
"""Generates mockups/rediseno-3.html: every screen of cw (desktop app and terminal version),
with two or three design options each. Terminal mockups are built cell by cell so boxes align."""
import html
import os
import unicodedata

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "rediseno-3.html")

# ---------------------------------------------------------------- terminal helpers
def w(s):
    return sum(2 if unicodedata.east_asian_width(c) in "WF" else 1 for c in s)

class Line:
    """A terminal line made of styled segments."""
    def __init__(self, *segs):
        self.segs = [s if isinstance(s, tuple) else (s, "") for s in segs]
    def width(self):
        return sum(w(t) for t, _ in self.segs)
    def pad(self, n, cls=""):
        if self.width() < n:
            self.segs.append((" " * (n - self.width()), cls))
        return self
    def html(self):
        out = []
        for t, c in self.segs:
            # Every non-ASCII glyph gets a cell of exactly one column, whatever font draws it.
            e = "".join(f'<span class="c">{html.escape(ch)}</span>' if ord(ch) > 127 else html.escape(ch) for ch in t)
            out.append(f'<span class="{c}">{e}</span>' if c else e)
        return "".join(out)

def L(*segs):
    return Line(*segs)

def box(lines, width, title="", close=True, border="dm", indent=0, fill=None):
    """Rounded box around lines (each padded to width-2)."""
    ind = " " * indent
    top = "╭─" + (f" {title} " if title else "")
    tail = " ✕ ╮" if close else "╮"
    top += "─" * (width - w(top) - w(tail)) + tail
    out = [L((ind, ""), (top, border))]
    for ln in lines:
        ln = ln if isinstance(ln, Line) else L(ln)
        body = Line(*ln.segs).pad(width - 2)
        out.append(L((ind, ""), ("│", border), *body.segs, ("│", border)))
    out.append(L((ind, ""), ("╰" + "─" * (width - 2) + "╯", border)))
    return out

def term(lines, cols=110, title=""):
    body = "\n".join(Line(*l.segs).pad(cols).html() if isinstance(l, Line) else html.escape(l) for l in lines)
    t = f'<div class="tt"><i></i><i></i><i></i>{html.escape(title)}</div>' if title else ""
    return f'<div class="T">{t}<pre>{body}</pre></div>'

def overlay(base, boxlines, top, left, dim=True):
    """Draws box lines over base lines (dimming the base) at row top, column left."""
    res = []
    for i, b in enumerate(base):
        b = b if isinstance(b, Line) else L(b)
        plain = "".join(t for t, _ in b.segs)
        if top <= i < top + len(boxlines):
            bl = boxlines[i - top]
            lt = plain[:left]
            rt_start = left + bl.width()
            rt = plain[rt_start:] if rt_start < len(plain) else ""
            d = "dd" if dim else ""
            res.append(L((lt.ljust(left), d), *bl.segs, (rt, d)))
        else:
            res.append(L((plain, "dd")) if dim else b)
    return res

# common terminal pieces
def bar2(active="bug-173"):
    tabs = [("bug-173", "●1", "bu"), ("backend", "", ""), ("bug-226", "●1", "bu"), ("infra", "z2", "dm")]
    segs = [(" ≡ Backend KYC  ", "mu")]
    for n, c, cc in tabs:
        if n == active:
            segs += [(f" {n} ", "sel b")]
        else:
            segs += [(f" {n} ", "mu")]
        if c:
            segs += [(c, cc)]
        segs += [("  ", "")]
    segs += [("+", "dm")]
    l1 = L(*segs)
    l1.pad(104)
    l1.segs.append(("? ⏻", "dm"))
    l2 = L(("   ", ""), ("fix-matriz", "fg b"), (" ●", "bu"), ("   review-291", "mu"), (" ●", "bu"),
           ("   tests", "mu"), (" ○", "ok"), ("   ❯ terminal", "mu"), ("   +", "dm"))
    l2.pad(94)
    l2.segs.append(("~/work/backend", "dm"))
    return [l1, l2, L(("─" * 110, "dm"))]

CLAUDE = [
    L(("❯ ", "fg"), ("arregla el filtro de acentos de la matriz de riesgo", "fg")),
    L(""),
    L(("● ", "ac"), ("Revisé "), ("risk_matrix/services/catalog.py", "ok"), (": la búsqueda compara sin normalizar.")),
    L(("  ⎿ Read webapp/risk_matrix/services/catalog.py (212 lines)", "dm")),
    L(("  ⎿ Edit catalog.py · +6 −2", "dm")),
    L(""),
    L(("● ", "ac"), ("Corrí los tests de "), ("risk_matrix", "ok"), (": 42 passed in 3.1s.")),
    L(""),
    L(("✻ Pensando… (esc para interrumpir)", "dm")),
    L(""),
    L(("╭" + "─" * 106 + "╮", "dm")),
    L(("│ ", "dm"), ("> ", "fg"), (" " * 103), ("│", "dm")),
    L(("╰" + "─" * 106 + "╯", "dm")),
]

def hints(text):
    return L((" " + text, "dm"))

# ---------------------------------------------------------------- page assembly
sections = []   # (id, title, kind, intro, [(option title, description, html)])

def screen(sid, title, kind, intro, options):
    sections.append((sid, title, kind, intro, options))

# ================================================================= DESKTOP
SIDE = """<aside class="side">
  <div class="wsrow"><div class="mark">≡</div><b>Backend KYC</b><span>⌄</span></div>
  <div class="grp"><span>Pestañas</span><span>+</span></div>
  <div class="t on"><span class="n">bug-185</span><span class="cnt"><i><span class="d b"></span>2</i><i><span class="d i"></span>1</i></span></div>
  <div class="t"><span class="n">front</span><span class="cnt"><i><span class="d w"></span>1</i></span></div>
  <div class="t"><span class="n">infra</span><span class="cnt"><i><span class="d z"></span>3</i></span></div>
  <div class="t"><span class="n">revisiones-291</span><span class="cnt"><i><span class="d i"></span>2</i></span></div>
  <div class="foot"><span class="ib">⚙</span><span class="ib" style="margin-left:auto">⏻</span></div>
</aside>"""
PILLS = """<div class="bar">
  <span class="p on"><span class="d b"></span>fix-matriz <span class="al">yolo</span><span class="x">✕</span></span>
  <span class="p"><span class="d b"></span>review-291</span><span class="p"><span class="d i"></span>tests</span>
  <span class="p"><span class="dimc">❯</span> terminal</span><span class="plus">+</span><span class="path">~/work/backend</span>
</div>"""
TERM = """<div class="term"><div class="u">❯ arregla el filtro de acentos de la matriz de riesgo</div>
<div class="sp"><span class="a">●</span> Revisé <span class="g">risk_matrix/services/catalog.py</span>: la búsqueda compara sin normalizar.</div>
<div class="m">  ⎿ Read webapp/risk_matrix/services/catalog.py (212 lines)</div>
<div class="sp"><span class="a">●</span> Corrí los tests: 42 passed in 3.1s.</div>
<div class="m sp">✻ Pensando… (esc para interrumpir)</div><div class="prompt">&gt;</div></div>"""
STATUS = '<div class="status"><span>● trabajando · yolo</span><span class="k"><kbd>Ctrl T</kbd> nueva · <kbd>Ctrl W</kbd> cerrar</span></div>'

def app(inner, cls="", h=420, extra=""):
    return f'<div class="A {cls}" style="height:{h}px">{inner}{extra}</div>'

def win(main, side=SIDE, cols="200px 1fr"):
    return f'<div class="win" style="grid-template-columns:{cols}">{side}<div class="main">{main}</div></div>'

# -- D1 inicio
screen("d-inicio", "Inicio · workspaces", "desktop",
       "Lo primero que se ve al abrir la app.",
       [("A · Lista", "Una fila por workspace con su estado, pestañas y cuándo se usó. Escala bien con muchos workspaces.",
         app("""<div class="home"><h3>Workspaces</h3><p>Retoma uno o crea uno nuevo.</p><div class="list">
<div class="row hl"><b>Backend KYC</b><span class="st"><span class="d i"></span>abierto</span><span>4 pestañas</span><span>ahora</span></div>
<div class="row"><b>Revisión 291</b><span class="st"><span class="d b"></span>2 trabajaban</span><span>2 pestañas</span><span>ayer</span></div>
<div class="row"><b>Manual KYC</b><span class="st"><span class="d z"></span>apagado</span><span>1 pestaña</span><span>12 oct</span></div>
<div class="row"><b>frontend-301</b><span class="st warnc">en la terminal</span><span>3 pestañas</span><span>ahora</span></div></div>
<div class="acts2"><span class="btn pr">Nuevo workspace</span><span class="btn">Desde una conversación…</span></div></div>""")),
        ("B · Tarjetas sobrias", "Tarjetas sin color de fondo, con las pestañas como texto gris. Más visual, ocupa más espacio.",
         app("""<div class="home"><h3>Workspaces</h3><p>Retoma uno o crea uno nuevo.</p><div class="cards">
<div class="card"><div class="ch"><b>Backend KYC</b><span class="d i"></span></div><div class="cm">4 pestañas · ahora</div><div class="ct">bug-185 · front · infra · revisiones</div></div>
<div class="card"><div class="ch"><b>Revisión 291</b><span class="d b"></span></div><div class="cm">2 trabajaban al apagar · ayer</div><div class="ct">plan · tests</div></div>
<div class="card"><div class="ch"><b>Manual KYC</b><span class="d z"></span></div><div class="cm">apagado · 12 oct</div><div class="ct">manual</div></div>
<div class="card new">+ Nuevo workspace</div></div></div>""")),
        ("C · Lista y detalle", "A la izquierda la lista; a la derecha el workspace elegido con sus pestañas y sesiones antes de abrirlo.",
         app("""<div class="split"><div class="sl"><div class="grp"><span>Workspaces</span><span>+</span></div>
<div class="t on"><span class="n">Backend KYC</span><span class="d i"></span></div><div class="t"><span class="n">Revisión 291</span><span class="d b"></span></div>
<div class="t"><span class="n">Manual KYC</span><span class="d z"></span></div><div class="t"><span class="n">frontend-301</span><span class="warnc">term.</span></div></div>
<div class="sr"><h3>Backend KYC</h3><p>abierto · 4 pestañas · 7 sesiones · ~/work/backend</p>
<div class="kv"><span>bug-185</span><span><span class="d b"></span> fix-matriz · <span class="d b"></span> review-291 · <span class="d i"></span> tests</span></div>
<div class="kv"><span>front</span><span><span class="d w"></span> arb-strings</span></div><div class="kv"><span>infra</span><span><span class="d z"></span> 3 suspendidas</span></div>
<div class="acts2"><span class="btn pr">Abrir</span><span class="btn">Renombrar</span><span class="btn">Apagar</span></div></div></div>"""))])

# -- D2 una sesión
screen("d-sesion", "Workspace · una sesión", "desktop",
       "La pantalla de todos los días: una pestaña con una sesión de Claude a la vista.",
       [("A · Barra lateral + sesiones arriba", "Pestañas a la izquierda; las sesiones de la pestaña como pestañas de navegador arriba; línea de estado abajo.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>' + STATUS))),
        ("B · Riel de iconos", "La barra lateral se reduce a un riel con la inicial de cada pestaña y su punto de estado; se expande al pasar el ratón. Más espacio para la terminal.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>' + STATUS,
                 side="""<aside class="rail"><div class="mark">≡</div><div class="ri on">B<span class="d b"></span></div><div class="ri">F<span class="d w"></span></div><div class="ri">I<span class="d z"></span></div><div class="ri">R<span class="d i"></span></div><div class="ri add">+</div></aside>""", cols="52px 1fr"))),
        ("C · Sesiones en la barra lateral", "Árbol: cada pestaña desplegable con sus sesiones debajo. Arriba solo el título de la sesión. Todo se ve de un vistazo.",
         app(win(f'<div class="bar"><span class="crumb">bug-185 <span class="dimc">/</span> <b>fix-matriz</b> <span class="al">yolo</span></span><span class="path">~/work/backend</span></div><div class="stage">{TERM}</div>' + STATUS,
                 side="""<aside class="side"><div class="wsrow"><div class="mark">≡</div><b>Backend KYC</b><span>⌄</span></div>
<div class="t on"><span class="n">▾ bug-185</span></div><div class="t sub on2"><span class="d b"></span><span class="n">fix-matriz</span></div><div class="t sub"><span class="d b"></span><span class="n">review-291</span></div><div class="t sub"><span class="d i"></span><span class="n">tests</span></div><div class="t sub"><span class="dimc">❯</span><span class="n">terminal</span></div>
<div class="t"><span class="n">▸ front</span><span class="cnt"><i><span class="d w"></span>1</i></span></div><div class="t"><span class="n">▸ infra</span><span class="cnt"><i><span class="d z"></span>3</i></span></div>
<div class="foot"><span class="ib">⚙</span><span class="ib" style="margin-left:auto">⏻</span></div></aside>""", cols="220px 1fr")))])

# -- D3 combinada
TWO = """<div class="pane focus"><div class="ph"><span class="d b"></span><b>fix-matriz</b> yolo<span class="acts">⏾ ⋯</span></div><div class="term"><div class="u">❯ arregla el filtro de acentos</div><div class="sp"><span class="a">●</span> Corrí los tests: 42 passed.</div><div class="prompt">&gt;</div></div></div>
<div class="pane"><div class="ph"><span class="d b"></span><b>review-291</b> claude-2<span class="acts">⏾ ⋯</span></div><div class="term"><div class="u">❯ revisa el plan de la 291</div><div class="sp"><span class="a">●</span> Encontré 3 riesgos en el plan…</div><div class="prompt">&gt;</div></div></div>"""
PILLS2 = '<div class="bar"><span class="p on"><span class="d b"></span>fix-matriz <span class="al">+</span> <span class="d b"></span>review-291<span class="x">✕</span></span><span class="p"><span class="d i"></span>tests</span><span class="plus">+</span></div>'
screen("d-combinada", "Vista combinada", "desktop", "Dos o más sesiones a la vez en la misma vista.",
       [("A · Columnas con encabezado", "Cada columna tiene una línea con nombre, alias y acciones; la enfocada se subraya con el acento.",
         app(win(PILLS2 + f'<div class="stage two">{TWO}</div>'))),
        ("B · Sin encabezados", "Las columnas van limpias; el nombre aparece en una etiqueta flotante en la esquina, y la enfocada tiene el fondo un tono más claro.",
         app(win(PILLS2 + """<div class="stage two"><div class="pane focus2"><span class="tag2">fix-matriz</span><div class="term"><div class="u">❯ arregla el filtro de acentos</div><div class="sp"><span class="a">●</span> Corrí los tests: 42 passed.</div><div class="prompt">&gt;</div></div></div>
<div class="pane"><span class="tag2">review-291</span><div class="term"><div class="u">❯ revisa el plan de la 291</div><div class="sp"><span class="a">●</span> Encontré 3 riesgos…</div><div class="prompt">&gt;</div></div></div></div>"""))),
        ("C · Foco y tira", "Una sesión grande y las demás como miniaturas en una tira a la derecha; clic en una miniatura la trae al centro.",
         app(win(PILLS2 + """<div class="stage focusgrid"><div class="pane focus"><div class="ph"><span class="d b"></span><b>fix-matriz</b> yolo</div><div class="term"><div class="u">❯ arregla el filtro de acentos</div><div class="sp"><span class="a">●</span> Corrí los tests: 42 passed.</div><div class="prompt">&gt;</div></div></div>
<div class="strip"><div class="mini"><b><span class="d b"></span> review-291</b><small>Encontré 3 riesgos…</small></div><div class="mini"><b><span class="d i"></span> tests</b><small>42 passed</small></div></div></div>""")))])

# -- D4 pestaña vacía
screen("d-vacia", "Pestaña vacía", "desktop", "Una pestaña recién creada, sin sesiones.",
       [("A · Centrado con recientes", "Tres acciones y la lista de conversaciones recientes de la carpeta.",
         app(win('<div class="bar"><span class="path">~/work/backend</span></div><div class="empty"><div class="ec"><h3>nueva</h3><p>~/work/backend</p><div class="acts2"><span class="btn pr">Nueva sesión de Claude</span><span class="btn">Terminal</span><span class="btn">Retomar por ID</span></div><div class="grp"><span>Recientes en esta carpeta</span></div><div class="kv"><span>fix-matriz</span><span>hace 5 min · «arregla el filtro…»</span></div><div class="kv"><span>review-291</span><span>ayer · «revisa el plan de la 291»</span></div></div></div>'))),
        ("B · Lista a pantalla completa", "Como una paleta: un buscador arriba y la lista de conversaciones; Enter abre la elegida, o «Nueva» arriba.",
         app(win('<div class="bar"><span class="path">~/work/backend</span></div><div class="palette"><div class="in f">Buscar conversación o escribe un nombre para una sesión nueva…</div><div class="pi hl"><b>+ Nueva sesión de Claude</b><span>Enter</span></div><div class="pi"><b>❯ Terminal</b><span></span></div><div class="grp"><span>Recientes</span></div><div class="pi"><b>fix-matriz</b><span>hace 5 min · yolo</span></div><div class="pi"><b>review-291</b><span>ayer · claude-2</span></div><div class="pi"><b>sin nombre</b><span>12 oct</span></div></div>'))),
        ("C · Tres tarjetas grandes", "Claude, Terminal y Retomar como tres botones grandes; los recientes debajo, más pequeños.",
         app(win('<div class="bar"><span class="path">~/work/backend</span></div><div class="empty"><div class="ec"><div class="big3"><div><b>✦</b>Claude</div><div><b>❯</b>Terminal</div><div><b>↻</b>Retomar</div></div><div class="grp"><span>Recientes</span></div><div class="kv"><span>fix-matriz</span><span>hace 5 min</span></div></div></div>')))])

# -- D5 menú contextual
screen("d-menu", "Menú contextual de una sesión", "desktop", "Clic derecho en una sesión (o en ⋯).",
       [("A · Lista plana con secciones", "Todo a la vista, agrupado por separadores; lo destructivo al final en rojo.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra="""<div class="menu" style="left:300px;top:60px"><div class="h"><span>fix-matriz</span><span>✕</span></div><div class="it">Renombrar</div><div class="it">Editar sesión…</div><div class="it">Cambiar alias <span class="k">yolo</span></div><div class="it">Suspender</div><div class="sep"></div><div class="it hl">Ver junto a review-291</div><div class="it">Distribución <span class="k">columnas</span></div><div class="it">Mover a la pestaña…</div><div class="sep"></div><div class="it dg">Cerrar <span class="k">Ctrl W</span></div></div>""")),
        ("B · Con submenús", "Lista corta; «Ver junto a», «Distribución» y «Mover a» abren un submenú a la derecha.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra="""<div class="menu" style="left:300px;top:60px;width:220px"><div class="it">Renombrar</div><div class="it">Cambiar alias <span class="k">›</span></div><div class="it">Suspender</div><div class="sep"></div><div class="it hl">Ver junto a <span class="k">›</span></div><div class="it">Mover a <span class="k">›</span></div><div class="sep"></div><div class="it dg">Cerrar</div></div><div class="menu" style="left:524px;top:150px;width:190px"><div class="it">review-291</div><div class="it hl">tests</div><div class="it">terminal</div></div>""")),
        ("C · Paleta de comandos", "Ctrl+K abre una paleta con búsqueda que reemplaza a los menús: se escribe «sus» y aparece «Suspender fix-matriz».",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra="""<div class="cmdk"><div class="in f">sus</div><div class="pi hl"><b>Suspender fix-matriz</b><span>sesión</span></div><div class="pi"><b>Suspender la pestaña bug-185</b><span>pestaña</span></div><div class="pi"><b>Reanudar infra</b><span>pestaña</span></div></div>"""))])

# -- D6 menú del workspace
screen("d-wsmenu", "Menú del workspace", "desktop", "Clic en el nombre del workspace (Alt+M).",
       [("A · Desplegable bajo el nombre", "Lista corta con las acciones del workspace y los otros workspaces abiertos.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra="""<div class="menu" style="left:14px;top:46px"><div class="it">Todos los workspaces</div><div class="it">Renombrar…</div><div class="it">Carpeta por defecto <span class="k">~/work/backend</span></div><div class="it">Guardar ahora</div><div class="it">Reabrir sesión cerrada <span class="k">Ctrl ⇧ T</span></div><div class="sep"></div><div class="it">Revisión 291 <span class="k">abierto</span></div><div class="sep"></div><div class="it dg">Apagar y guardar</div></div>""")),
        ("B · Selector de workspaces", "El nombre funciona como un selector: la parte de arriba cambia de workspace, la de abajo actúa sobre el actual.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra="""<div class="menu" style="left:14px;top:46px;width:280px"><div class="in f" style="margin:4px 4px 6px">Buscar workspace…</div><div class="it hl"><span class="d i"></span> Backend KYC <span class="k">✓</span></div><div class="it"><span class="d b"></span> Revisión 291</div><div class="it"><span class="d z"></span> Manual KYC</div><div class="sep"></div><div class="it">Ajustes del workspace…</div><div class="it dg">Apagar</div></div>"""))])

# -- D7 nueva sesión
DLG = """<div class="dlg"><div class="hd">Nueva sesión <span>✕</span></div><div class="bd">
<div class="seg"><span class="on">Claude</span><span>Terminal</span><span>Retomar por ID</span></div>
<div><label>Nombre</label><div class="in f">fix-matriz</div></div>
<div><label>Alias de Claude</label><div class="seg"><span>claude</span><span class="on">yolo</span><span>claude-2</span></div></div>
<div><label>Argumentos</label><div class="in dimc">--model opus</div></div><div class="cmd">yolo --session-id 7c5d… --name fix-matriz</div></div>
<div class="ft"><span class="btn">Cancelar</span><span class="btn pr">Crear</span></div></div>"""
screen("d-nueva", "Nueva sesión", "desktop", "Ctrl+T o «+» en la barra de sesiones.",
       [("A · Diálogo centrado", "Modal con segmentos Claude / Terminal / Retomar, el alias como segmentos y la vista previa del comando.",
         app(f'<div class="scrim">{DLG}</div>', h=420)),
        ("B · Panel lateral", "Se abre desde la derecha sin tapar la sesión actual; útil para comparar mientras se configura.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>'), extra=f'<div class="drawer">{DLG}</div>')),
        ("C · En línea, como pestaña nueva", "«+» crea una pestaña «nueva» y el formulario aparece en el área de la terminal; Enter la arranca.",
         app(win('<div class="bar"><span class="p"><span class="d b"></span>fix-matriz</span><span class="p on">nueva<span class="x">✕</span></span><span class="plus">+</span></div><div class="empty"><div class="ec" style="max-width:420px"><div class="in f">nombre de la sesión</div><div class="seg" style="margin-top:10px"><span>claude</span><span class="on">yolo</span><span>claude-2</span></div><div class="cmd" style="margin-top:10px">Enter para crear · Tab para opciones</div></div></div>')))])

# -- D8 nueva pestaña
screen("d-pestana", "Nueva pestaña", "desktop", "«+» en la barra lateral (Alt+T).",
       [("A · Diálogo", "Nombre, carpeta con sugerencias y con qué empieza.",
         app("""<div class="scrim"><div class="dlg"><div class="hd">Nueva pestaña <span>✕</span></div><div class="bd"><div><label>Nombre</label><div class="in f">bug-301</div></div><div><label>Carpeta</label><div class="in">~/work/backend-wt-301</div><div class="sug"><div class="hl">~/work/backend-wt-301 <small>reciente</small></div><div>~/work/backend</div></div></div><div><label>Empezar con</label><div class="seg"><span class="on">Claude</span><span>Terminal</span><span>Vacía</span></div></div></div><div class="ft"><span class="btn">Cancelar</span><span class="btn pr">Crear</span></div></div></div>""")),
        ("B · En la barra lateral", "Aparece una fila editable en la lista de pestañas; al escribir la carpeta salen sugerencias debajo. Sin modal.",
         app(win(PILLS + f'<div class="stage">{TERM}</div>', side="""<aside class="side"><div class="wsrow"><div class="mark">≡</div><b>Backend KYC</b></div><div class="grp"><span>Pestañas</span><span>+</span></div><div class="t"><span class="n">bug-185</span></div><div class="t"><span class="n">front</span></div><div class="t on newtab"><div class="in f" style="width:100%">bug-301</div></div><div class="sug" style="margin:0 8px"><div class="hl">~/work/backend-wt-301</div><div>~/work/backend</div></div></aside>""")))])

# -- D9 editar sesión
screen("d-editar", "Editar sesión", "desktop", "Nombre, alias y argumentos de una sesión existente.",
       [("A · Diálogo", "Igual que Nueva sesión, con «Guardar» y «Guardar y reiniciar».",
         app("""<div class="scrim"><div class="dlg"><div class="hd">Editar fix-matriz <span>✕</span></div><div class="bd"><div><label>Nombre</label><div class="in f">fix-matriz</div></div><div><label>Alias</label><div class="seg"><span>claude</span><span class="on">yolo</span><span>claude-2</span></div></div><div><label>Argumentos</label><div class="in">--model opus</div></div><div class="note2">Cambiar el alias la reinicia; está trabajando: recibirá «continue».</div></div><div class="ft"><span class="btn">Cancelar</span><span class="btn">Guardar y reiniciar</span><span class="btn pr">Guardar</span></div></div></div>""")),
        ("B · Inspector lateral", "Panel fijo a la derecha con las propiedades de la sesión enfocada; los cambios se aplican al salir del campo.",
         app(win(PILLS + f'<div class="stage withinsp">{TERM}<div class="insp"><div class="grp"><span>Sesión</span><span>✕</span></div><label>Nombre</label><div class="in">fix-matriz</div><label>Alias</label><div class="in">yolo ⌄</div><label>Argumentos</label><div class="in">--model opus</div><label>Conversación</label><div class="cmd">7c5d0e1a…</div><label>Carpeta</label><div class="cmd">~/work/backend</div><div class="acts2"><span class="btn">Reiniciar</span><span class="btn">Suspender</span></div></div></div>')))])

# -- D10 cambiar alias
screen("d-alias", "Cambiar alias de varias sesiones", "desktop", "De una vista, una pestaña o todo el workspace.",
       [("A · Diálogo con el impacto", "Elige el alias y muestra qué pasa con cada sesión antes de aplicarlo.",
         app("""<div class="scrim"><div class="dlg"><div class="hd">Alias de la pestaña bug-185 <span>✕</span></div><div class="bd"><div class="seg"><span>claude</span><span>yolo</span><span class="on">claude-2-yolo</span></div><label class="chk"><i>✓</i> Mandar «continue» a las que estaban trabajando</label><div class="cmd">fix-matriz → claude-2-yolo · se reinicia y continúa<br>review-291 → claude-2-yolo · se reinicia y continúa<br>tests → claude-2-yolo · se reinicia</div></div><div class="ft"><span class="btn">Cancelar</span><span class="btn pr">Cambiar 3 sesiones</span></div></div></div>""")),
        ("B · Selección múltiple", "Se marcan sesiones en la barra (Ctrl+clic) y aparece una barra de acciones abajo: alias, suspender, cerrar.",
         app(win('<div class="bar"><span class="p on">☑ fix-matriz</span><span class="p on">☑ review-291</span><span class="p">☐ tests</span></div>' + f'<div class="stage">{TERM}</div><div class="bulk">2 seleccionadas<span class="btn">Alias ⌄</span><span class="btn">Suspender</span><span class="btn">Ver juntas</span><span class="btn dgb">Cerrar</span></div>')))])

# -- D11 confirmación
screen("d-confirm", "Confirmar cerrar / apagar", "desktop", "Acciones que cierran sesiones.",
       [("A · Modal de confirmación", "Pregunta, explicación breve y botón rojo; Enter confirma.",
         app("""<div class="scrim"><div class="dlg" style="width:380px"><div class="hd">¿Cerrar fix-matriz? <span>✕</span></div><div class="bd"><p class="mutedp">Claude se cierra; la conversación queda en disco y puedes reabrirla con Ctrl+Shift+T.</p></div><div class="ft"><span class="btn">Cancelar</span><span class="btn dgb">Cerrar</span></div></div></div>""")),
        ("B · Sin preguntar, con deshacer", "Cierra al instante y muestra un aviso abajo con «Deshacer» durante unos segundos (reabre la sesión en su conversación).",
         app(win('<div class="bar"><span class="p on"><span class="d b"></span>review-291</span><span class="p"><span class="d i"></span>tests</span></div>' + f'<div class="stage">{TERM}</div>'), extra='<div class="toast2">fix-matriz cerrada <span class="btn">Deshacer</span><span class="dimc">Ctrl ⇧ T</span></div>'))])

# -- D12 configuración
screen("d-config", "Configuración", "desktop", "Ajustes generales, alias de Claude, apariencia y atajos.",
       [("A · Modal con secciones", "Navegación a la izquierda y ajustes en filas con su explicación.",
         app("""<div class="scrim"><div class="settings"><nav><b>Ajustes</b><div class="on">General</div><div>Alias de Claude</div><div>Apariencia</div><div>Atajos</div></nav><section><h4>General</h4><div class="srow"><div>Mensaje de continuación<small>Lo reciben las que estaban trabajando.</small></div><div class="in" style="width:150px">continue</div></div><div class="srow"><div>Atajos de navegador<small>Ctrl+T, Ctrl+W, Ctrl+Tab…</small></div><span class="tog on"><i></i></span></div><div class="srow"><div>Arranque diferido<small>Las ociosas esperan a Enter.</small></div><span class="tog"><i></i></span></div><div class="srow"><div>Autoguardado</div><div class="seg"><span>1 min</span><span class="on">2 min</span><span>5 min</span></div></div></section></div></div>""")),
        ("B · Página completa", "Los ajustes reemplazan la vista del workspace (con «← Volver»), en una sola columna larga con encabezados.",
         app("""<div class="page2"><div class="dimc">← Volver a Backend KYC</div><h3>Ajustes</h3><div class="grp"><span>General</span></div><div class="srow"><div>Mensaje de continuación</div><div class="in" style="width:160px">continue</div></div><div class="srow"><div>Atajos de navegador</div><span class="tog on"><i></i></span></div><div class="grp"><span>Alias de Claude</span></div><div class="srow"><div>yolo <small>CLAUDE_CONFIG_DIR=… claude --dangerously-skip…</small></div><span class="btn">Editar</span></div><div class="srow"><div>claude-2 <small>CLAUDE_CONFIG_DIR=~/.claude-2 claude</small></div><span class="btn">Editar</span></div></div>"""))])

# -- D13 estados
screen("d-estados", "Sesión suspendida / con error", "desktop", "Cómo se ve una sesión que no está corriendo.",
       [("A · Franja arriba del panel", "La terminal sigue visible (atenuada) y una franja ofrece la acción: Reanudar, o Reabrir si salió con error.",
         app(win(PILLS + '<div class="stage"><div class="pane"><div class="banner"><span class="d z"></span> fix-matriz está suspendida: no corre Claude.<span class="btn pr">Reanudar</span></div><div class="term faded">' + TERM[18:] + '</div></div></div>'))),
        ("B · Tarjeta centrada", "La terminal se oculta y se muestra una tarjeta con el estado, la última actividad y el botón.",
         app(win(PILLS + '<div class="stage"><div class="empty"><div class="ec" style="max-width:380px;text-align:center"><div style="font-size:22px" class="dimc">⏾</div><h3>fix-matriz está suspendida</h3><p>Su conversación sigue guardada. Última actividad: hace 2 h.</p><div class="acts2" style="justify-content:center"><span class="btn pr">Reanudar</span><span class="btn">Cerrar</span></div></div></div></div>')))])

# -- D14 nuevo workspace / desde ID
screen("d-nuevows", "Nuevo workspace / desde una conversación", "desktop", "Crear un workspace en una carpeta o alrededor de una conversación existente.",
       [("A · Diálogo con dos modos", "Un solo diálogo: «En una carpeta» o «Desde una conversación» (pegando su ID, con la búsqueda en vivo).",
         app("""<div class="scrim"><div class="dlg"><div class="hd">Nuevo workspace <span>✕</span></div><div class="bd"><div class="seg"><span>En una carpeta</span><span class="on">Desde una conversación</span></div><div><label>ID de la conversación</label><div class="in f">7c5d0e</div></div><div class="pi hl"><b>fix-matriz</b><span>hace 5 min · ~/work/backend</span></div><div class="cmd">«arregla el filtro de acentos de la matriz de riesgo»</div></div><div class="ft"><span class="btn">Cancelar</span><span class="btn pr">Crear workspace</span></div></div></div>""")),
        ("B · Fila en la lista de inicio", "Arriba de la lista de workspaces hay un campo único: una ruta crea en esa carpeta, un ID crea desde la conversación.",
         app("""<div class="home"><h3>Workspaces</h3><div class="in f" style="max-width:760px">~/work/backend-wt-301 <span class="dimc">  ↵ crear aquí · o pega el ID de una conversación</span></div><div class="list" style="margin-top:14px"><div class="row"><b>Backend KYC</b><span class="st"><span class="d i"></span>abierto</span><span>4 pestañas</span><span>ahora</span></div><div class="row"><b>Revisión 291</b><span class="st"><span class="d b"></span>2 trabajaban</span><span>2 pestañas</span><span>ayer</span></div></div></div>"""))])

# ================================================================= TERMINAL
BASE = bar2() + CLAUDE + [L("")] * 3 + [hints("Ctrl+T nueva · Ctrl+W cerrar · Ctrl+PgUp/PgDn cambiar · clic derecho: acciones · Alt+m menú · Alt+h ayuda")]

# -- T1 launcher
def launcher_a():
    rows = [L(""), L(("  cw", "fg b"), ("  workspaces", "mu")), L(""),
            L(("    ", ""), ("NOMBRE                ESTADO          PESTAÑAS   ÚLTIMO USO", "dm")),
            L(("  ▸ ", "ac"), ("Backend KYC           ", "sel b"), ("● abierto      ", "ok"), ("  4          ahora", "mu")),
            L(("    Revisión 291          ", "fg"), ("● 2 trabajaban ", "bu"), ("  2          ayer", "mu")),
            L(("    Manual KYC            ", "fg"), ("○ apagado      ", "dm"), ("  1          12 oct", "mu")),
            L(""), L(("    + Nuevo workspace aquí  ", "mu"), ("~/work/backend", "dm")),
            L(("    ↻ Desde una conversación…", "mu")), L(""), L(""),
            hints("↑↓ elegir · Enter abrir · n nuevo · i por ID · r renombrar · x borrar · q salir")]
    return term(rows, title="cw")
def launcher_b():
    left = [L(("  WORKSPACES", "dm")), L(""), L(("▸ Backend KYC", "sel b")), L(("  Revisión 291", "fg")), L(("  Manual KYC", "fg")), L(""), L(("  + nuevo aquí", "mu")), L(("  ↻ por ID", "mu"))]
    right = [L(("Backend KYC", "fg b"), ("  abierto · ~/work/backend", "mu")), L(""),
             L(("bug-185  ", "fg"), ("● fix-matriz  ● review-291  ○ tests", "mu")),
             L(("front    ", "fg"), ("? arb-strings", "mu")), L(("infra    ", "fg"), ("z 3 suspendidas", "dm")), L(""),
             L(("Enter abrir · r renombrar · s apagar", "dm"))]
    rows = [L("")]
    for i in range(max(len(left), len(right))):
        a = Line(*(left[i].segs if i < len(left) else [])).pad(26)
        b = right[i] if i < len(right) else L("")
        rows.append(L(("  ", ""), *a.segs, ("│ ", "dm"), *b.segs))
    rows += [L("")] * 3 + [hints("↑↓ elegir · Enter abrir · q salir")]
    return term(rows, title="cw")
screen("t-launcher", "Inicio (comando cw)", "terminal", "Lo que aparece al escribir cw en una terminal.",
       [("A · Lista con columnas", "Tabla alineada; la fila elegida con fondo gris; acciones con una letra en la línea de abajo.", launcher_a()),
        ("B · Lista y detalle", "Lista a la izquierda y, a la derecha, las pestañas y sesiones del workspace elegido.", launcher_b())])

# -- T2 barra principal
def bar_one_line():
    l1 = L((" ≡ ", "mu"), ("bug-173", "fg b"), (" › ", "dm"), ("fix-matriz", "sel b"), (" ●", "bu"), ("  review-291 ●  tests ○  ❯ terminal  +", "mu"))
    l1.pad(80); l1.segs.append(("backend  bug-226 ●1  infra z2   ? ⏻", "dm"))
    return [l1, L(("─" * 110, "dm"))]
def bar_bottom():
    return [L(("─" * 110, "dm")),
            L((" bug-173 ", "sel b"), ("●1", "bu"), ("  backend  bug-226 ", "mu"), ("●1", "bu"), ("  infra z2  +", "mu"), ("    │    ", "dm"), ("fix-matriz ●", "fg b"), ("  review-291 ●  tests ○  +", "mu"))]
screen("t-barra", "Barra principal", "terminal", "Pestañas, sesiones y estado, siempre a la vista.",
       [("A · Dos líneas arriba + atajos abajo", "Pestañas en la primera línea, sesiones de la pestaña en la segunda, y abajo los atajos según el contexto.",
         term(BASE, title="Windows Terminal")),
        ("B · Una sola línea", "Pestaña › sesiones en una línea; las otras pestañas resumidas a la derecha. Máximo espacio para Claude.",
         term(bar_one_line() + CLAUDE + [L("")] * 5, title="Windows Terminal")),
        ("C · Abajo, como la barra de vim", "Todo en una línea al pie, como statusline; arriba queda la sesión desde la primera línea.",
         term(CLAUDE + [L("")] * 5 + bar_bottom(), title="Windows Terminal"))])

# -- T3 combinada
def split_view(style):
    left = ["❯ arregla el filtro de acentos", "", "● Corrí los tests: 42 passed.", "", "✻ Pensando…"]
    right = ["❯ revisa el plan de la 291", "", "● Encontré 3 riesgos en el plan…", "", "◯ 2 agentes en segundo plano"]
    rows = bar2()
    if style == "a":
        rows.append(L(("── fix-matriz ", "ac"), ("yolo ", "dm"), ("─" * 39, "ac"), ("┬", "dm"), ("── review-291 ", "mu"), ("claude-2 ", "dm"), ("─" * 41, "dm")))
    else:
        rows.append(L((" fix-matriz", "fg b"), (" yolo", "dm"), (" " * 39), ("│", "dm"), (" review-291", "mu"), (" claude-2", "dm")))
    for a, b in zip(left, right):
        rows.append(L(Line((a, "fg")).pad(54).segs[0], (" " * (54 - w(a))), ("│ ", "dm"), (b, "fg")))
    rows += [L((" " * 54), ("│", "dm"))] * 6
    rows.append(hints("Alt+←/→ cambiar de panel · clic derecho: separar, distribución"))
    return term(rows, title="Windows Terminal")
screen("t-combinada", "Vista combinada", "terminal", "Dos sesiones en la misma vista (paneles de tmux).",
       [("A · Título sobre el borde", "La línea de arriba de cada panel lleva su nombre; la del panel enfocado en el color del acento.", split_view("a")),
        ("B · Título en línea, sin bordes", "Solo un separador vertical; el nombre en negrita en la primera línea de cada panel.", split_view("b"))])

# -- T4 pestaña vacía
def empty_a():
    rows = bar2("nueva")[:1] + [L(("─" * 110, "dm")), L(""),
            L(("     nueva", "fg b"), ("  ~/work/backend", "dm")), L(""),
            L(("     ", ""), (" Nueva sesión de Claude ", "sel"), ("   Terminal   Retomar por ID", "mu")), L(""),
            L(("     RECIENTES EN ESTA CARPETA", "dm")),
            L(("     fix-matriz            ", "fg"), ("hace 5 min · yolo     ", "dm"), ("«arregla el filtro de acentos de la matriz…»", "mu")),
            L(("     review-291            ", "fg"), ("ayer · claude-2       ", "dm"), ("«revisa el plan de la 291»", "mu")),
            L("")] + [L("")] * 6 + [hints("↑↓ elegir · Enter abrir · c Claude · t terminal · i por ID")]
    return term(rows, title="Windows Terminal")
def empty_b():
    rows = bar2("nueva")[:1] + [L(("─" * 110, "dm")), L(""),
            L(("     ¿Qué abrimos en ", "mu"), ("~/work/backend", "fg"), ("?", "mu")), L(""),
            L(("     1 ", "ac"), ("Nueva sesión de Claude", "fg")),
            L(("     2 ", "ac"), ("Terminal", "fg")),
            L(("     3 ", "ac"), ("Retomar fix-matriz", "fg"), ("     hace 5 min", "dm")),
            L(("     4 ", "ac"), ("Retomar review-291", "fg"), ("     ayer", "dm")),
            L(("     5 ", "ac"), ("Retomar por ID…", "fg"))] + [L("")] * 7 + [hints("1–5 o clic para elegir")]
    return term(rows, title="Windows Terminal")
screen("t-vacia", "Pestaña vacía", "terminal", "Una pestaña sin sesiones.",
       [("A · Botones y recientes", "Igual que la app: tres acciones y la lista de conversaciones de la carpeta.", empty_a()),
        ("B · Menú numerado", "Una lista numerada; se elige con un número. Lo más rápido con teclado.", empty_b())])

# -- T5 menú contextual
def ctx_items(width):
    rows = [L((" fix-matriz", "mu")), L(""), L((" Renombrar", "fg")), L((" Editar sesión…", "fg")),
            Line((" Cambiar alias de Claude", "fg")).pad(width - 8), L((" Suspender", "fg")),
            L((" " + "─" * (width - 4), "dm")), L((" Ver junto a review-291", "sel")).pad(width - 2, "sel"),
            L((" Distribución", "fg")), L((" Separar de la vista", "fg")), L((" " + "─" * (width - 4), "dm")),
            L((" Cerrar", "wa"))]
    rows[4].segs.append(("yolo", "dm"))
    rows[-1].pad(width - 9); rows[-1].segs.append(("Ctrl W", "dm"))
    return rows
def ctx_a():
    b = box(ctx_items(44), 44, "Acciones")
    return term(overlay(BASE, b, 3, 30), title="Windows Terminal")
def ctx_b():
    items = [L((" Renombrar", "fg")), L((" Cambiar alias", "fg")), L((" Suspender", "fg")), L((" Ver junto a… ›", "sel")).pad(28, "sel"),
             L((" Distribución ›", "fg")), L((" Cerrar", "wa"))]
    b = box(items, 30, "", close=False, border="mu")
    return term(overlay(BASE, b, 4, 8, dim=False), title="Windows Terminal")
screen("t-menu", "Menú contextual", "terminal", "Clic derecho en una sesión o en la barra.",
       [("A · Panel con fondo atenuado", "Ocupa toda la pantalla con el fondo atenuado; un clic afuera lo cierra; ✕ en la esquina. Se queda abierto entre acciones.", ctx_a()),
        ("B · Menú nativo compacto junto al cursor", "Menú pequeño de tmux donde se hizo clic, sin atenuar nada; se cierra al elegir. Más ligero, pero no se queda abierto.", ctx_b())])

# -- T6 menú principal
def mainmenu_a():
    items = [L((" CREAR", "dm")), L((" Sesión de Claude", "fg")), L((" Terminal", "fg")), L((" Pestaña", "fg")), L((" Reabrir sesión cerrada", "fg")),
             L((" ESTA PESTAÑA", "dm")), L((" Renombrar", "fg")), L((" Suspender o reanudar", "fg")), L((" Carpeta…", "fg")),
             L((" WORKSPACE", "dm")), L((" Guardar ahora", "sel")).pad(38, "sel"), L((" Configuración", "fg")), L((" Apagar y guardar", "wa"))]
    return term(overlay(BASE, box(items, 40, "Backend KYC"), 2, 35), title="Windows Terminal")
def mainmenu_b():
    items = [L((" > sus", "fg")), L((" " + "─" * 46, "dm")), L((" Suspender fix-matriz", "sel")).pad(48, "sel"),
             L((" Suspender la pestaña bug-173", "fg")), L((" Reanudar infra", "fg")), L(""), L((" Escribe para buscar · Enter ejecuta", "dm"))]
    return term(overlay(BASE, box(items, 50, "Comandos"), 4, 30), title="Windows Terminal")
screen("t-menuprincipal", "Menú principal (Alt+m)", "terminal", "Todas las acciones del workspace.",
       [("A · Menú agrupado", "Secciones Crear / Esta pestaña / Workspace, más corto que hoy (lo de una sesión va en su clic derecho).", mainmenu_a()),
        ("B · Paleta con búsqueda", "Se escribe y se filtran todas las acciones; Enter ejecuta. Sirve para todo sin recordar dónde está cada cosa.", mainmenu_b())])

# -- T7 nueva sesión
def newsess(style):
    if style == "a":
        rows = [L(""), L(("  ", ""), (" Claude ", "sel"), ("  Terminal   Por ID", "mu")), L(""),
                L(("  Nombre      ", "mu"), Line((" fix-matriz▏", "acb")).pad(36, "acb").segs[0], ("", "")),
                L(("  Alias       ", "mu"), ("‹ ", "dm"), ("yolo", "fg"), (" ›  ", "dm"), ("claude · claude-2", "dm")),
                L(("  Argumentos  ", "mu"), (" " * 36, "sel")), L(""),
                L(("  yolo --session-id 7c5d… --name fix-matriz", "dm")), L("")]
        last = L((" " * 34), ("Cancelar", "mu"), ("   ", ""), (" Crear ", "sel b"))
        rows.append(last)
        rows[3] = L(("  Nombre      ", "mu"), *Line((" fix-matriz▏", "acb")).pad(36, "acb").segs)
        return term(overlay(BASE, box(rows, 60, "Nueva sesión"), 3, 25), title="Windows Terminal")
    rows = [L(""), L(("  Paso 1 de 2 · ", "dm"), ("Nombre", "fg b")), L(""),
            L(("  ", ""), *Line((" fix-matriz▏", "acb")).pad(40, "acb").segs), L(""),
            L(("  Sugerencias: ", "dm"), ("fix-matriz-2", "mu"), (" · ", "dm"), ("review-291", "mu")), L(""),
            L(("  Enter sigue al alias · Esc cancela", "dm"))]
    return term(overlay(BASE, box(rows, 60, "Nueva sesión"), 4, 25), title="Windows Terminal")
screen("t-nueva", "Nueva sesión", "terminal", "Ctrl+T o «+ Claude».",
       [("A · Formulario en una caja", "Tipo de sesión, nombre, alias (‹ ›), argumentos y la vista previa; un solo botón principal.", newsess("a")),
        ("B · Paso a paso", "Pregunta una cosa a la vez (nombre → alias); Enter avanza. Menos que leer, más pasos.", newsess("b"))])

# -- T8 nueva pestaña
def newtab(style):
    rows = [L(""), L(("  Nombre   ", "mu"), *Line((" bug-301▏", "acb")).pad(40, "acb").segs),
            L(("  Carpeta  ", "mu"), *Line((" ~/work/backend-wt-301", "sel")).pad(40, "sel").segs)]
    if style == "a":
        rows += [L(("           ", ""), ("▸ ~/work/backend-wt-301 ", "fg"), ("reciente", "dm")), L(("             ~/work/backend", "mu")),
                 L(("             ~/work/frontend", "mu")), L(""), L(("  Empezar  ", "mu"), (" Claude ", "sel"), ("  Terminal   Vacía", "mu")), L(""),
                 L((" " * 34), ("Cancelar", "mu"), ("   ", ""), (" Crear ", "sel b"))]
    else:
        rows += [L(""), L(("  Tab completa la carpeta · Enter crea con Claude · Alt+Enter vacía", "dm"))]
    return term(overlay(BASE, box(rows, 60, "Nueva pestaña"), 3, 25), title="Windows Terminal")
screen("t-pestana", "Nueva pestaña", "terminal", "Alt+t o «+» en la barra.",
       [("A · Con sugerencias de carpeta", "Lista de carpetas recientes debajo del campo, y con qué empieza la pestaña.", newtab("a")),
        ("B · Mínimo", "Solo nombre y carpeta; Tab completa la ruta y Enter crea. Para quien escribe rápido.", newtab("b"))])

# -- T9 editar
def edit_t():
    rows = [L(""), L(("  Nombre      ", "mu"), *Line((" fix-matriz▏", "acb")).pad(36, "acb").segs),
            L(("  Alias       ", "mu"), ("‹ ", "dm"), ("claude-2-yolo", "fg"), (" ›", "dm")),
            L(("  Argumentos  ", "mu"), *Line((" --model opus", "sel")).pad(36, "sel").segs), L(""),
            L(("  [x] ", "bu"), ("Está trabajando: al cambiar el alias, mandarle «continue»", "mu")), L(""),
            L((" " * 16), ("Cancelar", "mu"), ("   ", ""), ("Guardar y reiniciar", "fg"), ("   ", ""), (" Guardar ", "sel b"))]
    return term(overlay(BASE, box(rows, 64, "Editar fix-matriz"), 4, 23), title="Windows Terminal")
def edit_t2():
    rows = [L(""), L(("  ▸ Nombre       ", "fg"), ("fix-matriz", "mu")), L(("    Alias        ", "fg"), ("claude-2-yolo", "mu")),
            L(("    Argumentos   ", "fg"), ("--model opus", "mu")), L(("    Carpeta      ", "fg"), ("~/work/backend", "mu")), L(""),
            L(("  Enter edita el campo · r reinicia · s suspende", "dm"))]
    return term(overlay(BASE, box(rows, 56, "fix-matriz"), 4, 27), title="Windows Terminal")
screen("t-editar", "Editar sesión", "terminal", "Nombre, alias y argumentos.",
       [("A · Formulario", "Los campos editables directamente y tres botones.", edit_t()),
        ("B · Lista de propiedades", "Ficha de solo lectura; Enter sobre una fila la edita. Más limpio, un paso más.", edit_t2())])

# -- T10 alias
def alias_t(style):
    if style == "a":
        rows = [L(""), L(("  ", ""), ("  claude          ", "mu")), L(("  ", ""), ("  yolo            ", "mu")), L(("  ", ""), *Line((" ▸ claude-2-yolo", "sel")).pad(18, "sel").segs, ("  CLAUDE_CONFIG_DIR=~/.claude-2 …", "dm")),
                L(""), L(("  [x] ", "bu"), ("Mandar «continue» a las que estaban trabajando", "mu")), L(""),
                L(("  fix-matriz, review-291 se reinician", "dm")), L(""), L((" " * 30), ("Cancelar", "mu"), ("   ", ""), (" Cambiar 2 ", "sel b"))]
    else:
        rows = [L(""), L(("  Alias   ", "mu"), ("‹ ", "dm"), ("claude-2-yolo", "fg b"), (" ›", "dm"), ("   3/4", "dm")), L(("          CLAUDE_CONFIG_DIR=~/.claude-2 …", "dm")), L(""),
                L(("  [x] ", "bu"), ("Mandar «continue» a las que trabajaban", "mu")), L(""), L((" " * 30), ("Cancelar", "mu"), ("   ", ""), (" Cambiar 2 ", "sel b"))]
    return term(overlay(BASE, box(rows, 62, "Alias de la vista"), 3, 24), title="Windows Terminal")
screen("t-alias", "Cambiar alias", "terminal", "De una sesión, vista, pestaña o todo.",
       [("A · Lista vertical", "Todos los alias a la vista con su definición; flechas para elegir.", alias_t("a")),
        ("B · Selector ‹ ›", "Un alias a la vez, como hoy; más compacto.", alias_t("b"))])

# -- T11 confirmación
def confirm_a():
    rows = [L(""), L(("  ¿Cerrar fix-matriz?", "fg b")), L(""), L(("  Claude se cierra; la conversación queda y", "mu")), L(("  Ctrl+Shift+T la reabre.", "mu")), L(""),
            L((" " * 22), ("Cancelar", "mu"), ("   ", ""), (" Cerrar ", "red b"))]
    return term(overlay(BASE, box(rows, 46, ""), 6, 32), title="Windows Terminal")
def confirm_b():
    rows = BASE[:-1] + [L((" ¿Cerrar fix-matriz? ", "red b"), ("  s cerrar · n cancelar   (la conversación queda; Ctrl+Shift+T la reabre)", "mu"))]
    return term(rows, title="Windows Terminal")
screen("t-confirm", "Confirmación", "terminal", "Antes de cerrar o apagar.",
       [("A · Caja pequeña", "Pregunta, explicación y botón rojo.", confirm_a()),
        ("B · En la línea de abajo", "La pregunta sustituye a la línea de atajos; s / n. No tapa nada.", confirm_b())])

# -- T12 configuración
def config_a():
    items = [L((" SESIONES", "dm")), L((" Mensaje de continuación      ", "fg"), ("«continue»", "mu")), L((" Arranque diferido            ", "fg"), ("no", "mu")),
             L((" Alias de Claude              ", "fg"), ("4 · por defecto yolo", "mu")), L((" TECLADO", "dm")), Line((" Atajos de navegador          ", "sel"), ("sí", "sel")).pad(52, "sel"),
             L((" APARIENCIA", "dm")), L((" Tema                         ", "fg"), ("gris cálido", "mu")), L((" Barra                        ", "fg"), ("arriba · 2 líneas", "mu"))]
    return term(overlay(BASE, box(items, 54, "Configuración"), 3, 28), title="Windows Terminal")
def config_b():
    rows = [L(""), L(("  ", ""), (" General ", "sel"), ("  Alias  Apariencia  Teclado", "mu")), L(("  " + "─" * 66, "dm")), L(""),
            L(("  Mensaje de continuación", "fg")), L(("  ", ""), *Line((" continue", "sel")).pad(30, "sel").segs),
            L(("  Lo reciben las sesiones que estaban trabajando al apagar o reiniciar.", "dm")), L(""),
            L(("  Arranque diferido   ", "fg"), ("[ ] ", "mu"), ("las ociosas esperan a Enter", "dm")),
            L(("  Autoguardado        ", "fg"), ("‹ 2 min ›", "mu"))]
    return term(overlay(BASE, box(rows, 72, "Configuración"), 2, 19), title="Windows Terminal")
screen("t-config", "Configuración", "terminal", "Ajustes desde la terminal.",
       [("A · Menú con valores", "Cada ajuste con su valor actual a la derecha; Enter lo cambia o abre un diálogo.", config_a()),
        ("B · Pantalla con secciones", "Pestañas General / Alias / Apariencia / Teclado, con explicación debajo de cada ajuste.", config_b())])

# -- T13 sesiones (Alt+e)
def sessions_a():
    rows = [L(""), L(("  PESTAÑA     NOMBRE              CLAUDE          ESTADO", "dm")),
            Line(("  bug-173     fix-matriz          yolo            ● trabajando", "sel")).pad(70, "sel"),
            L(("  bug-173     review-291          claude-2        ", "fg"), ("● trabajando", "bu")),
            L(("  front       arb-strings         claude          ", "fg"), ("? pide permiso", "wa")),
            L(("  infra       migraciones         claude          ", "fg"), ("z suspendida", "dm")), L(""),
            L(("  Enter ir · e editar · s suspender · x cerrar", "dm"))]
    return term(overlay(BASE, box(rows, 74, "Sesiones"), 3, 18), title="Windows Terminal")
def sessions_b():
    rows = [L(""), L(("  ▾ bug-173", "fg b"), ("  ●2", "bu")), Line(("      fix-matriz        yolo        ● trabajando", "sel")).pad(56, "sel"),
            L(("      review-291        claude-2    ", "fg"), ("●", "bu")), L(("  ▸ front", "fg b"), ("  ?1", "wa")), L(("  ▸ infra", "fg b"), ("  z3", "dm")), L(""),
            L(("  ← → plegar · Enter ir · e editar · s suspender", "dm"))]
    return term(overlay(BASE, box(rows, 60, "Sesiones"), 3, 25), title="Windows Terminal")
screen("t-sesiones", "Todas las sesiones (Alt+e)", "terminal", "Lista de todas las sesiones del workspace.",
       [("A · Tabla", "Una fila por sesión con pestaña, alias y estado.", sessions_a()),
        ("B · Árbol por pestaña", "Pestañas plegables con sus sesiones; los contadores en la fila de la pestaña.", sessions_b())])

# -- T14 ayuda
def help_a():
    rows = [L(""), L(("  MOVERSE", "dm"), (" " * 26), ("CREAR", "dm")),
            L(("  Ctrl+PgUp/PgDn ", "fg"), ("sesiones", "mu"), (" " * 11), ("Ctrl+T  ", "fg"), ("sesión", "mu")),
            L(("  Alt+↑ ↓        ", "fg"), ("pestañas", "mu"), (" " * 11), ("Alt+t   ", "fg"), ("pestaña", "mu")),
            L(("  Ctrl+1…9       ", "fg"), ("ir a la N", "mu"), (" " * 10), ("Ctrl+⇧T ", "fg"), ("reabrir", "mu")), L(""),
            L(("  ESTADOS   ", "dm"), ("● ", "bu"), ("trabajando  ", "mu"), ("? ", "wa"), ("pide permiso  ", "mu"), ("○ ", "ok"), ("lista  ", "mu"), ("z suspendida", "dm"))]
    return term(overlay(BASE, box(rows, 66, "Ayuda"), 3, 22), title="Windows Terminal")
def help_b():
    rows = BASE[:-1] + [L((" Ctrl+T ", "sel"), (" nueva ", "mu"), (" Ctrl+W ", "sel"), (" cerrar ", "mu"), (" Ctrl+⇧T ", "sel"), (" reabrir ", "mu"),
                         (" Ctrl+PgUp/Dn ", "sel"), (" cambiar ", "mu"), (" Alt+m ", "sel"), (" menú ", "mu"), (" ? ", "sel"), (" más", "mu"))]
    return term(rows, title="Windows Terminal")
screen("t-ayuda", "Ayuda y atajos", "terminal", "Cómo descubrir los atajos.",
       [("A · Hoja de atajos en caja", "Dos columnas y la leyenda de estados.", help_a()),
        ("B · Siempre visible abajo", "Los atajos principales como teclas en la línea inferior (como nano/lazygit); «?» abre el resto.", help_b())])

# -- T15 combinar / por ID
def combine_t():
    rows = [L(""), L(("  ESTA VISTA  ", "dm"), ("fix-matriz", "fg")), L(""), L(("  AGREGAR", "dm")),
            Line(("  [x] bug-173   review-291", "sel")).pad(56, "sel"), L(("  [ ] bug-173   tests", "fg")), L(("  [ ] front     arb-strings", "fg")), L(""),
            L(("  ", ""), (" Columnas ", "sel"), ("  Filas  Cuadrícula", "mu")), L(""), L((" " * 30), ("Cancelar", "mu"), ("   ", ""), (" Combinar 1 ", "sel b"))]
    return term(overlay(BASE, box(rows, 58, "Combinar"), 2, 26), title="Windows Terminal")
def byid_t():
    rows = [L(""), L(("  ID  ", "mu"), *Line((" 7c5d0e▏", "acb")).pad(44, "acb").segs), L(""),
            Line(("  7c5d0e1a  fix-matriz      hace 5 min   ~/work/backend", "sel")).pad(66, "sel"),
            L(("  «arregla el filtro de acentos de la matriz de riesgo»", "dm")), L(""), L((" " * 40), ("Cancelar", "mu"), ("   ", ""), (" Retomar ", "sel b"))]
    return term(overlay(BASE, box(rows, 70, "Retomar por ID"), 4, 20), title="Windows Terminal")
screen("t-combinar", "Combinar sesiones y retomar por ID", "terminal", "Dos diálogos de selección.",
       [("Combinar · casillas", "Marca las sesiones que quieres ver juntas y la distribución.", combine_t()),
        ("Retomar por ID · búsqueda en vivo", "Pega el ID (o sus primeros caracteres) y ve la conversación antes de abrirla.", byid_t())])

# -- T16 estados
def state_a():
    rows = bar2() + [L("")] * 5 + [L(("                         ⏾  ", "dm"), ("fix-matriz está suspendida", "fg b")), L(""),
            L(("                            No corre Claude, así que no consume recursos.", "mu")),
            L(("                            Enter la reanuda en su conversación.", "mu"))] + [L("")] * 6 + [hints("Enter reanudar · Ctrl+W cerrar")]
    return term(rows, title="Windows Terminal")
def state_b():
    rows = bar2() + [L((" ⏾ suspendida · Enter reanuda ", "sel"), ("  la conversación sigue guardada", "dm"))] + [L(("  " + l, "dd")) for l in [
        "❯ arregla el filtro de acentos de la matriz de riesgo", "", "● Corrí los tests de risk_matrix: 42 passed in 3.1s."]] + [L("")] * 10 + [hints("Enter reanudar · Ctrl+W cerrar")]
    return term(rows, title="Windows Terminal")
screen("t-estados", "Sesión suspendida / con error", "terminal", "Una sesión que no está corriendo.",
       [("A · Mensaje centrado", "Pantalla limpia con el estado y qué hacer.", state_a()),
        ("B · Franja y la última pantalla atenuada", "Se ve lo último que hizo, atenuado, con una franja arriba.", state_b())])

# ---------------------------------------------------------------- current design (real captures)
import re as _re
ACT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "actual")

def xterm256(n):
    base = ["000000", "cd0000", "00cd00", "cdcd00", "0000ee", "cd00cd", "00cdcd", "e5e5e5",
            "7f7f7f", "ff0000", "00ff00", "ffff00", "5c5cff", "ff00ff", "00ffff", "ffffff"]
    if n < 16:
        return "#" + base[n]
    if n < 232:
        n -= 16
        v = [0, 95, 135, 175, 215, 255]
        return "#%02x%02x%02x" % (v[n // 36], v[(n // 6) % 6], v[n % 6])
    g = 8 + (n - 232) * 10
    return "#%02x%02x%02x" % (g, g, g)

def ansi_html(text):
    """Converts a tmux capture (capture-pane -e) to HTML, one fixed cell per non-ASCII glyph."""
    out, st = [], {"fg": None, "bg": None, "b": False, "d": False, "r": False}
    def style():
        fg, bg = st["fg"], st["bg"]
        if st["r"]:
            fg, bg = (bg or "#121212"), (fg or "#c9c6bf")
        css = []
        if fg: css.append("color:" + fg)
        if bg: css.append("background:" + bg)
        if st["b"]: css.append("font-weight:700")
        if st["d"]: css.append("opacity:.6")
        return ";".join(css)
    pos = 0
    for m in _re.finditer(r"\x1b\[([0-9;]*)m", text):
        chunk = text[pos:m.start()]
        if chunk:
            e = "".join(f'<span class="c">{html.escape(c)}</span>' if ord(c) > 127 else html.escape(c) for c in chunk)
            sty = style()
            out.append(f'<span style="{sty}">{e}</span>' if sty else e)
        codes = [int(c) if c else 0 for c in m.group(1).split(";")] if m.group(1) else [0]
        i = 0
        while i < len(codes):
            c = codes[i]
            if c == 0: st.update(fg=None, bg=None, b=False, d=False, r=False)
            elif c == 1: st["b"] = True
            elif c == 2: st["d"] = True
            elif c == 22: st["b"] = st["d"] = False
            elif c == 7: st["r"] = True
            elif c == 27: st["r"] = False
            elif c in (38, 48) and i + 2 < len(codes) and codes[i + 1] == 5:
                st["fg" if c == 38 else "bg"] = xterm256(codes[i + 2]); i += 2
            elif c in (38, 48) and i + 4 < len(codes) and codes[i + 1] == 2:
                st["fg" if c == 38 else "bg"] = "#%02x%02x%02x" % tuple(codes[i + 2:i + 5]); i += 4
            elif c == 39: st["fg"] = None
            elif c == 49: st["bg"] = None
            elif 30 <= c <= 37: st["fg"] = xterm256(c - 30)
            elif 90 <= c <= 97: st["fg"] = xterm256(c - 90 + 8)
            elif 40 <= c <= 47: st["bg"] = xterm256(c - 40)
            elif 100 <= c <= 107: st["bg"] = xterm256(c - 100 + 8)
            i += 1
        pos = m.end()
    tail = text[pos:]
    out.append("".join(f'<span class="c">{html.escape(c)}</span>' if ord(c) > 127 else html.escape(c) for c in tail))
    return "".join(out)

def actual_term(name, title="Windows Terminal · cw 0.9 (captura real)"):
    path = os.path.join(ACT, name + ".ans")
    if not os.path.exists(path):
        return None
    body = ansi_html(open(path, encoding="utf-8").read().rstrip("\n"))
    return f'<div class="T actual"><div class="tt"><i></i><i></i><i></i>{html.escape(title)}</div><pre>{body}</pre></div>'

def actual_img(name):
    if not os.path.exists(os.path.join(ACT, name + ".png")):
        return None
    return f'<div class="shot"><img src="actual/{name}.png" alt="diseño actual"></div>'

ACT_DESC = {
    "d": "Captura real de la app de escritorio en la versión 0.9.",
    "t": "Captura real de la versión de terminal 0.9 (tmux), con sus colores.",
}
EXTRA_ACTUAL = {"t-combinar": ["t-combinar", "t-porid"], "t-menuprincipal": ["t-menuprincipal"]}
NOTES = {"t-menuprincipal": "Captura con 50 filas: con menos de 36 filas este menú hoy no se abre (tmux no lo dibuja si no cabe)."}
for idx, (sid, title, kind, intro, options) in enumerate(sections):
    acts = []
    if kind == "desktop":
        b = actual_img("act-" + sid)
        if b: acts.append(("Actual · 0.9", ACT_DESC["d"], b))
    else:
        for nm in EXTRA_ACTUAL.get(sid, [sid]):
            b = actual_term(nm)
            if b:
                label = "Actual · 0.9" + (" (retomar por ID)" if nm == "t-porid" else (" (combinar)" if sid == "t-combinar" else ""))
                acts.append((label, NOTES.get(sid, ACT_DESC["t"]), b))
    sections[idx] = (sid, title, kind, intro, acts + options)

# ---------------------------------------------------------------- HTML
CSS = open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "rediseno-3.css")).read()
parts = [f"""<!doctype html><html lang="es"><head><meta charset="utf-8"><title>cw · rediseño por pantalla</title><style>{CSS}</style></head><body>
<nav class="toc"><b>cw · rediseño</b><div class="grp2">Escritorio</div>"""]
for sid, title, kind, _, _ in sections:
    if kind == "desktop":
        parts.append(f'<a href="#{sid}">{html.escape(title)}</a>')
parts.append('<div class="grp2">Terminal</div>')
for sid, title, kind, _, _ in sections:
    if kind == "terminal":
        parts.append(f'<a href="#{sid}">{html.escape(title)}</a>')
parts.append("""</nav><main class="page"><h1>Rediseño de cw, pantalla por pantalla</h1>
<p class="lead"><b>Actual</b> (borde punteado) es una captura real de cw 0.9, para comparar; las letras son las propuestas.<br>Cada pantalla de la app de escritorio y de la versión de terminal, con dos o tres opciones. Todas comparten el mismo lenguaje sobrio:
grises cálidos, un acento azul grisáceo solo para el foco y color únicamente en los estados (● ámbar trabajando · ● rojo pide permiso · ● verde lista · ○ suspendida).
Para elegir, dime por pantalla la letra: por ejemplo «inicio A, sesión C, menú B…».</p>""")
for sid, title, kind, intro, options in sections:
    tag = "Escritorio" if kind == "desktop" else "Terminal"
    parts.append(f'<section id="{sid}" class="{"term-sec" if kind == "terminal" else ""}"><h2><span class="kind {kind}">{tag}</span> {html.escape(title)}</h2><p class="intro">{html.escape(intro)}</p><div class="opts n{len(options)}">')
    for otitle, desc, body in options:
        cls = "opt cur" if otitle.startswith("Actual") else "opt"
        parts.append(f'<div class="{cls}"><div class="ot">{html.escape(otitle)}</div><p class="od">{html.escape(desc)}</p>{body}</div>')
    parts.append("</div></section>")
parts.append("</main></body></html>")
open(OUT, "w").write("".join(parts))
print("wrote", OUT, len(sections), "screens")
