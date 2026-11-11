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

## factory · review · 2026-11-11 14:18
head: 38a391c43c7d46aed9d8efb2c7f17b45faa92e49
verdict: pass
Updated from fork PR #89 head e3a44a4270b84ab4e0e475e039ce759c347b3c46 at the owner's request. Merged fork (43ddcbd9) and master 7dd6decf (3ac5903e), resolving 32 conflicted files while retaining master's reviewed implementations. Corrected an integration-introduced title-menu action guard in 38a391c4 with regression coverage. Factory branch pushed; contributor fork unchanged.

Both previous P1 findings are fixed: title-button callback no longer panics, and the hyprconf socket test works under long TMPDIR. Four of five previous lower-priority findings are fixed: focused-window targeting, Files Up, Files List, shortcut capture wording. Lobby argument order remains incorrect.
notes:
- [P2] internal/i18n/locales/ja.po:8315 and internal/i18n/i18n_test.go:58: Lobby (playing=2,total=10,empty=8) still displays 10 playing out of 2 tables; the new test asserts the wrong text.
- [P2] internal/ui/appmenu.go:72/90/126,202: syncAppMenus ignores bar.menu=false; menu updates restore hidden menu sections (probe: 0 to 4 sections).
- [P2] internal/ui/launcher.go:218: branch drops master's 3fdac202 right-click fix, leaking a widget reference and not moving the highlight; static diff verification, not runtime reproduced.
- [P3] Clock placement differs from Settings/config documentation; renamed Up a folder loses existing es/fr/ko/zh_TW translations.

Validation by Claude Code: make test passed (18 Python tests, go vet, 117 Go packages with race detector), isolated HOME; uncached long-TMPDIR run passed at 175 bytes, and affected UI/hyprconf packages rechecked after final correction. Focused title/pack menu race tests passed; regression test fails under old guard. make i18n and make generate reproduced generated files. Format checks across six catalogs found no errors; Japanese has 4,928 translated and 25 untranslated new-master strings, no fuzzy/duplicate entries. Manager verified clean worktree, diff --check and no changed forbidden paths before push.

Scope: combined branch against master, 88 files before final targeted correction, including 38 backup binaries inspected only as metadata, not executed. No installs, visual UI/manual desktop checks, full Japanese proofread, real third-party menu checks, or niri/Wayfire runtime verification. Existing master's screen-recording wording remains outside introduced findings.

Moved review → ready per factory's no-P0/P1 threshold, not a claim that all defects are fixed. No merge to master, release or deployment.
