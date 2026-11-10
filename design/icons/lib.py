"""The drawing kit for inua's object icons (docs/feat/object-icons.md).

An icon is an object on a desk: inua's rounded tile, one light from the
top left, one soft shadow, shared materials, and real detail. Each icon is
a function that draws on an Icon, on a 256 grid; its 16 px drawing is a
second function on a 16 grid. This module holds what they share:

    ic = Icon("files")
    tile(ic, "#557797", "#2C4660")          # the ground, lighter on top
    shadow(ic, 128, 212, 96, 13)            # under the object
    ic.rect(40, 80, 176, 128, rx=8, fill=paper(ic, "kraft"))
    ic.text("Field notes", 82, 84, 11, font="sans-semibold", fill="#2C4660")
    ic.svg()                                 # the file, hairline added last

Colours are literal hex: an icon is a picture, the same in every theme.
Gradients and shapes only, never a filter.
"""

import math
import os
from contextlib import contextmanager
from functools import lru_cache

from fontTools.pens.basePen import BasePen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont
from fontTools.varLib.instancer import instantiateVariableFont

HERE = os.path.dirname(os.path.abspath(__file__))
FONT_DIR = os.path.join(HERE, "fonts")

# font name: (file, variable axes or None)
FONTS = {
    "sans": ("IBMPlexSans[wdth,wght].ttf", {"wght": 400, "wdth": 100}),
    "sans-medium": ("IBMPlexSans[wdth,wght].ttf", {"wght": 500, "wdth": 100}),
    "sans-semibold": ("IBMPlexSans[wdth,wght].ttf", {"wght": 600, "wdth": 100}),
    "sans-bold": ("IBMPlexSans[wdth,wght].ttf", {"wght": 700, "wdth": 100}),
    "sans-condensed": ("IBMPlexSans[wdth,wght].ttf", {"wght": 600, "wdth": 85}),
    "mono": ("IBMPlexMono-Regular.ttf", None),
    "mono-semibold": ("IBMPlexMono-SemiBold.ttf", None),
    "hand": ("Caveat[wght].ttf", {"wght": 600}),
}


@lru_cache(maxsize=None)
def _font(name):
    file, axes = FONTS[name]
    f = TTFont(os.path.join(FONT_DIR, file))
    if axes:
        f = instantiateVariableFont(f, axes)
    return f


def _fmt(v):
    """A number for SVG: short, no trailing zeros."""
    if isinstance(v, float):
        s = f"{v:.2f}".rstrip("0").rstrip(".")
        return s if s not in ("", "-0") else "0"
    return str(v)


def _attrs(kw):
    out = []
    for k, v in kw.items():
        if v is None:
            continue
        k = {"cls": "class", "href": "href"}.get(k, k.replace("_", "-"))
        out.append(f'{k}="{_fmt(v)}"')
    return (" " + " ".join(out)) if out else ""


class Icon:
    """One icon being drawn: its definitions (gradients) and its body."""

    def __init__(self, name, size=256, flat=False):
        self.name = name.lower().replace(".", "-").replace(" ", "-")
        self.size = size
        self.flat = flat  # the flat set: every gradient one colour (_solid)
        self.defs = []
        self.body = []
        self.n = 0
        self.hairline = None  # the tile's, drawn last, over everything

    # --- ids and definitions ------------------------------------------

    def id(self, what="g"):
        self.n += 1
        return f"{self.name}-{what}{self.n}"

    def lin(self, stops, x1=0, y1=0, x2=0, y2=1, units=None):
        """A linear gradient, top to bottom unless told; stops are
        (offset, colour) or (offset, colour, opacity). Returns url(#id)."""
        if self.flat:
            return _solid(stops, 0.3)
        i = self.id("l")
        u = f' gradientUnits="{units}"' if units else ""
        self.defs.append(
            f'<linearGradient id="{i}" x1="{_fmt(x1)}" y1="{_fmt(y1)}" x2="{_fmt(x2)}" y2="{_fmt(y2)}"{u}>'
            + "".join(_stop(s) for s in stops) + "</linearGradient>")
        return f"url(#{i})"

    def rad(self, stops, cx=0.5, cy=0.5, r=0.5, fx=None, fy=None, units=None):
        """A radial gradient. Returns url(#id)."""
        if self.flat:
            return _solid(stops, 0.4)
        i = self.id("r")
        f = (f' fx="{_fmt(fx)}"' if fx is not None else "") + (f' fy="{_fmt(fy)}"' if fy is not None else "")
        u = f' gradientUnits="{units}"' if units else ""
        self.defs.append(
            f'<radialGradient id="{i}" cx="{_fmt(cx)}" cy="{_fmt(cy)}" r="{_fmt(r)}"{f}{u}>'
            + "".join(_stop(s) for s in stops) + "</radialGradient>")
        return f"url(#{i})"

    def define(self, element, what="d"):
        """A definition of its own (a clip path, a pattern); its id is put
        in for {id}. Returns the id."""
        i = self.id(what)
        self.defs.append(element.replace("{id}", i))
        return i

    def clip(self, inner, what="c"):
        """A clip path of the given shapes; returns url(#id)."""
        i = self.id(what)
        self.defs.append(f'<clipPath id="{i}">{inner}</clipPath>')
        return f"url(#{i})"

    # --- shapes --------------------------------------------------------

    def add(self, element):
        self.body.append(element)

    def rect(self, x, y, w, h, rx=0, **kw):
        r = f' rx="{_fmt(rx)}"' if rx else ""
        self.add(f'<rect x="{_fmt(x)}" y="{_fmt(y)}" width="{_fmt(w)}" height="{_fmt(h)}"{r}{_attrs(kw)}/>')

    def circle(self, cx, cy, r, **kw):
        self.add(f'<circle cx="{_fmt(cx)}" cy="{_fmt(cy)}" r="{_fmt(r)}"{_attrs(kw)}/>')

    def ellipse(self, cx, cy, rx, ry, **kw):
        self.add(f'<ellipse cx="{_fmt(cx)}" cy="{_fmt(cy)}" rx="{_fmt(rx)}" ry="{_fmt(ry)}"{_attrs(kw)}/>')

    def path(self, d, **kw):
        self.add(f'<path d="{d}"{_attrs(kw)}/>')

    def line(self, x1, y1, x2, y2, **kw):
        self.add(f'<path d="M{_fmt(x1)} {_fmt(y1)}L{_fmt(x2)} {_fmt(y2)}"{_attrs(kw)}/>')

    def poly(self, points, close=True, **kw):
        d = "M" + "L".join(f"{_fmt(x)} {_fmt(y)}" for x, y in points) + ("Z" if close else "")
        self.path(d, **kw)

    @contextmanager
    def g(self, **kw):
        """A group: with ic.g(transform="rotate(-6 128 130)"): …"""
        self.add(f"<g{_attrs(kw)}>")
        try:
            yield
        finally:
            self.add("</g>")

    # --- text, as outlines --------------------------------------------

    def text(self, s, x, y, size, font="sans", anchor="start", spacing=0, **kw):
        """A line of text at baseline y, turned into an outline in font
        (FONTS), size in px; anchor start, middle or end; spacing in px
        between letters. Returns the line's width."""
        d, width = text_path(s, x, y, size, font, anchor, spacing)
        if d:
            self.path(d, **kw)
        return width

    # --- the file -------------------------------------------------------

    def svg(self):
        defs = f"<defs>{''.join(self.defs)}</defs>" if self.defs else ""
        tail = self.hairline or ""
        return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {self.size} {self.size}">'
                f"{defs}{''.join(self.body)}{tail}</svg>\n")


def _rgb(c):
    c = c.lstrip("#")
    if len(c) == 3:
        c = "".join(ch * 2 for ch in c)
    return [int(c[i:i + 2], 16) for i in (0, 2, 4)]


def _solid(stops, t):
    """A gradient's colour at t (0 to 1), for the flat set: the lit side
    of a surface. With opacity under 1 it is an rgba() colour."""
    stops = sorted(stops, key=lambda s: s[0])
    a, b = stops[0], stops[-1]
    for s, n in zip(stops, stops[1:]):
        if s[0] <= t <= n[0]:
            a, b = s, n
            break
    else:
        if t < stops[0][0]:
            b = a
        else:
            a = b
    k = 0 if b[0] == a[0] else (t - a[0]) / (b[0] - a[0])
    k = min(1, max(0, k))
    try:
        ca, cb = _rgb(a[1]), _rgb(b[1])
    except ValueError:
        return a[1]
    rgb = [round(x + (y - x) * k) for x, y in zip(ca, cb)]
    op = (a[2] if len(a) > 2 else 1) * (1 - k) + (b[2] if len(b) > 2 else 1) * k
    if op >= 0.995:
        return "#" + "".join(f"{v:02X}" for v in rgb)
    return f"rgba({rgb[0]},{rgb[1]},{rgb[2]},{_fmt(round(op, 3))})"


def _stop(s):
    off, col = s[0], s[1]
    op = f' stop-opacity="{_fmt(s[2])}"' if len(s) > 2 and s[2] != 1 else ""
    return f'<stop offset="{_fmt(off)}" stop-color="{col}"{op}/>'


class _RelPen(BasePen):
    """Path data in relative commands, each point rounded to a tenth of
    a pixel first so the rounding does not add up along a contour: about a
    third shorter than absolute data."""

    def __init__(self, gs):
        super().__init__(gs)
        self.out, self.cur, self.start = [], (0, 0), (0, 0)

    def _emit(self, cmd, *pts):
        vals = []
        for p in pts:
            q = (round(p[0] * 10), round(p[1] * 10))
            vals += [q[0] - self.cur[0], q[1] - self.cur[1]]
        self.cur = q
        s = ""
        for v in vals:
            t = _fmt(v / 10).replace("0.", ".", 1) if abs(v) < 10 and v % 10 else _fmt(v / 10)
            s += t if not s or t.startswith("-") else " " + t
        if self.out and self.out[-1][0] == cmd and cmd != "m":
            cmd = "" if s.startswith("-") else " "  # the same command again
        else:
            self.out.append("")
        self.out[-1] += cmd + s

    def _moveTo(self, p):
        self._emit("m", p)
        self.start = self.cur

    def _lineTo(self, p):
        self._emit("l", p)

    def _qCurveToOne(self, p1, p2):
        self._emit("q", p1, p2)

    def _curveToOne(self, p1, p2, p3):
        self._emit("c", p1, p2, p3)

    def _closePath(self):
        self.out.append("z")
        self.cur = self.start

    def _endPath(self):
        self.cur = self.start


def text_path(s, x, y, size, font="sans", anchor="start", spacing=0):
    """The outline of a line of text, as SVG path data, and its width."""
    f = _font(font)
    cmap = f.getBestCmap()
    gs = f.getGlyphSet()
    hmtx = f["hmtx"]
    scale = size / f["head"].unitsPerEm
    missing = [c for c in s if ord(c) not in cmap]
    if missing:
        raise ValueError(f"font {font} has no {''.join(missing)!r}: draw it as a shape")
    names = [cmap[ord(c)] for c in s]
    width = sum(hmtx[n][0] for n in names) * scale + spacing * max(0, len(s) - 1)
    pen_x = {"start": x, "middle": x - width / 2, "end": x - width}[anchor]
    pen = _RelPen(gs)
    for n in names:
        gs[n].draw(TransformPen(pen, (scale, 0, 0, -scale, pen_x, y)))
        pen_x += hmtx[n][0] * scale + spacing
    return "".join(pen.out), width


# --- the house style --------------------------------------------------

def tile(ic, top, bottom, radius=None):
    """inua's rounded tile in the ground's two tones, and its hairline of
    light, drawn last over everything."""
    s = ic.size
    r = radius if radius is not None else (56 if s == 256 else 3.5)
    ground = _solid([(0, top), (1, bottom)], 0.5) if ic.flat else ic.lin([(0, top), (1, bottom)])
    ic.rect(0, 0, s, s, rx=r, fill=ground)
    w = 2 if s == 256 else 0.5
    ic.hairline = (f'<rect x="{_fmt(w / 2)}" y="{_fmt(w / 2)}" width="{_fmt(s - w)}" height="{_fmt(s - w)}" '
                   f'rx="{_fmt(r - w / 2)}" fill="none" stroke="#fff" stroke-opacity="{0.14 if s == 256 else 0.18}" '
                   f'stroke-width="{_fmt(w)}"/>')


def shadow(ic, cx, cy, rx, ry, strength=1.0):
    """The soft shadow under an object, from the one shared gradient."""
    g = ic.rad([(0, "#000", 0.5 * strength), (0.6, "#000", 0.18 * strength), (1, "#000", 0)])
    ic.ellipse(cx, cy, rx, ry, fill=g)


def glare(ic, strength=1.0):
    """Light across glass, from the top left."""
    return ic.lin([(0, "#fff", 0.22 * strength), (0.5, "#fff", 0.04 * strength), (1, "#fff", 0)], 0, 0, 1, 1)


# Materials: a gradient for each, lit from the top left.

def paper(ic, kind="white"):
    tones = {
        "white": ("#FFFFFF", "#E7E3DA"),
        "cream": ("#FCF9F2", "#E4DCCB"),
        "kraft": ("#F3D493", "#C69447"),
        "kraft-dark": ("#C99C50", "#9C7231"),
        "washi": ("#F7F2E7", "#D8D0BF"),
        "newsprint": ("#EEEADF", "#D6D0C2"),
    }[kind]
    return ic.lin([(0, tones[0]), (1, tones[1])])


def brass(ic, x1=0, y1=0, x2=1, y2=1):
    return ic.lin([(0, "#F6DDA0"), (0.45, "#C99645"), (1, "#7E5A20")], x1, y1, x2, y2)


def steel(ic, horizontal=True):
    if horizontal:
        return ic.lin([(0, "#6B727B"), (0.5, "#C3C8CE"), (1, "#5D636B")], 0, 0, 1, 0)
    return ic.lin([(0, "#D4D8DD"), (0.5, "#9AA1A9"), (1, "#5D636B")])


def graphite(ic):
    return ic.lin([(0, "#4F535A"), (1, "#1E2024")], 0, 0, 1, 1)


def glass_dark(ic, tint="#103A22"):
    return ic.rad([(0, tint), (1, "#050B08")], cx=0.35, cy=0.4, r=0.8)


def wood(ic, light="#C48A55", dark="#7A4A22"):
    return ic.lin([(0, light), (1, dark)], 0, 0, 1, 1)


def felt(ic, light="#2F7A4B", dark="#164A2B"):
    return ic.rad([(0, light), (1, dark)], cx=0.4, cy=0.35, r=0.8)


def leather(ic, light="#8E3A2E", dark="#4E1A13"):
    return ic.lin([(0, light), (1, dark)], 0, 0, 1, 1)


def enamel(ic, light, dark):
    return ic.lin([(0, light), (1, dark)])


def wax(ic):
    return ic.rad([(0, "#E25A47"), (0.55, "#B1311F"), (1, "#6E1A10")], cx=0.38, cy=0.32, r=0.75)


def mark_path():
    """inua's mark (the logo's small version), its path on a 1024 grid."""
    import re
    with open(os.path.join(HERE, "..", "logo", "inua-mark-small.svg")) as f:
        return re.search(r'<path d="([^"]+)"', f.read()).group(1)


def brand(name):
    """A brand's own mark, brands/NAME.svg (where its licence allows a
    remix, noted in the file): its gradients (tag, attributes, stops, by
    id) and its shapes (tag, geometry, fill: url(#id) or a colour), the
    ones filled with nothing left out."""
    import re
    import xml.etree.ElementTree as ET
    root = ET.parse(os.path.join(HERE, "brands", name + ".svg")).getroot()
    grads, shapes = {}, []
    for el in root.iter():
        tag = el.tag.split("}")[1]
        if tag.endswith("Gradient"):
            grads[el.get("id")] = (tag, {k: v for k, v in el.attrib.items() if k != "id"},
                                   [dict(s.attrib) for s in el])
        elif tag in ("path", "circle"):
            m = re.search(r"fill:([^;]+)", el.get("style", ""))
            fill = el.get("fill") or (m and m.group(1))
            if fill and fill != "none":
                geom = {k: el.get(k) for k in ("d", "cx", "cy", "r") if el.get(k) is not None}
                shapes.append((tag, geom, fill))
    return grads, shapes


def brand_strokes(name, every=1):
    """A brand's stroked lines, brands/NAME.svg: (d, colour, width) each. A
    line drawn as a polyline of many short steps (M x y L x y …) keeps
    every n-th of its points and its last, to cost fewer bytes."""
    import re
    import xml.etree.ElementTree as ET
    root = ET.parse(os.path.join(HERE, "brands", name + ".svg")).getroot()
    out = []
    for el in root.iter():
        if not el.tag.endswith("}path") or el.get("stroke") in (None, "none"):
            continue
        d = el.get("d")
        if every > 1 and re.fullmatch(r"M[\d.\s-]+(L[\d.\s-]+)*", d.strip()):
            nums = [float(v) for v in re.findall(r"-?[\d.]+", d)]
            pts = list(zip(nums[0::2], nums[1::2]))
            pts = pts[:-1:every] + pts[-1:]
            d = "M" + "L".join(f"{_fmt(x)} {_fmt(y)}" for x, y in pts)
        out.append((d, el.get("stroke"), float(el.get("stroke-width", "1"))))
    return out


def brand_gradients(ic, grads):
    """Defines a brand's gradients in ic, as they are: their new ids, by
    their own."""
    ids = {}
    for gid, (tag, attrs, stops) in grads.items():
        a = "".join(f' {k}="{v}"' for k, v in attrs.items())
        st = "".join("<stop" + "".join(f' {k}="{v}"' for k, v in s.items()) + "/>" for s in stops)
        ids[gid] = ic.define(f'<{tag} id="{{id}}"{a}>{st}</{tag}>', "m")
    return ids


def brand_shape(ic, tag, geom, **kw):
    """Draws one of a brand's shapes."""
    if tag == "circle":
        ic.circle(float(geom["cx"]), float(geom["cy"]), float(geom["r"]), **kw)
    else:
        ic.path(geom["d"], **kw)


def mark(ic, x, y, size, **kw):
    """inua's mark, size px square, its top left at (x, y)."""
    k = size / 1024
    ic.path(mark_path(), transform=f"translate({_fmt(x)} {_fmt(y)}) scale({k:.5f})", **kw)


# --- the soft style (soft/) ---------------------------------------------
#
# The same objects with about half the detail: a flat colour or one gentle
# gradient a surface (two()), the soft shadow (soft_shadow()), no text and
# no glare; writing is drawn as rounded bars (bar()).

def two(ic, light, dark):
    """A surface's one gradient, light at the top to dark at the bottom."""
    return ic.lin([(0, light), (1, dark)])


def soft_shadow(ic, cx, cy, rx, ry):
    """The soft set's shadow; in the flat set, a hard one."""
    if ic.flat:
        ic.ellipse(cx, cy, rx * 0.86, ry * 0.7, fill="#000", opacity=0.2)
    else:
        ic.ellipse(cx, cy, rx, ry, fill=ic.rad([(0, "#000", 0.32), (1, "#000", 0)]))


def bar(ic, x, y, w, h=7, fill="#C9D3DD", **kw):
    """A line of writing, as a rounded bar."""
    ic.rect(x, y, w, h, rx=h / 2, fill=fill, **kw)


# --- the flat set --------------------------------------------------------
#
# The soft drawings drawn with Icon(flat=True): every gradient is one solid
# colour, its lit side (_solid), the tile one colour, the shadow hard. An
# icon that needs more (a darker side as its own shape) has its own @flat.


# --- the registry -------------------------------------------------------

ICONS = {}


def icon(name, *, kind="inua", names=(), title=None):
    """Registers an icon's drawing: kind "inua" (config/apps, file
    io.github.lyricat.Inua.NAME) or "app" (the inua-objects theme, file
    NAME, linked under names too)."""
    def wrap(f):
        e = ICONS.setdefault(name, {"name": name, "kind": kind, "names": list(names), "title": title or name})
        e["draw"] = f
        return f
    return wrap


def small(name):
    """Registers an icon's 16 px drawing, on a 16 grid."""
    def wrap(f):
        ICONS.setdefault(name, {"name": name})["small"] = f
        return f
    return wrap


def soft(name):
    """Registers an icon's soft drawing (the inua-objects-soft theme), on
    a 256 grid; its kind, names and title are the @icon's of that name.
    The 16 px drawing is the same in both sets."""
    def wrap(f):
        ICONS.setdefault(name, {"name": name})["soft"] = f
        return f
    return wrap


def flat(name):
    """Registers an icon's own flat drawing (the inua-objects-flat theme),
    in place of its soft drawing drawn flat; draw it on Icon.flat."""
    def wrap(f):
        ICONS.setdefault(name, {"name": name})["flat"] = f
        return f
    return wrap


def polar(cx, cy, r, deg):
    """The point at angle deg (0 up, clockwise) and radius r from (cx, cy)."""
    a = math.radians(deg)
    return cx + r * math.sin(a), cy - r * math.cos(a)
