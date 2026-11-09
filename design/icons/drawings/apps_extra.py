"""The inua-objects theme, two more: magpie and MrMorph
(docs/feat/object-icons.md)."""

import math

from lib import brand_strokes, brass, glare, icon, shadow, small, tile, wood


# --- magpie: a magpie on the rim of its nest, its hoard of bright chips -----

def _gem(ic, cx, cy, r, light, dark, glow):
    """A cut stone lying in the nest, seen from above, glowing a little."""
    ic.circle(cx, cy, r * 2.1, fill=ic.rad([(0, glow, 0.55), (0.5, glow, 0.18), (1, glow, 0)]))
    pts = [(cx + r * math.cos(math.radians(a)), cy + r * 0.86 * math.sin(math.radians(a)))
           for a in range(0, 360, 45)]
    ic.poly(pts, fill=ic.lin([(0, light), (1, dark)], 0, 0, 1, 1))
    t = r * 0.5
    ic.poly([(cx - t, cy - t * 0.86), (cx + t, cy - t * 0.86), (cx + t, cy + t * 0.86), (cx - t, cy + t * 0.86)],
            fill="#fff", opacity=0.28)
    ic.path(f"M{cx - r:.1f} {cy:.1f}L{cx - t:.1f} {cy - t * 0.86:.1f}L{cx:.1f} {cy - r * 0.86:.1f}Z",
            fill="#fff", opacity=0.45)
    ic.circle(cx - t * 0.6, cy - t * 0.7, r * 0.18, fill="#fff")


def _chip(ic, cx, cy, s, glow):
    """A small chip, gold pins out on four sides, its die lit."""
    ic.circle(cx, cy, s * 1.5, fill=ic.rad([(0, glow, 0.5), (0.5, glow, 0.15), (1, glow, 0)]))
    h = s / 2
    for i in (-0.25, 0.25):
        for side in (-1, 1):
            ic.rect(cx + i * s - 1.2, cy + side * h - (2.8 if side < 0 else 0), 2.4, 2.8, fill="#E2B860")
            ic.rect(cx + side * h - (2.8 if side < 0 else 0), cy + i * s - 1.2, 2.8, 2.4, fill="#E2B860")
    ic.rect(cx - h, cy - h, s, s, rx=2, fill=ic.lin([(0, "#3A4048"), (1, "#14171B")], 0, 0, 1, 1))
    ic.rect(cx - h * 0.5, cy - h * 0.5, s * 0.5, s * 0.5, rx=1, fill=glow)
    ic.rect(cx - h * 0.5, cy - h * 0.5, s * 0.5, s * 0.2, rx=1, fill="#fff", opacity=0.5)


@icon("magpie", kind="app", names=["magpie"], title="magpie")
def magpie(ic):
    tile(ic, "#93677B", "#4B2A3C")
    shadow(ic, 118, 210, 96, 11)
    # the nest: the back of its rim, the hollow, the hoard in it
    ic.ellipse(96, 160, 66, 19, fill=ic.lin([(0, "#B98A52"), (1, "#7A5228")]))
    ic.ellipse(97, 162, 55, 12, fill=ic.rad([(0, "#2A1A0E"), (1, "#4A3018")], cx=0.5, cy=0.7, r=0.7))
    for x0, y0, x1, y1 in ((40, 154, 70, 147), (118, 145, 150, 152), (60, 146, 92, 142)):
        ic.line(x0, y0, x1, y1, stroke="#D8B47C", stroke_width=1.6, stroke_linecap="round", opacity=0.7)
    ic.ellipse(68, 165, 9, 4, fill="#7E5A20")
    ic.ellipse(68, 162.5, 9, 4, fill=brass(ic))
    ic.ellipse(68, 162.5, 6, 2.6, fill="none", stroke="#7E5A20", stroke_opacity=0.6, stroke_width=0.8)
    _gem(ic, 86, 158, 9, "#B8F4FF", "#1C8FB0", "#5FE0F5")
    _chip(ic, 108, 160, 13, "#9DF5A8")
    _gem(ic, 127, 156, 7, "#FFD9F2", "#C23E96", "#F07AD0")
    _gem(ic, 60, 154, 6, "#FFE7A6", "#C98A1E", "#F7C24A")
    # the bowl in front, woven of twigs
    bowl = "M30 160A66 19 0 0 0 162 160C160 190 134 208 96 208C58 208 32 190 30 160Z"
    ic.path(bowl, fill=ic.lin([(0, "#A87A44"), (0.55, "#7A5228"), (1, "#4E321A")]))
    weave = ic.clip(f'<path d="{bowl}"/>')
    with ic.g(clip_path=weave):
        for k, dy in enumerate((6, 14, 22, 30, 38, 46)):
            ry = 19 + dy * 0.4
            for off, col, op in ((-1.2, "#E6C690", 0.45), (1.4, "#3A2410", 0.45)):
                ic.path(f"M28 {160 + dy + off}A68 {ry} 0 0 0 164 {160 + dy + off}",
                        fill="none", stroke=col, stroke_opacity=op, stroke_width=1.6)
        for x, y, d in ((44, 176, 1), (66, 186, -1), (92, 192, 1), (118, 188, -1), (140, 178, 1),
                        (54, 194, -1), (104, 202, 1), (130, 198, 1), (80, 200, -1), (150, 166, -1), (38, 166, 1)):
            ic.line(x - 8, y - 3 * d, x + 8, y + 3 * d, stroke="#C9A06A", stroke_width=2.2, stroke_linecap="round",
                    opacity=0.75)
            ic.line(x - 8, y - 3 * d + 2, x + 8, y + 3 * d + 2, stroke="#2E1C0C", stroke_width=1.2,
                    stroke_linecap="round", opacity=0.4)
    ic.path("M30 160A66 19 0 0 0 162 160", fill="none", stroke="#D8B47C", stroke_width=3, opacity=0.85)
    # a few loose twigs at the rim
    for x0, y0, x1, y1 in ((22, 150, 44, 166), (150, 172, 172, 164), (34, 176, 18, 170)):
        ic.line(x0, y0, x1, y1, stroke="#8A5E30", stroke_width=2.4, stroke_linecap="round")
    # the magpie, on the rim at the right, looking into its hoard
    black = ic.lin([(0, "#3E434C"), (0.5, "#16181C"), (1, "#08090B")], 0, 0, 1, 1)
    sheen = ic.lin([(0, "#2C6FA0"), (0.5, "#1B3E6A"), (1, "#0C1424")], 0, 0, 1, 1)
    tail = ic.lin([(0, "#1C2C44"), (0.4, "#2A7C78"), (0.75, "#2E5E9C"), (1, "#5A3F94")], 0, 0, 1, 1)
    # the tail, long and graduated, down behind the rim
    ic.path("M178 134L196 127C208 146 222 168 236 188C239 196 232 202 225 198C210 180 194 162 176 146Z", fill=tail)
    ic.line(188, 134, 230, 194, stroke="#7FD8D0", stroke_width=1.4, stroke_linecap="round", opacity=0.45)
    # legs and feet, gripping the rim
    for x0, x1, y1 in ((152, 148, 164), (164, 160, 162)):
        ic.line(x0, 146, x1, y1, stroke="#14161A", stroke_width=3, stroke_linecap="round")
        ic.path(f"M{x1 - 7} {y1 + 1}L{x1 + 5} {y1 + 1}", stroke="#14161A", stroke_width=2.4, stroke_linecap="round")
    # body: head, back and bib black, the belly white
    ic.path("M138 58C164 64 186 98 196 130L186 146C172 154 152 156 140 148C124 138 114 118 112 96C110 78 120 60 138 58Z",
            fill=black)
    ic.path("M122 118C136 114 154 120 170 128C182 134 188 142 184 147C172 154 152 156 140 148C130 141 124 130 122 118Z",
            fill=ic.lin([(0, "#FFFFFF"), (0.7, "#E9EAEE"), (1, "#B9BCC6")], 0, 0, 1, 1))
    ic.path("M122 118C128 116 134 116 140 117", fill="none", stroke="#000", stroke_opacity=0.25, stroke_width=1.5)
    # the folded wing: the white shoulder, then the blue sheen down to the tip
    ic.path("M142 92C166 92 194 118 212 156C194 152 176 144 162 132C150 122 142 108 142 92Z", fill=sheen)
    for k in range(4):
        ic.path(f"M{168 + k * 9} {136 + k * 3}C{180 + k * 8} {142 + k * 3} {192 + k * 6} {148 + k * 2} {204 + k * 2} {152 + k}",
                fill="none", stroke="#6FB4E0", stroke_opacity=0.35, stroke_width=1.2)
    ic.path("M144 94C160 94 178 108 190 124C176 124 162 116 152 108C147 104 144 99 144 94Z",
            fill=ic.lin([(0, "#FFFFFF"), (1, "#D6D9E0")]))
    # head: a little blue on the crown, the bill, the eye
    ic.path("M122 66C128 60 138 58 146 62C140 62 132 64 126 70Z", fill="#6C8AB4", opacity=0.6)
    ic.path("M114 72L90 82L114 88Z", fill=ic.lin([(0, "#3A3E46"), (1, "#0B0C0E")]))
    ic.line(112, 81.5, 96, 82, stroke="#000", stroke_width=1)
    ic.line(112, 74.5, 96, 80.5, stroke="#fff", stroke_opacity=0.25, stroke_width=1)
    ic.circle(124, 76, 4.6, fill="#2A2D33")
    ic.circle(124, 76, 3.2, fill="#050506")
    ic.circle(122.8, 74.8, 1.2, fill="#fff")


@small("magpie")
def magpie16(ic):
    tile(ic, "#93677B", "#4B2A3C")
    ic.path("M1.8 10.2C2 12.6 3.8 13.8 6.2 13.8C8.6 13.8 10.4 12.6 10.6 10.2Z", fill="#9C6C38")
    ic.circle(5, 9.8, 1, fill="#5FE0F5")
    ic.circle(7.4, 9.9, 0.9, fill="#F7C24A")
    ic.path("M11 9.4L12 9C13 10.4 14 11.8 14.6 13.4L13.6 13.6C12.8 12.2 11.8 11 10.6 10.2Z", fill="#2A7C9C")
    ic.path("M8.6 3.4C10.6 3.8 12 6.4 12.4 8.8L11.6 9.8C10.6 10.4 9.4 10.4 8.6 9.8C7.6 9 7 7.6 7 6.2C7 4.8 7.6 3.4 8.6 3.4Z",
            fill="#121417")
    ic.path("M7.8 7.6C9 7.6 10.6 8.4 11.4 9.4C10.6 10.2 9.4 10.4 8.6 9.8C8.2 9.2 7.9 8.4 7.8 7.6Z", fill="#F4F5F7")
    ic.path("M8.8 6.2C9.8 6.2 11 7.2 11.6 8.4C10.6 8.2 9.6 7.6 8.8 6.2Z", fill="#FFFFFF")
    ic.path("M7.2 4.4L5.6 5.1L7.2 5.5Z", fill="#121417")


# --- MrMorph: Mister Morph's own icon in its bunko colours
# (brands/mistermorph.svg, Apache 2.0): a black glass dome placed as Chrome's
# is, the mark's lines lit in it, inua's light over it --------------------

MORPH = (128, 120, 98)  # the disc's centre and radius on the tile, as Chrome's
MORPH_TILE = ("#3E4270", "#14162C")


def morph_lines(ic, weight=1.0, core=1.8):
    """The mark's lines as they are, on the disc MORPH, clipped to it: each a
    pink and a cyan line weight times as wide, its white core core times;
    the white core stands for where the two mix (screen) in the original."""
    cx, cy, r = MORPH
    k = r / 49
    disc = ic.clip(f'<circle cx="{cx}" cy="{cy}" r="{r}"/>')
    with ic.g(clip_path=disc):
        with ic.g(transform=f"translate({cx - 50 * k:.3f} {cy - 50 * k:.3f}) scale({k:.5f})"):
            for d, colour, w in brand_strokes("mistermorph", every=3):
                white = colour.upper() == "#F2F7FF"
                ic.path(d, fill="none", stroke=colour, stroke_width=w * (core if white else weight),
                        stroke_linecap="round")


@icon("com.mistermorph", kind="app", names=["com.mistermorph", "mistermorph", "org.wails.mistermorph"],
      title="MrMorph")
def mistermorph(ic):
    tile(ic, *MORPH_TILE)
    cx, cy, r = MORPH
    shadow(ic, cx, 224, 86, 10)
    # the face: black glass, a little lighter where the light comes from
    ic.circle(cx, cy, r, fill=ic.rad([(0, "#1C2033"), (0.7, "#0A0B14"), (1, "#05060B")], cx=0.4, cy=0.36, r=0.75))
    morph_lines(ic)
    # the glass's dome: light from the top left, shade to the lower right
    ic.circle(cx, cy, r, fill=ic.rad([(0, "#fff", 0.2), (0.45, "#fff", 0.03), (1, "#fff", 0)],
                                     cx=0.3, cy=0.25, r=0.65))
    ic.circle(cx, cy, r, fill=ic.rad([(0.7, "#000", 0), (1, "#000", 0.35)], cx=0.42, cy=0.38, r=0.62))
    ic.circle(cx, cy, r - 0.8, fill="none", stroke="#fff", stroke_opacity=0.3, stroke_width=1.6)
    ic.ellipse(cx - 46, cy - 50, 20, 7, fill=ic.lin([(0, "#fff", 0.55), (1, "#fff", 0)], 0, 0, 1, 1),
               transform=f"rotate(-40 {cx - 46} {cy - 50})")
    ic.circle(cx, cy, r, fill=glare(ic, 0.4))


@small("com.mistermorph")
def mistermorph16(ic):
    tile(ic, *MORPH_TILE)
    ic.circle(8, 8, 6.5, fill="#0A0B14")
    for ye, ym, half in ((5.4, 6.1, 3.6), (8, 8, 4.8), (10.6, 9.9, 3.6)):
        for dy, colour, w in ((0.3, "#FF2E8A", 1.0), (-0.3, "#1FE5FF", 1.0), (0, "#F2F7FF", 0.7)):
            ic.path(f"M{8 - half:.1f} {ye + dy:.1f}Q8 {2 * ym - ye + dy:.1f} {8 + half:.1f} {ye + dy:.1f}",
                    fill="none", stroke=colour, stroke_width=w, stroke_linecap="round")
