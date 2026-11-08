"""Soft drawings, two more: magpie and MrMorph. The same objects as
drawings/apps_extra.py, about half the detail, no text."""

import math

from lib import brand_strokes, soft, soft_shadow, tile, two


def _gem(ic, cx, cy, r, light, dark):
    pts = [(cx + r * math.cos(math.radians(a)), cy + r * 0.86 * math.sin(math.radians(a))) for a in range(0, 360, 45)]
    ic.poly(pts, fill=two(ic, light, dark))
    ic.circle(cx - r * 0.35, cy - r * 0.3, r * 0.25, fill="#fff", opacity=0.8)


@soft("magpie")
def magpie(ic):
    tile(ic, "#93677B", "#4B2A3C")
    soft_shadow(ic, 118, 210, 96, 11)
    # the nest and its hoard
    ic.ellipse(96, 160, 66, 19, fill=two(ic, "#C4955C", "#8A5E30"))
    ic.ellipse(97, 162, 55, 12, fill="#3A2614")
    ic.ellipse(66, 161, 9, 4.5, fill=two(ic, "#F2CC78", "#C8963E"))
    _gem(ic, 86, 157, 10, "#9BEFFC", "#2CA6C8")
    ic.rect(100, 151, 16, 16, rx=4, fill="#23272D")
    ic.rect(104, 155, 8, 8, rx=2, fill="#8EF0A0")
    _gem(ic, 129, 156, 8, "#FFC4EA", "#D454A8")
    bowl = "M30 160A66 19 0 0 0 162 160C160 190 134 208 96 208C58 208 32 190 30 160Z"
    ic.path(bowl, fill=two(ic, "#A87A44", "#6E4822"))
    weave = ic.clip(f'<path d="{bowl}"/>')
    with ic.g(clip_path=weave):
        for dy, ry in ((14, 24), (30, 30)):
            ic.path(f"M26 {160 + dy}A70 {ry} 0 0 0 166 {160 + dy}", fill="none", stroke="#C9A06A", stroke_width=4,
                    opacity=0.7)
    ic.path("M30 160A66 19 0 0 0 162 160", fill="none", stroke="#D8B47C", stroke_width=5, stroke_linecap="round")
    # the magpie
    ic.path("M178 134L196 127C208 146 222 168 236 188C239 196 232 202 225 198C210 180 194 162 176 146Z",
            fill=two(ic, "#2F8A86", "#2E5296"))
    for x0, x1, y1 in ((152, 148, 164), (164, 160, 162)):
        ic.line(x0, 146, x1, y1, stroke="#16181C", stroke_width=4, stroke_linecap="round")
    ic.path("M138 58C164 64 186 98 196 130L186 146C172 154 152 156 140 148C124 138 114 118 112 96C110 78 120 60 138 58Z",
            fill=two(ic, "#363A42", "#16181C"))
    ic.path("M122 118C136 114 154 120 170 128C182 134 188 142 184 147C172 154 152 156 140 148C130 141 124 130 122 118Z",
            fill=two(ic, "#FFFFFF", "#E2E4EA"))
    ic.path("M142 92C166 92 194 118 212 156C194 152 176 144 162 132C150 122 142 108 142 92Z",
            fill=two(ic, "#2C6FA0", "#16325A"))
    ic.path("M144 94C160 94 178 108 190 124C176 124 162 116 152 108C147 104 144 99 144 94Z", fill="#FFFFFF")
    ic.path("M114 72L90 82L114 88Z", fill="#16181C")
    ic.circle(124, 76, 4, fill="#FFFFFF", opacity=0.85)
    ic.circle(124.6, 76.4, 2.6, fill="#08090B")


@soft("com.mistermorph")
def mistermorph(ic):
    tile(ic, "#3E4270", "#14162C")
    cx, cy, r = 128, 120, 98
    soft_shadow(ic, cx, 224, 86, 10)
    ic.circle(cx, cy, r, fill=two(ic, "#1A1D2E", "#07080F"))
    k = r / 49
    with ic.g(transform=f"translate({cx - 50 * k:.3f} {cy - 50 * k:.3f}) scale({k:.5f})"):
        for d, colour, w in brand_strokes("mistermorph", every=3):
            white = colour.upper() == "#F2F7FF"
            ic.path(d, fill="none", stroke=colour, stroke_width=w * (1.8 if white else 1.0), stroke_linecap="round")
