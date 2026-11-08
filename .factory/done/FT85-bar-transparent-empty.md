---
source: pr #85
branch: factory/FT85-bar-transparent-empty
round: 1
---
# feat(bar): make empty workspace panel transparent

## factory · 2026-11-08 19:39
Imported GitHub PR #85 directly into review at the owner's request: “好 85 87 加到 review”.
Source: https://github.com/lyricat/inua/pull/85
Upstream head: 1810a4e28fd06164d847fa6bfdc000721aa7ab67
Factory branch preserves the upstream head without changes.

Adds bar.transparent_empty (default off), persisted in config and exposed in Bar settings. Empty-workspace transparency is evaluated independently per monitor; normal background returns when windows appear. Includes generated UI, catalogs and documentation.

The author's verification claims have not been independently repeated by the factory.
No code review, tests, or visual UI checks were run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-08 20:00
head: 1b5924d9db283ccbaa3df2dbf691baf9027f0240
verdict: pass
notes:
- [P3] ui/style.css:15: With normal density and transparent_empty enabled on an empty workspace, opening the calendar leaves an isolated bar-border line. CSS hides the island border, but drop.hang enables the outline whenever !b.compact and drop.snapshot draws the joined bar/card outline. Make dropdown drawing account for the transparent bar while preserving the card border. Verified from the drawing call chain; not visually reproduced.
- Merged origin/master and resolved translation-catalog conflicts; pushed merge commit 1b5924d. No protected paths changed.
- Codex reviewed all 17 changed files in origin/master...HEAD. No P0/P1 findings.
- make test passed independently for manager and reviewer with isolated HOME and XDG_RUNTIME_DIR: 18 Python tests, go vet, and Go race tests. Initial merge verification lacked XDG_RUNTIME_DIR and failed four hyprconf tests; rerunning with an isolated runtime directory passed.
- make generate and make i18n passed during conflict resolution; reviewer confirmed generated UI and catalog references.
- Installs: none. UI was not visually checked; real multi-monitor rendering and compositor effects remain unverified.
- Moved review/ → ready/; awaiting owner approval, not merged into master.

## factory · 2026-11-08 21:34
Merged into master at 3121e872, at the owner's word: “if all items in ready is fine, merge them to master”. make test passed on the merged master (655b7e89, with FT86). PR #85: already closed (GitHub marked it merged when master took its head). The P3 note on the calendar outline is not fixed.
