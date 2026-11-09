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
