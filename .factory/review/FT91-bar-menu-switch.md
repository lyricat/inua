---
source: pr #75
branch: factory/FT91-bar-menu-switch
round: 1
---
# feat(bar): a switch for the application menu, and the clock at the end

## factory · 2026-11-10 08:36
Imported GitHub PR #75 directly into review at the owner's request: “全部放到 review”.
Source: https://github.com/lyricat/inua/pull/75
Upstream head at import: d9712fa602e043366083f128eba945e27e2d72aa
Uses the existing PR branch; no code changes made during import.

Review the PR against the current base, including its requested behavior, regressions and tests.
No review, tests or visual UI checks run during import. Awaiting review; not approved for merge.

## factory · 2026-11-10 10:01
Review blocked: the recorded branch `feat/bar-menu-switch` does not exist on origin (lyricat/inua). GitHub PR #75 is open, but its head is on the fork iamcheyan/inua at d9712fa602e043366083f128eba945e27e2d72aa. The review workflow requires the recorded origin branch, including push access if base conflicts require resolution; no replacement branch was created or header changed. Owner must correct the branch setup before review can proceed.

No coder invoked, tests run, installs, or review verdict. Item remains in review/. FT89 was skipped because its current head already has a review.

## factory · 2026-11-10 10:20
Owner requested repair of the blocked branch setup. Fetched PR #75 head d9712fa602e043366083f128eba945e27e2d72aa and created origin branch factory/FT91-bar-menu-switch. Merged origin/master and resolved conflicts at 98dd40aa88c48f30c9a974858316a221ddd6e675; pushed the branch and corrected the header. Original fork PR branch is unchanged.
Preserved menu switch behavior and custom bar layouts. make generate and make i18n passed. Coder build tests and manager's independent make test passed with throwaway HOME. No installations. UI not visually checked. No merge to master.

## factory · review · 2026-11-10 10:20
head: 98dd40aa88c48f30c9a974858316a221ddd6e675
verdict: fix
fix:
- [P1] internal/apps/mail/bodystructure_test.go:76: Required make test failed at line 88 in TestBodyStructureMessagePart with unexpected EOF. Fake server assumes its first command is FETCH, but the client can send CAPABILITY first; advertise capabilities in greeting or dispatch replies by command.
notes:
- The failing test file is identical to origin/master; this is not attributed to FT91. Reviewer reports 20 subsequent isolated runs passed, consistent with an intermittent failure. No unrelated base fix was made.
- No confirmed new menu/clock functional defects found. Script tests and go vet passed. Review made no code changes; no installations; UI not visually checked.
- Remains in review because a required review test failed, despite earlier full test passes. Branch lookup blocker and merge conflicts are resolved.
