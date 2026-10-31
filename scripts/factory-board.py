#!/usr/bin/env python3
"""Render a repository's .factory/ board as one static HTML page.

A copy of the factory skill's scripts/board.py, kept here for CI
(.github/workflows/factory-board.yml).

    scripts/factory-board.py [REPO] [-o board.html]

REPO defaults to the current directory. The page has a column per folder,
a card per item (whose turn it is, its branch, its fix round) and, behind
each card, the item's thread and the times it moved between folders, read
from git. It needs only Python 3.11 and git, and loads nothing from the
network. Item files are other people's text: everything is escaped.
"""

import argparse
import datetime as dt
import html
import os
import re
import subprocess
import sys
import tomllib
from zoneinfo import ZoneInfo

FOLDERS = ["new", "blocked", "building", "review", "ready", "done", "stop"]
ITEM = re.compile(r"^(\d{4})-([a-z0-9]+(?:-[a-z0-9]+)*)\.md$")
SECTION = re.compile(r"^## (.+?) · (?:(plan) · )?(\d{4}-\d{2}-\d{2} \d{2}:\d{2})\s*$")
DONE_SHOWN = 12
TZ = None  # factory.toml's timezone; the machine's own when unset


def git(repo, *args):
    r = subprocess.run(["git", "-C", repo, *args], capture_output=True, text=True)
    return r.stdout if r.returncode == 0 else ""


def parse(text):
    """The header, title and sections of an item file, and what is wrong with it."""
    lines = text.splitlines()
    header, problems, i = {}, [], 0
    if lines and lines[0].strip() == "---":
        for j in range(1, len(lines)):
            if lines[j].strip() == "---":
                for line in lines[1:j]:
                    key, sep, value = line.partition(":")
                    if sep:
                        header[key.strip()] = value.strip()
                i = j + 1
                break
        else:
            problems.append("the header has no closing ---")
    while i < len(lines) and not lines[i].strip():
        i += 1
    title = ""
    if i < len(lines) and lines[i].startswith("# "):
        title = lines[i][2:].strip()
        i += 1
    else:
        problems.append("no # title line")
    sections, current = [], None
    for line in lines[i:]:
        if line.startswith("## "):
            m = SECTION.match(line)
            if not m:
                problems.append("a section heading not in the form ## name · YYYY-MM-DD HH:MM")
                current = {"who": line[3:].strip(), "plan": False, "time": "", "body": []}
            else:
                current = {"who": m.group(1), "plan": bool(m.group(2)), "time": m.group(3), "body": []}
            sections.append(current)
        elif current is not None:
            current["body"].append(line)
        elif line.strip():
            problems.append("text before the first section")
    if not sections:
        problems.append("no sections")
    for s in sections:
        s["body"] = "\n".join(s["body"]).strip("\n")
    return header, title, sections, problems


def moves(repo, path):
    """When the item entered each folder: [(folder, unix time)], oldest first."""
    out = git(repo, "log", "--follow", "--name-only", "--format=%x00%ct", "--", path)
    seen = []
    for chunk in out.split("\x00")[1:]:
        parts = chunk.strip().splitlines()
        if len(parts) < 2:
            continue
        folder = parts[-1].split("/")[-2] if "/" in parts[-1] else ""
        seen.append((folder, int(parts[0])))
    seen.reverse()
    steps = []
    for folder, t in seen:
        if not steps or steps[-1][0] != folder:
            steps.append((folder, t))
    return steps


def turn(folder, sections, owners):
    """Who acts next: "you", "agent" or "" (nobody)."""
    if folder in ("done", "stop"):
        return ""
    if folder in ("new", "building"):
        return "agent"
    last_factory = max((i for i, s in enumerate(sections) if s["who"] == "factory"), default=-1)
    owner_after = any(s["who"] in owners for s in sections[last_factory + 1:])
    if owner_after:
        return "agent"
    return "you"


def branch_link(repo, remote, base, branch):
    url = git(repo, "remote", "get-url", remote).strip()
    m = re.match(r"^(?:git@github\.com:|https://github\.com/)([\w.-]+/[\w.-]+?)(?:\.git)?$", url)
    if not m or not branch:
        return ""
    return f"https://github.com/{m.group(1)}/compare/{base}...{branch}"


def when(t):
    return dt.datetime.fromtimestamp(t, TZ).strftime("%Y-%m-%d %H:%M") if t else ""


def body_html(text):
    out = html.escape(text)
    out = re.sub(r"`([^`\n]+)`", r"<code>\1</code>", out)
    out = re.sub(r"(https://[^\s<&]+(?:&amp;[^\s<&]+)*)", r'<a href="\1" rel="noopener">\1</a>', out)
    return out


def load(repo):
    cfg_path = os.path.join(repo, ".factory", "factory.toml")
    with open(cfg_path, "rb") as f:
        cfg = tomllib.load(f)
    global TZ
    if cfg.get("timezone"):
        TZ = ZoneInfo(cfg["timezone"])
    owners = set(cfg.get("owners", []))
    base, remote = cfg.get("base", "master"), cfg.get("remote", "origin")
    items = []
    for folder in FOLDERS:
        d = os.path.join(repo, ".factory", folder)
        if not os.path.isdir(d):
            continue
        for name in sorted(os.listdir(d)):
            m = ITEM.match(name)
            if not m:
                continue
            path = os.path.join(".factory", folder, name)
            with open(os.path.join(repo, path), encoding="utf-8", errors="replace") as f:
                header, title, sections, problems = parse(f.read())
            steps = moves(repo, path)
            items.append({
                "n": int(m.group(1)), "slug": m.group(2), "folder": folder,
                "title": title or m.group(2).replace("-", " "),
                "branch": header.get("branch", ""), "round": header.get("round", ""),
                "sections": sections, "problems": problems, "steps": steps,
                "changed": steps[-1][1] if steps else 0,
                "turn": turn(folder, sections, owners),
                "link": branch_link(repo, remote, base, header.get("branch", "")),
            })
    name = os.path.basename(os.path.abspath(repo))
    return cfg, name, items


CSS = """
:root{--bg:#f3f4f7;--surface:#fff;--fg:#1c1f2a;--muted:#5d6273;--line:#d9dce5;--accent:#5319e7;--accent-soft:#ece6fd;
--you:#b4530f;--you-soft:#fbeee3;--ok:#1d7a4c;--warn:#b42318;--code:#eceef3;
--sans:system-ui,-apple-system,"Segoe UI",sans-serif;--mono:ui-monospace,"SFMono-Regular",Menlo,Consolas,monospace}
@media (prefers-color-scheme:dark){:root:not([data-theme=light]){--bg:#14161d;--surface:#1c1f28;--fg:#e4e6ee;--muted:#9aa0b3;--line:#2f3340;
--accent:#a98bff;--accent-soft:#2a2245;--you:#f0a060;--you-soft:#3a2a1c;--ok:#6fd0a0;--warn:#ff8a7a;--code:#232733;color-scheme:dark}}
:root[data-theme=dark]{--bg:#14161d;--surface:#1c1f28;--fg:#e4e6ee;--muted:#9aa0b3;--line:#2f3340;
--accent:#a98bff;--accent-soft:#2a2245;--you:#f0a060;--you-soft:#3a2a1c;--ok:#6fd0a0;--warn:#ff8a7a;--code:#232733;color-scheme:dark}
*{box-sizing:border-box}
body{margin:0;background:var(--bg);color:var(--fg);font:15px/1.5 var(--sans);padding:28px max(16px,env(safe-area-inset-left)) 60px}
header{max-width:1400px;margin:0 auto 22px;display:flex;flex-wrap:wrap;gap:8px 24px;align-items:baseline}
h1{font-size:1.6rem;margin:0}
.meta{color:var(--muted);font-size:.9rem}
.sum{display:flex;gap:14px;flex-wrap:wrap;font-size:.9rem}
.sum b{font-family:var(--mono)}
.board{max-width:1400px;margin:0 auto;display:grid;grid-template-columns:repeat(7,minmax(180px,1fr));gap:12px;overflow-x:auto;padding-bottom:6px}
.col{display:flex;flex-direction:column;gap:8px;min-width:0}
.col h2{font:600 .78rem/1 var(--mono);letter-spacing:.1em;text-transform:uppercase;color:var(--muted);margin:0;padding:6px 2px;display:flex;justify-content:space-between}
.col.wait h2{color:var(--you)}
.card{display:flex;flex-direction:column;gap:6px;background:var(--surface);border:1px solid var(--line);border-radius:4px;padding:10px 12px;color:inherit;text-decoration:none}
.card:hover,.card:focus-visible{border-color:var(--accent);outline:none}
.card .t{font-weight:600;line-height:1.3}
.card .n{font:600 .78rem var(--mono);color:var(--muted)}
.row{display:flex;flex-wrap:wrap;gap:6px;align-items:center;font-size:.78rem;color:var(--muted)}
.chip{font:600 .68rem/1 var(--mono);letter-spacing:.05em;text-transform:uppercase;padding:4px 6px;border-radius:2px}
.chip.you{background:var(--you-soft);color:var(--you)}
.chip.agent{background:var(--accent-soft);color:var(--accent)}
.chip.bad{background:var(--you-soft);color:var(--warn)}
.empty{color:var(--muted);font-size:.85rem;font-style:italic;padding:2px}
.more{color:var(--muted);font-size:.82rem;padding:2px}
.detail{display:none;max-width:860px;margin:28px auto 0;background:var(--surface);border:1px solid var(--line);border-radius:4px;padding:20px 22px}
.detail:target{display:block}
.detail .top{display:flex;justify-content:space-between;gap:12px;align-items:baseline}
.detail h2{margin:0;font-size:1.3rem}
.close{font-size:.9rem;color:var(--accent)}
.facts{display:flex;flex-wrap:wrap;gap:6px 18px;font-size:.88rem;color:var(--muted);margin:10px 0 0}
.facts code,.sec code{font-family:var(--mono);font-size:.85em;background:var(--code);padding:.05em .3em;border-radius:2px}
.steps{display:flex;flex-wrap:wrap;gap:4px 8px;margin:14px 0 4px;font:.8rem var(--mono);color:var(--muted)}
.steps b{color:var(--fg)}
.problems{color:var(--warn);font-size:.88rem;margin:10px 0 0}
.sec{border-top:1px solid var(--line);padding:12px 0 2px}
.sec .h{font:600 .8rem var(--mono);display:flex;gap:10px}
.sec.you .h{color:var(--you)}
.sec.agent .h{color:var(--accent)}
.sec .h span{color:var(--muted);font-weight:400}
.sec .b{white-space:pre-wrap;overflow-wrap:anywhere;margin-top:4px;font-size:.94rem}
a{color:var(--accent)}
@media (max-width:760px){.board{grid-template-columns:repeat(7,minmax(240px,80vw))}}
"""


def render(cfg, name, items):
    owners = set(cfg.get("owners", []))
    now = dt.datetime.now(TZ).strftime("%Y-%m-%d %H:%M")
    by = {f: [] for f in FOLDERS}
    for it in items:
        by[it["folder"]].append(it)
    for f in FOLDERS:
        by[f].sort(key=lambda it: -it["changed"] if f == "done" else it["changed"])
    yours = sum(1 for it in items if it["turn"] == "you")
    agents = sum(1 for it in items if it["turn"] == "agent")
    e = html.escape
    out = [
        "<!doctype html><html lang=en><head><meta charset=utf-8>",
        '<meta name=viewport content="width=device-width,initial-scale=1">',
        f"<title>{e(name)} · factory board</title><style>{CSS}</style></head><body>",
        f"<header><h1>{e(name)}</h1><span class=meta>factory board · built {now}</span>",
        f'<span class=sum><span><b>{yours}</b> waiting for you</span><span><b>{agents}</b> for the agent</span>'
        f"<span><b>{len(by['done'])}</b> done</span></span></header>",
        '<main class=board>',
    ]
    for f in FOLDERS:
        wait = " wait" if f in ("blocked", "review", "ready") else ""
        shown = by[f][:DONE_SHOWN] if f == "done" else by[f]
        out.append(f'<section class="col{wait}"><h2><span>{f}/</span><span>{len(by[f])}</span></h2>')
        if not shown:
            out.append("<span class=empty>empty</span>")
        for it in shown:
            chips = ""
            if it["turn"]:
                label = "you" if it["turn"] == "you" else "agent"
                chips += f'<span class="chip {label}">{label}</span>'
            if it["problems"]:
                chips += '<span class="chip bad">check file</span>'
            rnd = f"round {e(it['round'])}" if it["round"] not in ("", "0") else ""
            out.append(
                f'<a class=card href="#item-{it["n"]}"><span class=n>#{it["n"]}</span>'
                f'<span class=t>{e(it["title"])}</span>'
                f'<span class=row>{chips}<span>{rnd}</span><span>{when(it["changed"])}</span></span></a>')
        if f == "done" and len(by[f]) > DONE_SHOWN:
            out.append(f"<span class=more>and {len(by[f]) - DONE_SHOWN} more</span>")
        out.append("</section>")
    out.append("</main>")
    for it in items:
        facts = [f"in <code>{it['folder']}/</code>"]
        if it["branch"]:
            b = f"<code>{e(it['branch'])}</code>"
            if it["link"]:
                b = f'<a href="{e(it["link"])}" rel="noopener">{b}</a>'
            facts.append("branch " + b)
        if it["round"]:
            facts.append(f"fix round {e(it['round'])}")
        whose = {"you": "your turn", "agent": "the agent's turn", "": "nobody's turn"}[it["turn"]]
        facts.append(whose)
        steps = " → ".join(f"<b>{e(fo)}/</b> {when(t)}" for fo, t in it["steps"])
        out.append(
            f'<article class=detail id="item-{it["n"]}"><div class=top><h2>#{it["n"]} {e(it["title"])}</h2>'
            f'<a class=close href="#">Close</a></div><div class=facts>{"<span>" + "</span><span>".join(facts) + "</span>"}</div>'
            f'<div class=steps>{steps}</div>')
        if it["problems"]:
            out.append("<p class=problems>The file breaks the format: " + e("; ".join(sorted(set(it["problems"])))) + ".</p>")
        for s in it["sections"]:
            side = "agent" if s["who"] == "factory" else ("you" if s["who"] in owners else "")
            kind = " · plan" if s["plan"] else ""
            out.append(
                f'<div class="sec {side}"><div class=h>{e(s["who"])}{kind}<span>{e(s["time"])}</span></div>'
                f'<div class=b>{body_html(s["body"])}</div></div>')
        out.append("</article>")
    out.append("</body></html>\n")
    return "".join(out)


def main():
    ap = argparse.ArgumentParser(description="Render a .factory/ board as one HTML page.")
    ap.add_argument("repo", nargs="?", default=".", help="the repository (default: the current directory)")
    ap.add_argument("-o", "--out", help="write here instead of to standard output")
    a = ap.parse_args()
    if not os.path.isfile(os.path.join(a.repo, ".factory", "factory.toml")):
        sys.exit(f"board.py: {a.repo} has no .factory/factory.toml")
    page = render(*load(a.repo))
    if a.out:
        with open(a.out, "w", encoding="utf-8") as f:
            f.write(page)
    else:
        sys.stdout.write(page)


if __name__ == "__main__":
    main()
