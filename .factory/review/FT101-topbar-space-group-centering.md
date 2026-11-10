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
