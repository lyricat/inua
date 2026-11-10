---
branch: factory/FT101-topbar-space-group-centering
round: 1
---
# Topbar：两个弹性 SPACE 之间的组件组相对整条栏居中

## factory · 2026-11-10 15:16
根据 owner 的新功能请求登记到 backlog；本次仅记录需求，不启动构建。基于已完成的 FT88 可视化布局编辑功能，调整弹性 SPACE 的布局语义。

### 用户需求

当一个或多个连续组件被左右两个可拓展 SPACE 包夹时，这些组件作为一个整体，应相对整个 topbar 视觉居中，而不是仅在左右其他组件占用后剩余的空间内居中。

示例一：
`[A][SPACE][DATETIME][SPACE][TRAY][NOTIFICATION][CENTER]`
即使 DATETIME 右侧组件比左侧多，DATETIME 的中心仍应与 topbar 的水平中心重合。

示例二：
`[A][SPACE][DATETIME][PASS][SPACE][TRAY][NOTIFICATION][CENTER]`
DATETIME 和 PASS 组成一个连续组件组，整个组的中心应与 topbar 的水平中心重合，不是分别让每个组件居中。

### 验收要点

- 使用组件组的实际整体宽度（含组内正常间距）计算中心；以当前显示器的整条 topbar 为参照。
- 左右两侧 SPACE 可以具有不同宽度；不能继续以均分剩余空间作为这两个 SPACE 的布局规则。
- 两侧外围组件数量、宽度或可见性变化时，只要空间足够，目标组件组保持居中；组内文字或组件宽度变化后重新计算位置。
- 编辑器布局预览与实际 topbar 使用一致的居中语义；顺序、拖拽配置、保存和重启恢复保持有效。
- 回归测试覆盖上述两个例子、两侧宽度不对称、组内宽度变化、外围组件动态显示/隐藏及不同显示器宽度。

### 构建前需明确的边界

用户示例均为一对 SPACE 包夹一个非空组件组。若存在三段以上 SPACE、多个候选居中组、连续 SPACE，或栏宽不足以同时满足居中与不重叠，构建计划须明确选择规则与降级行为，不能承诺多个独立组件组同时占据同一中心。空间不足时应优先避免组件重叠并保持配置顺序；具体规则在计划中记录并测试。


## factory · 2026-11-10 20:37
Owner requested moving all current backlog items to new: “把 backlog 这些放到 new，注意他们的分支也在 fork 上”。
Moved backlog → new; no build or review performed in this update.
This item has no source PR or branch recorded; no fork branch was inferred.


## factory · 2026-11-10 21:01
Owner corrected the requested destination: “诶不对，他们应该放到 review”。
Moved new → review at the owner’s request; no build, review or tests performed in this update.
No source PR or implementation branch is recorded; locating the implementation is required before code review. This move does not certify that implementation exists.


## factory · 2026-11-10 21:03
Owner corrected FT101 specifically: “FT101 应该是 new”。
Moved review → new; implementation is pending. Other items remain unchanged. No build or review performed.


## factory · plan · 2026-11-10 21:52
Behaviour: For a custom layout with exactly two SPACE entries enclosing a nonempty item group, center that whole group relative to the full bar width, including normal internal spacing. Redistribute the two spaces asymmetrically. Recompute when available width, item measurements or visibility changes. Use the same rule in the editor preview. Keep default/unset and empty layouts working, widget identity and configuration order intact.
Scope: bar layout and preview allocation, toolkit-free geometry helpers/tests if useful, documentation. No config format changes or unrelated UI work.
Decisions: Only exactly two non-adjacent SPACE entries select a center group. Other layouts (zero/one/three-plus/adjacent spaces) retain equal-space distribution. When exact centering cannot fit, clamp the group between peripheral groups, prioritizing order and nonoverlap; if content itself cannot fit, retain existing GTK minimum-size/overflow behaviour without deliberately overlapping children. Hidden items consume no width. Preview uses its own container width and chip measurements with the identical selection/clamping semantics.
Checking: Regression tests for the two requested examples, unequal peripheral sizes, changing group size, visibility, monitor widths, boundary/fallback layouts and insufficient room. Run make generate for Blueprint edits; make i18n before every Go/Blueprint commit; make test with a throwaway HOME. UI not checked by eye; record that explicitly.

## factory · 2026-11-10 21:52
Claude Code 完成构建，提交 a798c9ed、4f3eeab8；分支已推送。
实现整组相对整条栏居中，包含内部间距并补偿非对称 padding；实际栏与编辑器共享布局规则，尺寸和可见性变化时重新分配。恰好两个非相邻 SPACE 且中间有可见组件时启用；其他布局均分剩余空间。空间不足时限制中心位置，保持顺序、不主动重叠；小于最小内容宽度时保持顺序并溢出。未设置布局保留默认行为，配置格式不变。
添加纯几何回归测试及 headless Broadway GTK 分配测试；共享原有 Broadway 测试初始化以支持新测试。更新文档与翻译引用。make generate 完成，无生成 UI 差异；make i18n 完成并提交 catalogs。
安装：无。
测试：Claude 首次完整测试遇到无关的 TestVisitorGame/chinese-checkers: websocket: bad handshake；该包连续四次重跑通过，第二次完整 make test 通过。管理者独立以临时 HOME 运行 make test 通过（Python、go vet、Go race tests）。工作树干净，未修改禁止路径。
未检查：未目视检查 UI，未在真实 Hyprland 上检查热插拔、layer-shell 尺寸、compact density 或拖拽外观；Broadway 使用简单测试控件，未覆盖真实 shell 控件和完整样式表。请在审查时关注这些边界。
new/ → review/；尚未合并或发布。

## factory · review · 2026-11-10 22:23
head: 4f3eeab8a37490d3a8571aad3643b3c8e9fe9539
verdict: pass

Claude Code reviewed origin/master...HEAD, both commits and all 24 changed files against the request and plan. No confirmed P0–P3 findings. Merge check against current base passed. No source changes or commits during review.
Validation: isolated make test passed with throwaway HOME/TMPDIR/XDG_RUN2026-11-10 22:23_DIR and env -i: 18 Python tests, go vet, 117 Go race-test packages. TestBarRowsOnGTK ran successfully on Broadway (not skipped); TestBarRowEntries passed. Scratch-copy make i18n and make generate matched committed outputs. Verified asymmetric padding, group spacing, hidden items, insufficient-width clamping, GTK measurement/allocation and preview wiring. Installs: none.
Residual risks: no visual UI check, real layer-shell sizing, hot-plug, real-bar compact density or drag appearance verification. Broadway used stand-in widgets rather than the complete shell and stylesheet. Unset layout retains its pre-existing small padding offset; explicit layout now centers exactly. RTL reversal is not implemented (no current RTL locale). Preview displays chips whose real widgets may be hidden; hidden-only center groups fall back as planned.
review/ → ready/. Not merged or released. Owner should visually check the real bar and editor before shipping.


## factory · 2026-11-10 23:35
Shipped to master; merge commit: 7dd6decf22e8e33fa026768cc2b673508dc59078. Release: v0.1.33, final master 7dd6decf22e8e33fa026768cc2b673508dc59078; master and annotated tag pushed atomically.
No linked issue or PR. Contributor forks were not modified.
Claude Code resolved integration conflicts and performed a fresh read-only combined-diff review (same persistent Claude session, not an independent reviewer): no P0/P1 findings. make test passed after every merge, on final combined review, and on independent manager rerun: 18 Python tests, go vet, Go race tests. make generate and make i18n current; protected paths unchanged. No installs or test failures this run.
Combined FT99/FT100/FT101 wiring reviewed. Broadway GTK allocation test ran; real monitor sizing and editor appearance remain unverified.
Tests used isolated HOME/TMPDIR/private XDG_RUNTIME_DIR and private D-Bus. UI not visually checked; no real multi-monitor/nested desktop, power actions or live settings exercised. Per-bar wiring lacks direct automated GDK monitor coverage.
ready/ → done/.
