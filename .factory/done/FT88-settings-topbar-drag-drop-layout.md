---
branch: factory/FT88-settings-topbar-drag-drop-layout
round: 1
---
# Settings panel：拖拽配置 topbar 布局

## factory · 2026-11-08 23:02
根据 owner 本次要求登记到 backlog，暂不进入开发。

### 用户需求

在 settings panel 提供 Drag and Drop UI，方便调整 topbar 上的元素和布局：

- 展示一个虚拟 bar，包含可放置元素的 slots；另有元素备选区。
- 用户可从备选区拖拽元素到虚拟 bar，并自行调整 bar 上的布局。
- 备选区提供会扩充空间的 space（弹性空白），允许重复放置多个。
- 除 space 外，每一种元素最多放置一次；已放置的元素不可再次添加。
- 备选元素覆盖当前 topbar 已有的组件，包括 workspace indicator、app menu、datetime、icon tray、pass、clipboard、notifications、control center 等；此列表不是固定上限。

### 后续实现时的验收要点

- 可在虚拟 bar 内拖拽调整顺序与位置，可移除元素并重新从备选区添加。
- 多个 space 可以共存并分配可用空白空间；其他组件的唯一性在添加、移动及配置恢复时保持一致。
- 设置界面能读取当前布局，保存后的布局应用于实际 topbar，并在重启后保留。
- 不改变各组件原有功能；本需求只负责布局配置与编辑界面。

### 待设计细节

slots 的具体组织方式、space 的空间分配规则、保存或即时应用的交互、隐藏元素后的恢复方式，在进入开发前确定；当前不指定固定 slot 数量或左中右分区。

## factory · 2026-11-09 15:22
Moved backlog/ → new/ at the owner's request: “move FT88 to new”. The details left open above (slots, how spaces share the room, save or apply at once, bringing back a removed element) are for the build to decide and record in its plan.

## factory · plan · 2026-11-09 16:19
### Behaviour
Settings' topbar page shows a virtual bar with insertion slots and a palette covering every current bar component (workspaces/Engawa, app menu, app title, datetime, tray, recording, passwords, clipboard, notifications, dictation, control center), plus repeatable space. Drag palette items into slots; drag existing items to reorder; remove items to return them to the palette. Non-space components occur at most once. Every space receives an equal share of leftover width. Existing enable/availability conditions still apply. Apply edits through the existing immediate-apply/debounced-save settings flow, showing save errors using existing status feedback. Restore defaults and non-drag keyboard/button controls are available. An explicit empty layout stays empty and remains editable; absent layout preserves the existing default appearance. Invalid restored entries are normalized safely with duplicate non-spaces removed and unknown IDs ignored; distinguish absent versus explicitly empty data.
### Scope
Add toolkit-free layout representation/validation and tests, persistence and sample/docs, settings drag/drop editor, real per-monitor bar layout application. Reuse existing widgets and callbacks, keeping dynamic state, drop panels and hover/click targeting correct after rearrangement. Do not alter individual component functionality or external services. No workflows or factory config changes.
### Decisions
Use an ordered extensible list and insertion gaps, not a fixed number of slots or mandatory three zones. Spaces share spare width equally. Preserve legacy centered-clock layout for unconfigured defaults if necessary; document how switching to explicit editing behaves. Keep availability controlled by existing feature toggles; removed components cannot reappear just because a later status update enables them. Preserve widget ownership safely on detach/reparent. Treat workspaces/Engawa as their existing combined component unless inspection warrants separate safe handling. Immediate application matches changeSettings; no new Save mode. Palette and actual bar use one canonical component registry where practical.
### Checking
Add logic regression tests for add/move/remove, duplicate suppression, repeated spaces, absent/empty/malformed config roundtrips, normalization and defaults. Run make generate for changed Blueprint, make i18n before each Go/Blueprint commit, and make test with throwaway HOME. Compile UI code as covered by build/test. Check integration around per-monitor updates, runtime visibility, popover anchors and edge targeting. UI is not checked by eye in this environment; explicitly record that and list manual drag/drop, restart and multi-monitor checks still needed.

## factory · 2026-11-09 16:19
Built and pushed `d26f57f5` from `bf807607`. Settings → Bar & Dock now has a layout editor with palette, insertion indicator, drag/add/reorder/remove and non-drag buttons; repeatable spaces share spare width. Layout applies immediately to all bars and uses existing debounced persistence. Unset keeps the legacy centered clock; explicit empty stays empty. Unknown IDs and duplicates normalize safely. Existing availability gates remain; removed widgets stay detached. Panel anchors and edge hit areas follow the new order.

Checks: coder and manager independently passed `make test` with isolated throwaway HOME/XDG directories. Generated Blueprint UI and i18n catalogs committed. Coder reports an isolated Broadway smoke run covering settings and empty/unset/malformed/reordered layout reloads without GTK errors. No unpinned installs or manifest changes. Protected paths unchanged.

Limitations: UI was not checked by eye and actual drag/drop was not exercised. Real Hyprland/layer-shell, multi-monitor behavior, restart persistence, moved popover placement, keyboard controls, compact density and edge-gap appearance still need manual checking. Reordering shows an insertion line rather than sliding neighboring items (documented design difference). Existing icons reused; new strings remain untranslated in catalogs. First coder test used relative HOME and failed three VM tests; corrected to absolute isolated HOME and full suite passed; stray files removed.

Moved new/ → review/.

## factory · review · 2026-11-09 16:41
head: d26f57f5a0fbb51f2b6f2463da591cc1d36e1ba0
verdict: pass
notes:
- [P2] internal/ui/bar.go:637: 重排后面板扩展命中范围跨过托盘。布局 ["control_center","tray","notifications"] 中 tray 未被标记为 Break；独立运行命中函数验证托盘内坐标返回控制中心且 onButton=false，后续悬停/滚轮逻辑可进入音量调整分支。让托盘等独立交互组件阻断相邻面板的扩展命中范围。

Codex reviewed all 32 changed files in origin/master...HEAD; no P0/P1 findings. No merge conflicts with master. Required make test passed with isolated absolute HOME/XDG directories: 18 Python tests, go vet, and race tests across 115 Go packages. Blueprint compilation and regenerated i18n in a temporary copy matched committed output. Initial GTK probe crashed on an ordinary widget; rerun with isolated D-Bus passed, not attributed to this change. No unpinned installs. Code worktree clean; protected paths unchanged.

UI was not checked by eye. Actual drag/drop, multi-monitor behavior, restart persistence and relocated popover placement still need manual checking. No code fixes made. No linked GitHub issue or PR to comment on.

Moved review/ → ready/ under the no-P0/P1 review rule; P2 remains for owner attention.


## factory · 2026-11-09 22:09
Shipped at owner request after Claude Code fixes. Merge commit: d401a937. Fixed outstanding P2: independent bar items block neighboring panel hit ranges, including tray and dictation. Fix commit: 2cd0dbb3. Generated translation conflicts resolved preserving both parents translations. No linked issues or PRs.
Claude Code added regression tests and passed make test; manager independently reran make test with isolated absolute HOME/XDG directories: exit 0 (18 Python tests, go vet, full Go race suite). No installs; protected paths unchanged. master and annotated tag v0.1.30 atomically pushed at 2cd0dbb3.
Not checked: visual UI, actual drag/drop, multi-monitor behavior, restart persistence, real-application key delivery and real compositor behavior. Moved ready/ → done/.
