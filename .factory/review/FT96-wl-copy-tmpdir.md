---
source: pr #92
branch: factory/FT96-wl-copy-tmpdir
round: 1
---
# fix(screenshot): keep wl-copy working when TMPDIR is gone

## factory · 2026-11-10 08:36
Imported GitHub PR #92 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/92
Upstream head at import: 5bda2a3ea929c7be1a23d52e65ccd85695f6993a
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog

## factory · 2026-11-10 10:41
Moved from backlog to new at the owner's request: “那把 FT96 放入 new，也是 fork 分支。”
Confirmed PR #92 is open and its head is iamcheyan/inua:pr/wl-copy-tmpdir (5bda2a3ea929c7be1a23d52e65ccd85695f6993a). Removed the imported fork-only branch header so build uses source PR #92, fetches origin pull/92/head, and creates factory/FT96-wl-copy-tmpdir instead of looking for the fork branch on origin.
Queue update only; no build, tests, review or merge performed.

## factory · plan · 2026-11-10 11:08
Behaviour: keep normal screenshot copying and daemon-safe diagnostic files. If the default temporary directory is unavailable, use XDG_RUNTIME_DIR, or /tmp when unset. Return useful errors when creation or wl-copy fails, and clean diagnostics on success and failure. No first-run or settings changes.
Scope: PR #92's screenshot fallback, focused regression tests, and generated catalogs. Other clipboard callers and UI redesign are excluded.
Decisions: preserve the PR's narrow fallback rather than changing process environment or using rundir (which can also depend on missing TMPDIR). Do not retry the same failed directory.
Checking: fake wl-copy tests for valid/deleted TMPDIR, runtime fallback, unset runtime, fallback and command errors, arguments/input and cleanup; make i18n and make test. No live clipboard access.

## factory · 2026-11-10 11:08
Built from PR #92 head 5bda2a3ea929c7be1a23d52e65ccd85695f6993a with Claude Code. Merged current master in 05e32947; catalog reference conflicts resolved by regeneration. Implementation and regression tests committed as 7c5223b6 and pushed to factory/FT96-wl-copy-tmpdir.
The fallback now retains both directory errors when both fail, and avoids retrying the same directory. Added isolated fake-command tests and regenerated catalogs.
Tests: make i18n passed; targeted race tests passed three times; full make test passed for coder and independently for manager (Python session tests, go vet, Go race tests). No forbidden paths changed.
Installs: none.
Not checked: no real screenshot/clipboard test or visual UI check in a live or nested session; unwritable system /tmp case not simulated. No visual UI change.
Isolation note: coder reported its first full test used the existing user cache directory accidentally; it reran with isolated HOME and all XDG directories. Manager verification likewise used isolated HOME/XDG directories.
Moved new/ → review/. Awaiting review; not merged into master.
