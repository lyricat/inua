---
source: pr #93
branch: pr/focus-ring-border
round: 2
---
# fix(bar): match the other monitor's focus ring to the focused border

## factory · 2026-11-10 08:36
Imported GitHub PR #93 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/93
Upstream head at import: cdbc2ad761e065260337789f2015fb05a9f9f48b
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## Lyric · 2026-11-10 09:53
先把 FT92~FT100移动到 backlog


## factory · 2026-11-10 20:37
Owner requested moving all current backlog items to new: “把 backlog 这些放到 new，注意他们的分支也在 fork 上”。
Moved backlog → new; no build or review performed in this update.
Verified source branch: `iamcheyan/inua:pr/focus-ring-border` (fork), head `cdbc2ad761e065260337789f2015fb05a9f9f48b`.
The existing branch header names the fork branch, not an origin branch. For the next build, fetch `refs/pull/93/head` from origin to obtain the PR head; continue from that code rather than assuming `origin/pr/focus-ring-border` exists or starting again from master. Preserve the existing PR work and branch identity; do not push to the contributor's fork in this board update.


## factory · 2026-11-10 21:01
Owner corrected the requested destination: “诶不对，他们应该放到 review”。
Moved new → review at the owner’s request; no build, review or tests performed in this update.
Review the existing fork PR implementation using the source PR head (refs/pull/<PR>/head); the branch header is a fork branch, not necessarily an origin branch. The preceding instruction to build is superseded by this review request.

## factory · review · 2026-11-10 21:22
head: cdbc2ad761e065260337789f2015fb05a9f9f48b
verdict: fix
fix:
- [P1] internal/ui/focusring.go:42-44: Decode Hyprland's custom JSON field, not gradient/color. Example: {"option":"general:col.active_border","custom":"ee33ccff ee00ff99 45deg","set":true} leaves both current fields empty and selects fallback #89b4fa instead of the active border colour. Add a regression test using this reply shape. Installed Hyprland getoption format strings and internal/hypr/room.go corroborate the contract; no live session was queried.
notes:
- [P2] internal/ui/shell.go:796: configreloaded never refreshes the new cached ring colour. Once reply decoding is fixed, changing col.active_border in hyprland.conf leaves the old colour until inua reload/restart. Refresh it in the existing event handler.

Claude Code reviewed origin/master...HEAD (three files, merge base bf807607). Isolated make test passed: 18 Python tests, go vet, 115 Go race-test packages. No installs or code changes. No visual UI check. Reply shape checked against installed binary format strings, not live Hyprland. make i18n was not successfully run; no changed translatable strings. Speculative low-alpha parsing and generic blocking-I/O concerns excluded from confirmed findings.

Fetched PR #93 head per owner request; merge-tree against origin/master passed. Published the unchanged head to origin/pr/focus-ring-border, preserving the recorded branch identity for the next fix build; contributor fork unchanged. Initial push credential-path error resolved on one retry with command-local credential.useHttpPath=true. Round 1 of 3: review/ → new/.

## factory · plan · 2026-11-10 22:04
Fix round 2.
Behaviour: Decode the real getoption custom field, choose the first gradient stop and preserve alpha. Empty, invalid or missing replies and unavailable Hyprland retain the original blue. Refresh cached colour and existing rings on configreloaded.
Scope: Correct the decoder and reload path and add regression tests; keep the PR's square corners and border width. No unrelated changes.
Decisions: Use the actual custom contract rather than invented gradient/color fields. Read on the existing background event handler and repaint on the GTK main thread.
Checking: Test the review's exact JSON, first-stop alpha and invalid/empty replies. Run make i18n and isolated make test. Inspect reload wiring; visual verification remains for the owner.

## factory · 2026-11-10 22:04
Claude Code completed fix round 2 in a62741f9 on pr/focus-ring-border; pushed to origin, contributor fork unchanged. Fixed P1 decoding and P2 cached colour refresh, including repainting existing rings. Added focusring_test.go regression coverage.
Verification: Manager checked commits and protected paths; only original PR work and this fix are ahead of base, and no never paths changed. Merge check against origin/master (3478c014) clean. Both coder and manager ran make test with throwaway HOME and isolated session environment: passed (Python tests, go vet, Go race tests). make i18n ran before commit with no catalog changes; no Blueprint changes, so make generate unnecessary.
Installs: none.
Not checked: No visual UI check or live Hyprland query. Reload wiring inspected in code, not exercised by automated integration tests; unavailable-Hyprland fallback not integration-tested.
new/ → review/; round 2 of 3. Awaiting review, not approved for merge.


## factory · review · 2026-11-10 22:37
head: a62741f979e80ae4f6032a2545ce97d0fd6f2fe1
verdict: pass
notes:
- Claude Code reviewed origin/master...HEAD: four files, two commits, merge base bf807607; base 3478c014. No confirmed introduced defects. Previous P1 custom-field decoding and P2 configreloaded cache refresh/repaint are fixed.
- Required make test passed with throwaway HOME and isolated session environment: 18 Python tests, go vet, 115 Go race-test packages (some cached). Fresh TestActiveBorderColor race tests also passed. No tracked changes or installs.
- make i18n in a throwaway copy was blocked by the sandbox with Function not implemented; no changed translatable strings or catalog references to changed files were found. The build previously ran make i18n successfully.
- No visual UI check or live Hyprland query. Reload repaint and unavailable-Hyprland behavior were inspected, not integration-tested. Low-alpha unpadded reply formatting remains unverified, not a confirmed finding.

Merge check against origin/master passed. review/ → ready/; no code-branch push, merge to master or release.
