#!/usr/bin/env python3
"""Builds inua's object icons and the inua-objects icon theme
(docs/feat/object-icons.md) from the drawings in design/icons/drawings/.

    python3 design/icons/build.py            # everything
    python3 design/icons/build.py files mail # only these, and a sheet of them
    python3 design/icons/build.py --sheet    # also a contact sheet (PNG, via Chrome)
    python3 design/icons/build.py --sheet-dir=bin/sheet-a files mail  # the sheet elsewhere
    python3 design/icons/build.py --soft files mail  # a sheet of their soft drawings
    python3 design/icons/build.py --flat files mail  # … or of their flat ones

Writes:
    config/apps/io.github.lyricat.Inua.NAME.svg        inua's icons
    config/apps/small/io.github.lyricat.Inua.NAME.svg  their 16 px drawings
    config/icons/inua-objects/                         the theme for other apps
    config/icons/inua-objects-soft/                    the soft drawings (soft/), of
                                                       inua's apps and other apps
    config/icons/inua-objects-flat/                    the same drawn flat
    bin/icon-sheet/                                    the sheet (not committed)

It needs Python 3 with fontTools (and Chrome for the sheet).
"""

import glob
import importlib.util
import os
import re
import shutil
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.normpath(os.path.join(HERE, "..", ".."))
sys.path.insert(0, HERE)

import lib  # noqa: E402

BUDGET = 80_000  # bytes an icon may take: text outlines are most of it
APPS = os.path.join(ROOT, "config", "apps")
THEME = os.path.join(ROOT, "config", "icons", "inua-objects")
ICONS_DIR = os.path.join(ROOT, "config", "icons")
SHEET = os.path.join(ROOT, "bin", "icon-sheet")

INDEX = """[Icon Theme]
Name=inua objects
Comment=Apps drawn as the objects they stand for, in inua's style
Inherits=Papirus,Numix-Circle,Adwaita,hicolor
Directories=scalable/apps,16/apps

[scalable/apps]
Size=256
MinSize=17
MaxSize=512
Type=Scalable
Context=Applications

[16/apps]
Size=16
Type=Fixed
Context=Applications
"""

# The soft and flat sets inherit the objects set, so an app not drawn in
# them has its object, and share its 16 px drawings (a link to its 16
# directory). Each: the drawing it renders, its theme's name and title.
SETS = {
    "soft": ("inua-objects-soft", "inua objects soft", "more simply"),
    "flat": ("inua-objects-flat", "inua objects flat", "flat"),
}


def set_index(title, how):
    return INDEX.replace("Name=inua objects\n", f"Name={title}\n").replace(
        "in inua's style\n", f"{how}, in inua's style\n").replace(
        "Inherits=", "Inherits=inua-objects,")


LICENSE = """The drawings in this theme are inua's (https://github.com/lyricat/inua),
under the same licence as inua. The apps' names and marks shown in them
remain their owners' trademarks; they are drawn here, simplified, only to
name the apps on a desktop, as icon themes do.

The Firefox icons are drawn from Firefox's own logo, under the Mozilla
Public License 2.0 (https://mozilla.org/MPL/2.0/), as their source is,
design/icons/brands/firefox-2019.svg; Firefox is a trademark of the Mozilla
Foundation. The Google Chrome icons are drawn from Chrome's icon, in the
public domain (design/icons/brands/chrome-2022.svg); Google Chrome is a
trademark of Google LLC. The Chromium icons are drawn from Chromium's icon
(design/icons/brands/chromium.svg): Copyright (c) The Chromium Projects,
redistributed with or without modification under the BSD 3-clause licence
(https://chromium.googlesource.com/chromium/src/+/main/LICENSE), provided
"as is", without warranty of any kind. The Mister Morph icons are drawn
from Mister Morph's own icon in its bunko colours
(design/icons/brands/mistermorph.svg, from
https://github.com/quailyquaily/mistermorph), under the Apache License 2.0
(https://www.apache.org/licenses/LICENSE-2.0): its disc and lines as they
are, with inua's light and depth added; Mister Morph's mark remains its
owner's trademark.

The text in the drawings is set in IBM Plex (Copyright 2017 IBM Corp.) and
Caveat (Copyright 2014 The Caveat Project Authors), both under the SIL Open
Font License 1.1, and turned into outlines.
"""


def load_drawings():
    """Loads every drawing module; one that fails is reported and left
    out, so the others still build."""
    failed = []
    for f in sorted(glob.glob(os.path.join(HERE, "drawings", "*.py"))) + sorted(glob.glob(os.path.join(HERE, "soft", "*.py"))):
        pkg = os.path.basename(os.path.dirname(f))
        spec = importlib.util.spec_from_file_location(pkg + "." + os.path.basename(f)[:-3], f)
        mod = importlib.util.module_from_spec(spec)
        try:
            spec.loader.exec_module(mod)
        except Exception as e:  # noqa: BLE001 - a drawing being written
            failed.append(f"{os.path.basename(f)}: {e!r}")
    return failed


def render(entry, which):
    size = 16 if which == "small" else 256
    ic = lib.Icon(entry["name"] + {"draw": "", "soft": "-soft", "flat": "-flat", "small": "-16"}[which],
                  size, flat=which == "flat")
    draw = entry.get("flat", entry.get("soft")) if which == "flat" else entry[which]
    draw(ic)
    return ic.svg()


def check(name, svg):
    problems = []
    if len(svg.encode()) > BUDGET:
        problems.append(f"{len(svg.encode())} bytes, over {BUDGET}")
    if "<filter" in svg or "filter=" in svg:
        problems.append("a filter")
    if "<text" in svg:
        problems.append("live text (use ic.text, which outlines it)")
    ids = re.findall(r'\bid="([^"]+)"', svg)
    if len(ids) != len(set(ids)):
        problems.append("ids used twice")
    for ref in set(re.findall(r"url\(#([^)]+)\)", svg)):
        if ref not in ids:
            problems.append(f"no #{ref}")
    return [f"{name}: {p}" for p in problems]


def write(path, text):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)


def relink(target, path):
    if os.path.lexists(path):
        if os.path.islink(path) and os.readlink(path) == target:
            return
        if os.path.isdir(path) and not os.path.islink(path):
            shutil.rmtree(path)
        else:
            os.remove(path)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    try:
        os.symlink(target, path)
    except FileExistsError:  # another build at the same time
        pass


def main(argv):
    global SHEET
    sheet = "--sheet" in argv
    show = next((k for k in SETS if f"--{k}" in argv), None)  # the set on the sheet
    for a in argv:
        if a.startswith("--sheet-dir="):
            SHEET, sheet = os.path.abspath(a.split("=", 1)[1]), True
    only = {a.lower() for a in argv if not a.startswith("--")}
    problems = [f"module {m}" for m in load_drawings()]
    entries = [e for e in lib.ICONS.values() if "draw" in e and (not only or e["name"].lower() in only)]
    if only and not entries:
        sys.exit(f"no icon named {', '.join(sorted(only))}")
    built = []
    for e in sorted(entries, key=lambda e: (e["kind"], e["name"].lower())):
        try:
            big = render(e, "draw")
            tiny = render(e, "small") if "small" in e else None
        except Exception as err:  # noqa: BLE001
            problems.append(f"{e['name']}: {err}")
            continue
        drawn = {}
        for which, (theme, _, _) in SETS.items():
            if "soft" not in e and which not in e:
                continue
            try:
                svg = render(e, which)
            except Exception as err:  # noqa: BLE001
                problems.append(f"{e['name']} ({which}): {err}")
                continue
            drawn[which] = svg
            problems += check(f"{e['name']} ({which})", svg)
            apps = os.path.join(ICONS_DIR, theme, "scalable", "apps")
            base = f"io.github.lyricat.Inua.{e['name']}" if e["kind"] == "inua" else e["name"]
            write(os.path.join(apps, base + ".svg"), svg)
            for alias in e["names"] if e["kind"] == "app" else ():
                if alias != e["name"]:
                    relink(e["name"] + ".svg", os.path.join(apps, alias + ".svg"))
        problems += check(e["name"], big)
        if tiny:
            problems += check(e["name"] + " (16 px)", tiny)
        if e["kind"] == "inua":
            base = f"io.github.lyricat.Inua.{e['name']}.svg"
            write(os.path.join(APPS, base), big)
            if tiny:
                write(os.path.join(APPS, "small", base), tiny)
        else:
            write(os.path.join(THEME, "scalable", "apps", e["name"] + ".svg"), big)
            if tiny:
                write(os.path.join(THEME, "16", "apps", e["name"] + ".svg"), tiny)
            for alias in e["names"]:
                for d, has in (("scalable", True), ("16", tiny is not None)):
                    if not has or alias == e["name"]:
                        continue
                    link = os.path.join(THEME, d, "apps", alias + ".svg")
                    if os.path.lexists(link):
                        os.remove(link)
                    os.symlink(e["name"] + ".svg", link)
        built.append((e, drawn.get(show) if show else big, tiny))
    if show:
        built = [b for b in built if b[1]]
    if any(e["kind"] == "app" for e, _, _ in built):
        write(os.path.join(THEME, "index.theme"), INDEX)
        write(os.path.join(THEME, "LICENSE"), LICENSE)
    if any("soft" in e for e in entries):
        for theme, title, how in SETS.values():
            write(os.path.join(ICONS_DIR, theme, "index.theme"), set_index(title, how))
            write(os.path.join(ICONS_DIR, theme, "LICENSE"), LICENSE)
            relink(os.path.join("..", "inua-objects", "16"), os.path.join(ICONS_DIR, theme, "16"))
    for p in problems:
        print("problem:", p, file=sys.stderr)
    print(f"built {len(entries)} icons ({sum(1 for e in entries if 'small' in e)} with a 16 px drawing, "
          f"{sum(1 for e in entries if 'soft' in e)} soft and flat)")
    if sheet or only:
        make_sheet(built)
    return 1 if problems else 0


def make_sheet(built):
    """A contact sheet: each icon at 256, 64, 32, 24 and 16 px, on both of
    inua's grounds, as HTML and, with Chrome, a PNG."""
    if os.path.isdir(SHEET):
        shutil.rmtree(SHEET)
    os.makedirs(SHEET)
    cells = []
    for e, big, tiny in built:
        write(os.path.join(SHEET, e["name"] + ".svg"), big)
        if tiny:
            write(os.path.join(SHEET, e["name"] + "-16.svg"), tiny)
        small = e["name"] + ("-16.svg" if tiny else ".svg")
        sizes = "".join(f'<img src="{e["name"]}.svg" width="{s}" height="{s}">' for s in (64, 32, 24))
        cells.append(f'<figure><img src="{e["name"]}.svg" width="160" height="160"><div class="sizes">{sizes}'
                     f'<img src="{small}" width="16" height="16"></div><figcaption>{e["title"]}</figcaption></figure>')
    html = f"""<!doctype html><meta charset="utf-8"><style>
body{{margin:0;font:13px/1.4 "IBM Plex Sans",sans-serif}}
section{{display:flex;flex-wrap:wrap;gap:22px;padding:28px}}
.dark{{background:#0E161B;color:#E7E4DA}} .light{{background:#EAE3D4;color:#1E1E20}}
figure{{margin:0;width:170px;display:flex;flex-direction:column;align-items:center;gap:8px}}
.sizes{{display:flex;align-items:flex-end;gap:8px}} figcaption{{opacity:.75}}
</style><section class="dark">{''.join(cells)}</section><section class="light">{''.join(cells)}</section>"""
    write(os.path.join(SHEET, "index.html"), html)
    chrome = shutil.which("google-chrome") or shutil.which("chromium")
    if chrome:
        rows = (len(built) + 5) // 6
        h = 2 * (rows * 300 + 56)
        subprocess.run([chrome, "--headless=new", "--disable-gpu", f"--window-size=1240,{h}",
                        "--virtual-time-budget=3000", f"--screenshot={os.path.join(SHEET, 'sheet.png')}",
                        "file://" + os.path.join(SHEET, "index.html")],
                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
    print("sheet:", os.path.join(SHEET, "index.html"))


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
