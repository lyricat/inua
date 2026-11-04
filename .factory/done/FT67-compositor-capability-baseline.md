---
source: issue #60
branch: factory/FT67-compositor-capability-baseline
round: 1
---
# Compare compositor capabilities and propose a minimum support baseline

## lyric · 2026-11-04 22:58
Build GitHub issue #60: https://github.com/lyricat/inua/issues/60
Parent overview (context only): #58. The sub-issues of #58 are worked one by one, in the order #58 lists them; this is the second. #61–#64 stay outside the factory until the one before them is done.

This supersedes #60's keep-out-of-factory status for this issue only. Investigation and documentation only: no runtime code changes, no interface design, no abstraction layer.

### Inputs
- The coupling inventory from #59 (FT58): docs/hyprland-coupling.md. Use it as the feature list; do not redo it.
- #61 has since been retitled "Validate niri and Wayfire with small integration experiments": weigh niri and Wayfire with that in mind, while still comparing Sway and KWin at the capability level as #60 asks.

### Deliverables / acceptance criteria
- A repository document with the capability matrix: for each feature in the inventory, Hyprland, Sway, niri, Wayfire and KWin, with protocol or IPC, versions, sources, and a verification status that separates documentation-only findings from real-session checks and unknowns.
- A proposed minimum usable baseline and the optional capabilities beyond it.
- The explicit UI and configuration behaviour for unsupported and temporarily unavailable capabilities.
- The features that need compositor plugins or scripts, or compositor-internal APIs that are not exposed.
- A justified recommendation for the first additional compositor; the owner decides before the experiment, so present the choice, do not start it.
- Link the document from docs/hyprland-coupling.md and the architecture documentation.

## factory · plan · 2026-11-05 02:30
Behaviour: Document proposed first-run checks, valid empty states versus disconnection, unsupported controls, action errors and recovery without replaying stale actions. Distinguish proposals from current implementation and documentation/source findings from real-session evidence.
Scope: Compare all 23 FT58 inventory features across Hyprland, Sway, niri, Wayfire and KWin; add docs/compositor-capabilities.md and links from the inventory and architecture. No runtime changes, interfaces, abstraction layer or #61 experiments.
Decisions: Preserve native workspace semantics; propose a textual overview, ordinary shortcuts and external settings as defaults. Recommend niri pending owner approval, compare Wayfire for subsequent experiments and retain Sway/KWin alternatives.
Checking: Verify inventory coverage, version/source references, local links and git diff --check; run make test in isolated HOME and session environment. Documentation needs no new automated tests. Real compositor sessions remain untested.

## factory · 2026-11-05 02:30
Recovered the prior interrupted build's three uncommitted documentation files in the existing worktree and completed them with Codex, preserving their work. Commit: 90f7d2cc1dcba4f5605d45569d78d440a3b9c0d7; pushed branch factory/FT67-compositor-capability-baseline.
Built the five-compositor, 23-feature capability matrix, minimum/optional baseline, explicit unavailable/unsupported UI and configuration proposals, plugin/script/permission requirements and first-target recommendation. niri remains a recommendation for the owner, not implemented support.
Checks: Codex make test passed (Python 15 tests, go vet, race tests: 103 packages passed, 0 failed); 48 top-level tests and 6 subtests skipped, with causes recorded in the document. Manager independently reran make test with throwaway HOME/XDG and isolated D-Bus environment: exit 0. git diff --check passed; only the three planned documentation paths changed, no forbidden paths.
Installs: none reported; no dependency manifest changes. No UI changes, real-session compositor checks or experiments; runtime capabilities and protocol negotiation remain unverified. Earlier path-length test failures and successful final checks are recorded in the document.
Moved new/ → review/. Owner should examine the proposed minimum baseline and niri recommendation after review; FT68 was not built in this run.

## factory · review · 2026-11-05 02:40
head: 90f7d2cc1dcba4f5605d45569d78d440a3b9c0d7
verdict: pass
notes:
- [P2] docs/compositor-capabilities.md:92: niri press/release bindings are marked "Since: next release" in the cited official documentation and absent from v26.04 configuration source. Mark this as development-only rather than available in the compared release; for example, `Mod { release { toggle-overview; } }` is not supported by that release.

Validation: Codex reviewed origin/master...HEAD; all 23 inventory rows and five compositor columns, local links, reference definitions and git diff --check passed. All 46 external references were retrieved and key capability/version evidence checked. Final complete make test exited 0: Python 15 tests, go vet and race tests passed (103 packages passed, 33 without tests; 1159 top-level tests passed, 53 top-level and 6 subtests skipped). Initial four-package failures from temporary /proc path aliases disappeared after correcting isolated test paths; affected code is unchanged from base. No remaining test failure. No merge conflicts. Worktree clean; no code changes, commits, installs or dependency restoration.
Limits: skipped tests require unavailable terminal engine, libturbojpeg, fish, JavaScript runtime or download conditions; compositor and GPG/pass tools were excluded from test PATH for isolation, and some helper tests skip by design. Rolling docs and source checks do not prove real-session interoperability. No visual, live compositor or integration experiment validation.
Moved review/ → ready/: no P0/P1 findings. Owner should examine the P2 version caveat, proposed minimum baseline and niri recommendation; this review does not approve or start experiments.

## factory · 2026-11-05 08:05
Owner authorized shipping ready items without blockers. Merged into master at 4e53f24; pushed integrated master f9cecde. Moved ready/ → done/. Closed linked issue #60.

Integration: resolved catalog conflicts through regeneration and preserved both terminal session drag and context-menu handlers. make generate and make i18n passed with no remaining generated changes. Final make test passed in isolated HOME/XDG/runtime and D-Bus (15 Python tests, go vet, race tests across 104 Go packages); compositor experiment suite passed 17 tests. Initial temporary-path failures were corrected in test isolation, not business code. 50 skip records include unavailable terminal engine, libturbojpeg, fish, bun and conditional/helper tests. No installs. No protected paths changed.
Limits: no visual or real-compositor validation; existing review P2 notes remain unresolved and nonblocking under the configured gate.
