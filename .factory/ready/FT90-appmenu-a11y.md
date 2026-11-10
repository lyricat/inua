---
source: pr #74
branch: feat/appmenu-a11y
round: 1
---
# feat(appmenu): read menus through the accessibility tree

## factory · 2026-11-10 08:36
Imported GitHub PR #74 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/74
Upstream head at import: d910da2e0f70ed46172a0e443193135855ebc7ea
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## factory · review · 2026-11-10 09:09
head: 5dfc7a8488bb64f72b2e6cbd6973b00eb883bcd2
verdict: pass
notes:
- [P2] internal/appmenu/atspi.go:487-489: Checked and radio entries use Toggle="check", but the UI recognizes "checkmark" and "radio"; checked Show Toolbar renders without a tick. Emit the matching distinct toggle names.
- [P2] internal/appmenu/atspi.go:198-205: withOpen only annotates direct children. File > Open Recent > notes.txt retains PID=0 and no reopen chain, so activation targets an object after its parent menu closes. Recursively attach the PID and full reopen chain.
- [P3] internal/ui/appmenu_a11y.go:93-100: An unfilled nested submenu becomes a plain action; Prefilled filters only the top level, allowing never_press to be bypassed by clicking a nested Zoom submenu. Preserve submenu identity and recursively drop or disable unfilled nodes under never_press.
- [P3] internal/appmenu/atspi.go:430: The node counter is passed by value and measures depth rather than total nodes; a 5000-item menu passes the 4096-node bound. Share one counter across the traversal.
- [P3] internal/appmenu/atspi.go:93-95: source returns a cached disconnected accessibility bus after daemon restart; subsequent calls fail until shell restart. Check Connected(), clear and re-dial.

Claude Code reviewed all 20 changed files in origin/master...HEAD against base a6787817, including merge integration and untrusted D-Bus input/action dispatch. No P0/P1 findings; moved review/ → ready/ under the factory threshold. P2/P3 notes remain for owner consideration.

Merged master into the PR head, resolved source/catalog conflicts, and pushed 5dfc7a84 to origin/feat/appmenu-a11y. PR #74 is from a fork: its head was fetched through pull/74/head; the merged review branch is now on lyricat/inua, not on the author's fork. No force push or base push.

Validation: isolated make test passed (18 Python tests, go vet, 115 Go packages under race detector; 36 cached). Manager's initial run hit TestBodyStructureMessagePart: unexpected EOF; reproduced on unchanged master and traced to the fake IMAP server CAPABILITY/FETCH race. Subsequent full runs passed; unrelated flake not changed. Five findings were verified with temporary fake-tree/UI/private-bus probes. Catalog regeneration matched committed catalogs; gofmt clean. No installs.

Not checked visually or in a real/nested desktop: Firefox/Electron/Qt/GTK interoperability. Firefox derivatives may miss never_press matching; conflicting Firefox activation safety comments remain unresolved. Large-tree latency and accessibility bus autostart were not verified. Duplicate-label activation ambiguity has no concrete app example and is not a finding.
