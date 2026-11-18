# Settings: increase the default panel width to prevent sidebar compression

## factory · 2026-11-18 14:58
Registered the owner's UI fix request in `new/`; implementation has not started.

Owner's request:
> the default width of settings panel is too small. need to increase, or the sidebar of settings panel will be compressed.

Requested behavior:
- Increase the settings panel's default width so its sidebar is not compressed on initial opening.
- Choose the width based on the existing sidebar and content layout; preserve usable space for both without changing the visual design unnecessarily.

Acceptance checks:
- Opening settings at its default size shows the sidebar at its intended width, with readable navigation labels and usable content.
- Resizing remains usable and the larger default respects the available screen space on smaller displays.
- Add appropriate regression coverage for the default size/layout; record any visual checks that could not be performed.
