---
source: pr #79
branch: factory/FT77-bar-clock-text-case
round: 1
---
# fix(bar): keep the clock's text case as written

## factory · 2026-11-07 12:03
Added directly to review at the owner's request: “Move GitHub issue 79, 80, 81,82,83 to review status”. GitHub #79 is an open pull request.

Source: https://github.com/lyricat/inua/pull/79
PR head: f573c0ec7cc8834026492d7830ee1f469afc58d1
Original branch: pr/clock-case
The exact PR head is preserved on `factory/FT77-bar-clock-text-case`. No code changes, review, or tests were performed during this status change. Verification statements below are the PR author's claims, not independently checked here.

PR description (quoted):
> The clock label forced uppercase with CSS, so a locale's mixed or lower-case date was displayed changed. Remove the transform: the text is shown exactly as the clock format writes it.
> 
> Verified by building the shell and loading the style; the clock no longer applies text-transform.

## factory · review · 2026-11-07 12:54
head: f573c0ec7cc8834026492d7830ee1f469afc58d1
verdict: pass
notes:
- Codex reviewed origin/master...HEAD; no findings. The only change removes the clock CSS uppercase transform.
- make test passed: 18 script tests, go vet ./..., and race-enabled tests across 112 packages, with a throwaway HOME.
- No installs. UI was not checked by eye; actual font and layout rendering remain unverified.
