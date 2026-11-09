---
source: pr #89
branch: factory/FT89-japanese-translations
round: 1
---
# i18n(ja): complete and correct Japanese translations

## factory · 2026-11-09 19:52
Imported GitHub PR #89 directly into review at the owner's request: “把 89 放到 review”.
Source: https://github.com/lyricat/inua/pull/89
Upstream head: 3ca8b3235eea20126bdd34cc073d259ff4216f47
Factory branch preserves the upstream head without changes.

Completes the Japanese catalog and corrects mistranslations and context errors including Right, Hide, Hang Up, Quit with Children, MEMORY, Traffic, Up, and Resident.
Review should check translation context, terminology, placeholders, markup, completeness, generated catalog consistency and regressions against the current base.

The author reports 4,920 translated entries, none remaining, regenerated catalogs via make i18n, and passing make build, go test ./internal/i18n and git diff --check. These claims were not independently verified during import.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-09 20:17
head: 214eebe9f0632907ffc22b60ed1ccff9c4e95f72
verdict: fix
fix:
- [P1] internal/ui/bar.go:262: The app title button binds an unassigned onAppTitle callback; emitting clicked reproduces a nil callback panic. Implement and assign the handler before binding.
- [P1] internal/hyprconf/hyprconf_test.go:514: TestApplyReloadsLua creates its Unix socket under t.TempDir(); with a 117-byte TMPDIR, make test fails with `bind: invalid argument`, while the same baseline run passes. Use sockdir.New(t).
notes:
- [P2] internal/ui/appmenu_packs.go:54: With same-PID windows A and B and B focused, Close Window targets A. Pass the focused window ID and include it in refresh conditions.
- [P2] internal/i18n/locales/ja.po:10237: Up now renders as 稼働時間 in Files parent-folder navigation. Separate uptime and navigation message keys.
- [P2] internal/i18n/locales/ja.po:1478: Files List view renders as メーリングリスト. Separate mailing-list and list-view message keys.
- [P2] internal/i18n/locales/ja.po:8241: Lobby arguments (2, 10, 8) render as 2 total tables and 10 playing. Use indexed formatting: total %[2]d, playing %[1]d, empty %[3]d.
- [P3] internal/i18n/locales/ja.po:16205: Shortcut capture is translated as audio recording (録音中). Translate as shortcut registration.

Merged origin/master at bf807607 into the item branch, resolved generated catalog reference conflicts, and pushed merge 214eebe9 before review. Review scope was origin/master...HEAD: 112 files, not only Japanese translations; 38 backup binaries were inspected only for metadata, not executed.

Validation: normal isolated make test passed (18 Python tests, go vet, Go race tests). Long-TMPDIR make test failed on this head but passed on baseline. Isolated Broadway probes reproduced the nil callback panic and wrong-window target. make i18n reproduced catalogs byte-for-byte; Blueprint compilation matched committed UI files. Japanese catalog has 4,920 entries, no empty/fuzzy/duplicate entries. git diff --check passed. Initial PID-isolation failures were environmental and excluded after correcting the test environment.

No installs. No visual UI or real Hyprland session verification. Binary behavior and third-party application shortcuts remain unchecked. Item stays in review/ for owner decision; add a note and move to new/ for fixes.
