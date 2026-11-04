---
source: issue #60
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
