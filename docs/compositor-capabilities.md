# 合成器能力比较与最低支持基线

本文对应 #60（FT67），审阅日期 2026-11-05，仓库基线 `d6a2e5a2`。
功能范围沿用 [FT58 清单](hyprland-coupling.md#功能与依赖矩阵) 的 23 行，
不重新审计调用链。#58 只作背景；#61 的 niri、Wayfire 小型集成实验尚未开展。
本文不新增运行代码、接口或抽象层，也不改变安装、配置和会话行为。

**建议首个新增目标为 niri，先提供下述最低基线；由 owner 决定是否采纳。**
Sway 是有完整 IPC 证据的备选，不是预定的实现顺序。Wayfire 的插件控制能力和
0.11 捕获扩展值得与 niri 同时进入 #61 的比较；KWin 则需要额外处理 KDE 专用
协议、脚本和桌面服务。五者都没有在本次运行真实会话。

## 如何读证据

- **源**：在指定版本的实现或本仓库 FT58 中确认。代码路径存在不等于组合可用。
- **文**：上游维护的文档、发布说明或 API 文档明确描述；仍未实际连接。
- **未知**：没有足够证据，或只确认了部分能力。不是断言合成器永远不能支持。
- **不适用／不支持**：限定在注明的版本、通道或当前 inua 路径；不扩大到整个产品。
- **实测**：本次全部为“未测”。单元测试、协议 XML、其他项目的演示均不充当
  inua 的真实会话证据。表内“源／文”都隐含这一限制。

表内的“可选”“禁用”“改用”均为**产品建议，尚未实现**。当前 inua 仍按
Hyprland 启动、读写配置和发送动作；普通 GTK 窗口能显示，不等于桌面支持。
现有退化及其缺陷以 FT58 为准，特别是 Launcher 聚焦失败只记日志、旧快照在
断线后保留、显示查询失败可能显示“No displays”，不能写成本文建议已经存在。

## 产品版本、通道与来源范围

以下是本次比较的版本，不是“最新版本”清单，也不是已验证的最低安装版本。
`_v1` 是协议名称的一部分，不表示产品版本或实际协商值。

| 合成器 | 产品／源码取样 | 控制通道与证据范围 | 版本限制 |
| --- | --- | --- | --- |
| Hyprland | 仓库要求 0.53+；Overview／Lua 路径要求 0.55+ | 请求与事件 Unix socket，classic dispatcher／`eval`；沿用 [FT58][H] | 无 IPC 版本握手；Lua 探测成功不代表全部脚本动作存在。本文不扩大原有兼容承诺 |
| Sway | **1.12**，Debian 源码包 **1.12-1** | `SWAYSOCK`，i3 IPC 帧与 JSON，命令、树、工作区、输出、订阅；[固定版本手册][S1]、[服务端][S2] | Debian 修订号不是协议版本。Arch 上游手册镜像为滚动页面，仅作命令说明，不能用于倒推引入版本 |
| niri | **26.4.0** 的 `niri-ipc` 文档与源文件 | `NIRI_SOCKET`，逐行 JSON 请求／回复及独立事件流；[固定版本 API][N1] | crate 跟随产品版本，Rust API 不承诺 semver 稳定。官网配置／协议文档为滚动版；未注明引入版本的能力，其在 26.4.0 的版本下限仍未知 |
| Wayfire | **0.10.0**，Debian 源码包 **0.10.0-1**；另读 **0.11.0** 发布说明 | `WAYFIRE_SOCKET`，`ipc` Unix socket 传输与 `ipc-rules` 方法／事件；[传输][W14]、[方法][W1]、[事件][W13]、[0.11 说明][W11] | 0.10 源码不能证明 0.11 的全部实现。0.11 新能力只标“文”；插件构建、启用列表和版本需一并固定 |
| KWin | **6.5.2**，上游 tag `v6.5.2` | KDE Wayland 扩展、D-Bus、KWin JavaScript；[服务端][K1]、[脚本对象][K2] | KDE 专用 API 与扩展各有自己的版本；本文没有证明滚动脚本文档的每个成员都在 6.5.2 存在 |

Sway、Wayfire 的固定源码来自 Debian 的上游源码镜像；KWin 来自 KDE 官方
仓库；niri 来自项目发布的 docs.rs 文档。固定 URL 的有关源文件内容已核对；
无法确定出处的片段不承担支持结论。未取得的 issue、PR 和源码不作为
已核实证据，所缺部分保留“未知”。

## 协议边界

此表说明有哪些服务端证据和版本信息。客户端仍须被允许访问；“支持”不保证
当前构建、沙箱或会话暴露该 global。XML 本身不作为合成器支持证据。

| 能力／协议 | Hyprland | Sway 1.12 | niri | Wayfire | KWin 6.5.2 |
| --- | --- | --- | --- | --- | --- |
| `zwlr_layer_shell_v1` | 源：经 gtk4-layer-shell；实际协商未知 [H] | 源：服务端上限 **5** [S2] | 文：官网列出 layer-shell 组件；上限未知 [N6] | 源：0.10 注册 **4** [W4] | 源：注册 **5** [K3] |
| `ext_session_lock_manager_v1` | 源：当前锁路径需 **1**；恢复另受 Hyprland 配置控制 [H] | 源：注册 session-lock；恢复政策另核 [S8] | 文：官网说明 ext-session-lock；实际版本、恢复组合未测 [N5] | 源：session-lock 实现，需核对恢复和插件组合 [W5] | 当前 inua ext-session-lock 路径不适用：所查服务端采用 KScreenLocker 专用连接；未找到该 manager 注册 [K1] |
| 窗口列表与控制 | 源：IPC；预览另用 wlr handle [H] | 源：IPC；另注册 wlr management、ext list **1** [S1][S2] | 文：IPC `Window`、事件；不从 list 协议推断管理动作 [N1] | 源：`ipc-rules`；0.11 **文**：`ext-toplevel` 插件提供 ext list [W1][W11] | 源：`org_kde_plasma_window_management`，受权限过滤；脚本可访问窗口 [K1][K2][K4] |
| 单窗口图像 | 源：mapping **1**、export 实际最低 **2**；现有版本检查有缺口 [H] | 源：ext list **1** → foreign-toplevel capture source **1** → image-copy **1**；IPC 提供关联标识 [S1][S2] | 文：支持 ext-image-copy／wlr-screencopy；IPC ID 与捕获 handle 的关联未知 [N4][N1] | 0.11 文：ext-image-capture-source／image-copy；具体 source 类型、版本和 IPC 身份关联未知 [W11] | 源：KDE screencast 插件按窗口 UUID 提供 PipeWire 流，接口受过滤 [K1][K5] |
| 虚拟指针＋虚拟键盘 | 源：inua 两个 manager 都绑定 **1** [H] | 源：input manager 注册两者；依赖库给出的上限未核 [S3] | 文：官网明确允许主机会话客户端使用两者；过滤连接不可据此推断 [N5] | 源：0.10 core 注册两者；上限未核 [W3] | 当前 inua 的这对 wlr／virtual-keyboard 路径无支持证据；替代为授权 EIS／KDE 输入路径，非即插即用 [K1][K6] |
| 输出配置 | 源：IPC＋生成配置 [H] | 源／文：IPC output 命令、wlr output-management [S2][S4] | 文：IPC `OutputAction`＋KDL，不能据此推断 wlr output-management [N1][N7] | 源：wlr output-management＋配置；镜像为实现内行为 [W6] | 源：KDE output-management v2 家族；与 wlr 协议不互换 [K7] |
| 激活／外来父窗口 | 源：GDK 处理；变体与实际协商未知 [H] | 源：xdg-activation、xdg-foreign v1/v2 实现 [S2] | 未知：未取得对应版本服务端注册证据，不以 GTK 可运行代替确认 | 源：core 的 xdg-foreign v1/v2；activation 路径需另核 [W3] | 源：xdg-foreign v2；激活由 KWin 政策控制 [K1] |

基础 layer-shell anchors、exclusive zone 为 v1；动态换层需 v2，按需键盘交互
需 v4。建议最低基线采用 **layer-shell v4 语义**，不依赖库对旧版本的未知模拟。
这不要求所有增强协议存在。窗口列表可来自 IPC，不能以 foreign-toplevel-list
代替工作区、几何、MRU 或控制；输出管理不能据名称推导出创建虚拟输出。
GTK 的 xdg-shell、输出、缩放与输入法协商版本仍由 GTK／GDK 决定，本文没有
记录 registry 实测。参见 [FT58 协议版本说明](hyprland-coupling.md#协议与版本边界)。

## 按 FT58 功能逐项比较

行号仅用于核对覆盖，不定义 API。每行的 Hyprland 列都引用 [FT58][H]，
其他列按上一节的产品版本和源文件范围解释。完整真实会话状态均为**未测**。

### 窗口、工作区、输出与 Overview

| FT58 功能 | Hyprland：现有路径 | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- |
| 01 窗口列表、标题、运行中的应用 | 源：IPC client 地址、class、PID、标题、焦点历史 | 源：`GET_TREE`、window 事件；app_id／XWayland class、PID、focus 子树 [S1] | 文：Window ID、可空 app_id／PID、workspace_id、focus_timestamp，事件更新 [N1] | 源：`list-views`＋watch；app-id、PID、role、mapped、last-focus-timestamp [W1][W2][W13] | 源：Plasma window 协议或脚本 `windowList`；普通 `org.kde.KWin` D-Bus 不是完整窗口列表／事件接口 [K2][K4][K8] |
| 02 Bar 工作区、滚轮切换、各屏标题 | 源：正 ID、单屏归属；各屏可见与全局焦点分开 | 源：`GET_WORKSPACES` 的 name／num／output、visible／focused；命令切换。非数字名的 num=-1 不能过滤 [S1][S5] | 文：id≠idx，is_active≠is_focused，active_window_id 可空；按稳定 ID 激活 [N1][N2] | 源：wset＋二维格点；`vswitch` 切换，不能当成全局整数序列；各屏非焦点标题的选择仍待证 [W1][W2][W7] | 源：虚拟桌面与输出分别管理，currentDesktop 是全局桌面；不能伪造“一桌面只属一屏” [K2] |
| 03 多屏 Bar、背景、Dock、弹层位置 | 源：GDK 生命周期＋IPC connector／focused monitor | 源／文：layer-shell＋输出树；GDK connector 联接未测 [S1][S2] | 文：layer-shell＋Outputs／FocusedOutput；无输出可为空 [N1][N6] | 源：layer-shell＋list-outputs／get-focused-output／增减事件 [W1][W2][W4][W13] | 源：layer-shell＋脚本 screens／activeScreen；和 Plasma shell 同时存在时的区域占用未测 [K2][K3] |
| 04 屏幕模式、缩放、镜像、合盖 | 源：IPC、生成配置、内核／logind | 文：output 配置支持模式／缩放／位置，bindswitch 可处理盖子；原生镜像未证，不能把同坐标视作镜像 [S4][S5] | 文：OutputAction／KDL 支持模式、缩放、变换和位置；官网镜像走 screencast，非原生输出克隆；合盖组合未测 [N1][N7][N4] | 源：output-layout 含镜像和模式；动态设置与持久文件不同，盖子政策未证 [W6][W8] | 源：KDE 输出管理；KScreen／电源服务集成另核，不能把 Apply 等同于持久化或合盖安全 [K7] |
| 05 Launcher 聚焦、取消和窗口命令 | 源：按地址聚焦、恢复、移动／关闭等 | 源／文：`[con_id=…] focus`／kill、floating、fullscreen、move／resize；取消恢复需目标仍存在 [S1][S5] | 文：FocusWindow／CloseWindow 可指定 ID；移动、浮动、全屏等各有 Action，不能发 classic 命令 [N1] | 源：focus-view／close-view／configure-view；全屏、置顶等还由动作插件提供 [W1][W9] | 源／文：Plasma 请求或脚本 activeWindow／窗口动作；受焦点政策约束 [K2][K4][K9] |
| 06 Dock 点击窗口时保持指针位置 | 源：临时 cursor:no_warps；批处理不是事务 | 文：`mouse_warping` 可配置；逐次激活而不修改用户全局偏好的等价方法未知 [S5] | 文：有焦点动作；是否移指针受 `warp-mouse-to-focus` 配置影响，逐动作免 warp 未证 [N1][N8] | 源：focus-view 调 default_wm；插件最终是否 warp 未证 [W1] | 未知：激活政策、脚本与指针移动的组合没有验证，不能承诺无 warp [K2] |
| 07 Overview 浏览、搜索、MRU、每屏 New 卡片 | 源：Lua 探测；无 Lua 转 Launcher Windows，New 预留正 ID | 源：树和工作区可支撑文字视图；focus 子树不是 Hyprland 全局 MRU；New 生命周期不同 [S1][S5] | 文：窗口／工作区／MRU 时间戳；动态末尾空工作区已有 New 的语义，不预留数字 ID；原生 Overview 也有独立动作 [N1][N2][N3] | 源：wset 网格和焦点时间戳；新增网格格点不等于创建 Hyprland 工作区 [W2][W7] | 源：桌面列表与 createDesktop；按屏 New 不适用全局桌面模型，MRU 等价关系未知 [K2] |
| 08 Overview 激活、拖放、新工作区、关闭／强制结束 | 源：多条 Lua 请求、follow=false、屏幕路由、close／kill、鼠标绑定保护 | 文：窗口／工作区 move 与 close；工作区跨屏操作、非跟随移动和鼠标冲突组合未测；`kill` 是关闭请求，不等于杀进程 [S5] | 文：MoveWindowToWorkspace 有 window_id、reference、focus=false；工作区移屏另有动作。强制结束和全局鼠标绑定保护无等价证据 [N1] | 源：configure-view、vswitch/send-view、set-workspace；grid/wset 路由不同，多步成功与强制终止未证 [W1][W7] | 源／文：脚本桌面归属、sendClientToScreen、关闭；不能照搬每屏创建及路由；任意强制终止须另核 [K2][K9] |
| 09 Overview 实时预览 | 源：wlr handle→Hyprland mapping→export；非屏幕矩形裁剪 | 源：ext 捕获链，GET_TREE 的 foreign_toplevel_identifier 提供身份依据；后台／遮挡／XWayland 帧仍未测 [S1][S2] | 文：捕获协议和 portal 存在；IPC Window 未给出 ext handle 关联字段，不能用标题／PID 猜身份。暂不承诺 inua 预览 [N1][N4] | 0.10 当前 Hyprland 路径不适用；0.11 文：新捕获协议，单窗 source 和身份关联仍未知 [W11] | 源：KDE screencast 单窗流可作候选，但受接口权限与 PipeWire 生命周期约束；不是当前 SHM export 路径 [K1][K5] |
| 10 Dock 避让、自动隐藏及 Overview 上方 Dock | 源：exclusive zone、pointer Revealer、换层；没有窗口相交算法 | 源：layer-shell；浮动／全屏避让和换层交互未测 [S2] | 文：layer-shell；滚动列、背景与原生 Overview 的区域政策需实测 [N6] | 源：layer-shell；scale／expo／wm 插件可影响最终场景布局 [W4][W10] | 源：layer-shell；Plasma 面板占用、浮动／全屏与脚本工作区面积需另核 [K2][K3] |

### 输入、捕获、锁与桌面集成

| FT58 功能 | Hyprland：现有路径 | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- |
| 11 普通快捷键及设置录制 | 源：binds、生成绑定、inua-record submap | 文：bindsym／bindcode、mode、配置 reload；GET_CONFIG 不是完整有效绑定枚举；崩溃后 mode 恢复未证 [S1][S5] | 文：KDL binds；录制期间可靠暂停／恢复全部外部绑定无证据，不能把 inhibit 当成录制事务 [N10] | 源：command 配置与运行选项；完整冲突枚举、录制隔离及恢复未知 [W8][W10] | 文：KGlobalAccel／脚本 registerShortcut 是候选；不是全局输入录制或安全禁用其他快捷键的证明 [K9] |
| 12 单按 Super、Super+Tab、数字槽位 | 源：Lua raw key listener／held-key／释放绑定／custom event | 文：release binding 只证明释放触发；单按与组合键 shadowing、跨焦点按键释放和槽位行为未证 [S5] | 文：press/release binds；本次未证全部组合语义。基线仅普通组合键，原生 Overview 可保留 [N10] | 未知：既有 IPC 不等于原始按键观察；完整行为可能需 compositor 插件，尚未证明必须编写 [W1][W8] | 未知：修饰键服务／脚本快捷键不是任意按键观察；完整行为可能需内部效果或输入插件，不能默认取得 [K1][K9] |
| 13 截图、取色、窗口选择、录屏 | 源：IPC 几何＋grim／wf-recorder＋export picker | 源／文：屏幕捕获、IPC 几何和 ext 单窗捕获；工具构建、缩放／旋转裁剪、取色组合未测 [S1][S2] | 文：Screenshot／ScreenshotWindow、PickWindow／PickColor、portal／捕获协议；滚动视图不能直接作 grim 全局矩形 [N1][N4] | 源：wlr-screencopy；0.11 新捕获协议。工具、变换后窗口区域和单窗 picker 未证 [W3][W11] | 源：KDE screencast 可提供屏／窗；现有 grim／wf-recorder 路径无等价保证，交互截图另用桌面服务 [K5] |
| 14 Portal 截图与屏幕共享选择 | 源：inua Screenshot；ScreenCast 为 xdph，picker 返回 Hyprland 地址 | 文：xdpw 提供截图／共享及输出选择；这里核对的 chooser 是输出，不承诺单窗口；xdph picker 不兼容 [P1] | 文：xdg-desktop-portal-gnome＋PipeWire＋niri 会话；可共享屏／窗，不能复用 xdph selector [N4][N9] | 源：0.10 portal 配置选 wlr／gtk；0.11 文有新 picker，但安装版本与替换契约未证 [W12][W11] | 文／源：xdg-desktop-portal-kde＋KWin screencast；应使用其授权选择流程，不能替换为 Hyprland 地址 [P2][K5] |
| 15 锁屏、锁定期间输出变化、重启恢复 | 源：ext-session-lock＋认证 helper＋restore 设置 | 源：session-lock；客户端死亡、第二个 locker、热插拔的端到端行为未测 [S8] | 文：ext-session-lock；官网描述主机信任和重建锁表面的能力，不保证 inua marker／恢复适用 [N5] | 源：session-lock；重启恢复和新输出锁表面须实测 [W5] | 源：KScreenLocker 管理；当前 inua 锁 UI 不支持这条专用连接。基线应委托已验证的 KDE 锁服务 [K1] |
| 16 手机触控板／键盘 | 源：两个虚拟设备 manager 都必需；单 seat | 源：虚拟指针＋键盘；沙箱 global 过滤，多 seat 未测 [S2][S3] | 文：两协议允许主机客户端，过滤会话不可用；设备／seat 和输入法未测 [N5] | 源：core 两 manager；0.11 security-context 插件可能过滤；权限和 seat 未测 [W3][W11] | 当前直连路径无支持证据；EIS／portal RemoteDesktop 为候选，涉及授权和设备映射，尚未适配 [K6][P2] |
| 17 剪贴板历史的复制与粘贴 | 源：wl-copy；有地址走 sendshortcut；nil client 才 wtype 当前焦点回退 | 源：数据控制与虚拟输入是候选；不能保证按指定窗口粘贴；基线仅复制／手动粘贴 [S2][S3] | 文：数据控制、虚拟键盘；FocusWindow 后发送到当前焦点有竞争，不能等同地址投递 [N1][N5] | 源：数据控制、虚拟键盘；未证指定 view 收键，基线仅复制 [W3] | 未知：历史工具与 KDE 数据控制版本未核；授权输入也不等于任意窗口定向粘贴。普通应用复制不受此结论否定 [K6] |
| 18 听写插入及提示位置 | 源：必须有 Hyprland 活动地址，无 wtype 回退；几何定位、录音 submap | 未知：焦点＋输入、提示几何、录音绑定需一起验证；建议仅保留转写后复制 [S1][S3][S5] | 文：焦点／输入可分别请求；滚动位置可缺失，暂无定向插入保证；建议角落提示、手动粘贴 [N1][N5] | 未知：IPC 几何和键盘通道分别存在，目标、录音隔离与最终布局组合未证 [W1][W3] | 未知：需授权输入和焦点政策；不能由转写服务可用推导自动插入 [K2][K6] |
| 19 通知激活、文件选择器父窗口 | 源：GDK activation token、xdg-foreign parent；可失败 | 源：相应扩展注册；应用接受 token、跨客户端父子关系未测 [S2] | 未知：GDK 路径的对应版本、token 策略与 foreign parent 未核 | 源：xdg-foreign；activation 版本及实际聚焦仍待核 [W3] | 源：xdg-foreign v2；激活受焦点政策限制，不保证通知点击强制抢焦点 [K1] |

### 应用控制、配置与无屏会话

| FT58 功能 | Hyprland：现有路径 | Sway | niri | Wayfire | KWin |
| --- | --- | --- | --- | --- | --- |
| 20 应用重开、设置附加面板、Mail 平铺 | 源：直接 IPC 按 PID 找窗、BringHere、RoomRight、移动／resize／settiled | 文：按节点控制位置／浮动／平铺；树、边框、gap 不等于 RoomRight，增强需重验 [S1][S5] | 文：PID 可空，滚动列／浮动动作；固定右侧剩余空间假设不成立，普通 present 是建议退化 [N1] | 源：configure-view、grid／tile／wm-actions；效果变换后的 bbox 不等于可放置区 [W1][W2][W9] | 文／源：脚本 clientArea、窗口几何和 sendClientToScreen；需加载脚本，普通 GTK present 不保证搬到当前桌面 [K2][K9] |
| 21 Assistant／MCP／Workshop 的 desk API | 源：直接 IPC＋classic dispatcher／独立轮询 | 源：IPC 可提供查询／动作，但现有 desk 不会自动适配；语义不支持必须返回错误 [S1][S5] | 文：查询／指定 ID 动作可作依据；不能把无焦点当查询错误，未知字段与失败均需明确处理 [N1] | 源：插件方法／事件；方法未加载不等于空桌面 [W1][W2] | 源：仅普通 KWin D-Bus 不够；完整视图需获准协议或脚本，对外控制不能绕过桌面权限 [K1][K2][K8] |
| 22 登录、初次配置、输入设置 | 源：Hyprland 会话脚本、bootstrap、生成设备／键／窗口／图层规则 | 文：Sway config／include／exec／input；需另做会话接入，当前 inua init 不支持 [S5][S6] | 文：niri-session、KDL input／binds／rules；当前 Hyprland bootstrap 不适用 [N9][N10][N11] | 源：wayfire.ini、autostart／command 等插件；需确认插件集合与 portal 路由 [W10][W12] | 文／源：Plasma/KWin 会话、KConfig／脚本／KDE 服务；不能在现有 Plasma 中自动停用面板或锁服务 [K1][K9][P2] |
| 23 无物理输出时远程桌面启动 | 源：Hyprland 实例发现＋headless output；禁用但连接的屏也阻止创建 | 源：create_output 只对 Wayland／X11／headless backend 生效；不是任意 DRM 会话的完整创建／移除承诺 [S7] | 未知：所查 IPC 无普通 headless 输出创建／销毁动作；有无屏工作区模型不证明能启动远程编码 [N1][N2] | 源：ipc-rules 有 create-headless-output／destroy-headless-output；须启用插件，首次无帧、模式、编码器仍未测 [W8] | 源：screencast 插件可创建随流销毁的虚拟输出；有权限／后端条件，不等于登录前创建持久远程桌面 [K1][K5] |

## niri 的工作区、几何和焦点不能照搬

[niri 26.4.0][N1] 的窗口 ID 在窗口存活期间稳定；工作区 [id][N2] 在移屏和排序
时稳定，idx 只是当前屏幕上的位置，其他屏幕可以有相同 idx。ID 不保证从 1 开始
或单调递增。空的中间工作区会消失，每屏末尾保留空工作区；命名工作区另有保留
规则 [N3]。因此建议尊重现有空工作区，不保留 inua 自选的“11 起”数字 ID，
不以正数筛选，也不把位置序号当身份。无输出时 output 可以为空，仍可能有窗口。

WindowLayout 提供列／格点序号、tile 尺寸、可空的当前 workspace-view 位置和
窗口在 tile 中的偏移；这些是逻辑坐标，允许小数 [N12]。**推断**：滚动到屏外的
窗口仍属当前工作区，但不能按输出原点直接裁成完整截图。建议最低基线只提供
文字窗口列表，不画伪造的缩略几何；后续检查滚动中、屏外列、浮动／全屏、缩放、
旋转和输出拔插后，再决定是否提供空间化 Overview。

每屏可以有 active workspace，但全局只一个 focused workspace；layer-shell
取得焦点时可以没有 focused window。取消 Launcher 应按此前稳定 ID 恢复仍存活
的目标，失败时由合成器维持焦点。`MoveWindowToWorkspace` 的 `focus=false`
是明确文档能力；这不证明“移动窗口＋移动工作区到另一屏”是原子动作。事件流
起始状态和后续更新仍可能短暂出现窗口引用已移除的工作区；不可据此清空整个
桌面或把下一条动作改发给当前窗口 [N1][N13]。

## 建议的最低可用基线

目标是可日常使用的桌面 shell，不要求 Hyprland 的全部增强行为。此提案需要
owner 认可后，再由后续实现和真实会话验证兑现；产品版本表不是通过名单。

| 必需能力 | 最低可见行为 | 不能代替它的条件 |
| --- | --- | --- |
| 桌面表面与输出 | layer-shell v4 语义；Bar、背景、Dock／弹层可定位；输出增减时正确创建和释放，零输出时不崩溃 | 仅能打开普通 GTK 窗口；无法将 IPC 输出与 GDK 输出可靠对应 |
| 窗口状态与基本控制 | 枚举稳定身份、标题／应用标识（允许缺省）、焦点、窗口与工作区关系；收到变化或可靠重读；按身份请求激活／关闭并能识别失败 | 只提供标题列表；只能对“不确定的当前窗口”发破坏性动作 |
| 工作区导航 | 显示本机模型中的工作区／可见／焦点状态，选择已有工作区；不强求整数、每屏独占或固定 MRU | 捏造不属于合成器的工作区，或把全局桌面显示为独立每屏集合 |
| 启动与常用入口 | 能启动应用；普通组合快捷键可打开 Launcher、控制中心和通知；应用重开至少能正常 present | 必须监听全局原始键盘才能使用；没有单按 Super 就无法打开入口 |
| 会话和配置 | 明确选择目标及依赖，配置可持久保存且能说明是否生效；不改写其他桌面的配置；不抢占已有服务 | 生成 Hyprland 文件后声称已配置其他合成器 |
| 可靠锁定 | 已验证的 ext-session-lock＋认证链，或目标桌面提供并已验证的外部锁服务；错误可见 | 普通全屏密码窗口；命令存在但未确认真正锁住会话 |
| 错误与恢复 | 空状态、权限不足、不支持、断线、动作失败可区分；断线后不向过期身份发动作；重连重建状态 | 静默显示旧数据并继续接受操作；动作已发送就宣称成功 |

托盘、通知、系统服务和应用启动多数不由合成器控制，沿用原有能力，但必须确认
服务名与现有桌面不冲突。没有 NetworkManager 等独立服务时，仍按现有设计显示
不可用；不因此伪装成合成器失败。剪贴板基础复制可保留，历史监听和自动粘贴
另算能力。单 seat 为首次支持范围，多 seat 明确未支持。

### 可选能力及建议的 UI／配置行为

| 不纳入最低基线的能力 | 能力缺少或尚未验证时的建议 |
| --- | --- |
| 空间化 Overview、实时预览、后台窗口捕获 | Overview 入口打开文字 Windows 列表；不留永久加载动画，不显示旧会话纹理。搜索保留；New、拖放和强制结束只在分别验证后出现 |
| 完整 MRU、每屏非焦点标题 | 缺历史时采用稳定的现有顺序并说明；某屏无法确定活动窗口时留空标题，不借用另一屏标题 |
| 无 warp 聚焦 | 普通激活可用；说明指针移动由合成器设置决定，不为一次点击临时篡改全局偏好 |
| 屏幕／键盘／触控板配置、镜像、合盖联动 | 相应设置项只读或禁用并说明由目标桌面管理；已有 inua 值保留，但标为未应用；不写 hypr 配置作为假成功 |
| 快捷键录制、单按 Super、按住 Super+Tab 的槽位 | 提供普通组合键接入说明；无法安全暂停外部绑定时禁用录制。保留救援入口，不接管目标合成器原有 Overview 绑定 |
| 截图、取色、录屏、共享选择 | 仅显示已验证的屏／区域／窗口模式；其余禁用并说明缺工具、协议或权限。外部 portal 可作为独立路径，不能冒充当前 inua picker |
| 手机输入、剪贴板自动粘贴、听写插入 | 手机输入入口禁用；剪贴板仍可复制；听写保留转写文本和复制，提示位置可用角落。不得把地址粘贴悄悄降成向任意当前焦点发键 |
| 应用搬到当前工作区、附加面板精确放置、Mail 自动平铺 | 正常打开／present，由合成器放置；对应增强配置显示未应用，不以猜测的 PID／几何移动别的窗口 |
| blur、动画抑制、精确 Dock 窗口避让 | 保留 CSS 可表达的外观；背景采样、浮动／全屏避让不保证。配置注明目标不支持，不能称 exclusive zone 已实现智能隐藏 |
| 无屏虚拟输出和远程启动 | 未验证创建、模式、移除和编码器恢复前禁用虚拟屏选项；不能仅因会话存在就宣称可远程连接 |
| desk API 的可选动作 | 调用返回具体不支持或暂不可用；不得成功返回空列表来掩盖断线，不改成猜测当前窗口的命令 |

禁用控件应在所属设置项旁说明原因；若整个功能没有入口，则在相关设置页说明。
这遵循 [界面反馈规则](../design/ui.md#feedback)，不是新 UI 实现。

### 首次运行与异常情况的建议

首次运行的能力检查建议按以下顺序进行，**尚未实现，本次也没有执行**：

1. 确认目标合成器、版本与当前 Wayland 会话。环境变量仅作线索；若多个 IPC
   endpoint 的归属无法确认，要求明确选择，不尝试依次向不同实例发送动作。
2. 只读查询输出、窗口、工作区、焦点和事件通道；记录实际可访问的 Wayland
   globals 与协商版本，以及所需 IPC 方法／插件是否可用。连接成功不等于全部
   能力可用；文档有描述、方法未确认和查询成功应分别显示。
3. 核对锁服务、portal、普通快捷键接入、外部工具版本与配置归属；检查已有通知
   服务等冲突。不以命令存在证明锁定、捕获或输入成功，也不为检查自动加载
   插件、申请捕获、注入按键、锁屏、移动窗口或创建输出。
4. 对照最低基线逐项说明可用、未验证、不支持或暂不可用及其原因。缺少必需项
   时不标为支持的桌面会话；可选项缺失只影响对应入口。实际动作的成功仍须由
   返回结果和后续状态确认，不能用首次检查结果永久放行动作。

| 情况 | 建议行为；均尚未实现 |
| --- | --- |
| 首次运行，无目标／版本证据 | 显示尚未支持或尚未验证的目标与缺项；不运行 Hyprland 初始化、不覆盖现有会话。缺核心基线时不得宣称完整桌面可用；普通应用可单独使用 |
| IPC 首次尚未返回 | 显示正在连接；没有窗口数据前不显示“No windows”。无 endpoint、拒绝访问与解析失败分别说明 |
| 已成功查询，窗口为零 | 窗口页显示无窗口，固定应用仍能启动；焦点可空。只按目标真实模型展示空工作区；不凭空创建 New ID |
| 输出为零或输出移除 | 不创建无归属表面，不截图；取消依赖消失输出的选择流程。输出重新出现后重新匹配；不将无输出误报成 IPC 断线 |
| 协议／方法未提供 | 标为不支持；插件未启用时指出插件前提。保留不依赖它的功能，避免持续高频重试必然缺少的能力 |
| 权限被拒或用户取消授权 | 拒绝显示在发起动作处；取消保持为取消。不要通过改环境、加载插件或换低层输入／捕获路径绕开授权 |
| IPC 丢失 | 标示连接中断；旧列表若保留只能作为不可操作的旧数据。暂停窗口动作与可选捕获；独立的应用启动、时钟等可继续 |
| IPC 恢复／合成器换实例 | 确认是同一 Wayland 会话的目标，重新获取完整状态和能力，丢弃旧身份、待执行动作与纹理；不重放移动、关闭或输入。GTK 显示连接死亡不承诺原进程自动恢复 |
| 目标窗口消失或动作拒绝 | 在动作所属页面显示原因并刷新；取消恢复焦点失败时不聚焦替代目标。Launcher 不应先完成再仅写日志；此处不同于当前实现 |
| 多步动作部分成功 | 显示已完成和未完成的部分，以合成器新状态为准；不显示永久的乐观投影，不盲目执行相反动作 |
| 写配置成功，应用／重载失败 | 保留“已保存、未生效”及错误；不把保存当成功，不自动切换另一套配置语法 |
| 锁请求失败或 locker 崩溃 | 未取得锁不得显示已锁定；已经锁住时不尝试普通窗口替代或自行解锁。恢复和新增输出保护须由合成器／locker 的实测结果决定 |

## 哪些能力需要插件、脚本或内部政策

Wayland 普通客户端无权从自己的 GTK 键盘事件观察整个桌面，也不能靠 CSS
实现其他表面的背景模糊。以下前提必须单独确认，不能用同名动作推导等价行为。

| 目标 | 已有外部途径 | 额外前提与不可访问的边界 |
| --- | --- | --- |
| Hyprland | classic／Lua IPC，专有 mapping／export | 当前 Super 行为已经在 compositor 内运行 Lua；不是可移植的外部键盘监听。锁恢复、warp、规则有独立政策 [H] |
| Sway | IPC 可满足基本列表、导航和控制，无需新 compositor 插件 | mode／release 绑定不证明 Hyprland 的 raw listener 语义；沙箱过滤 layer／capture／input globals。背景 blur、精确布局增强无本次证据 [S2][S5] |
| niri | IPC 覆盖基本状态与按 ID 动作，无需新 compositor 插件；KDL 注册普通绑定 | 捕获 ID 关联、逐动作无 warp、录制隔离未证；官网的受信任主机权限不等于沙箱权限。新无障碍输入能力不能未经版本与权限核对就当作按键监听替代 [N1][N5][N8] |
| Wayfire | **必须启用已有 `ipc`＋`ipc-rules`** 才有本文使用的方法；导航另需 `vswitch`，部分窗口动作需 `wm-actions`／grid／tile，启动命令需对应插件 | `ext-toplevel` 是 0.11 新插件；security-context 可以隐藏特权 global。插件卸载会移除方法；全局键、场景图、精确变换或身份关联若需新增插件，会增加版本维护成本，目前不作必须新增的结论 [W1][W7][W9][W10][W11] |
| KWin | 获准的 Plasma window 协议可管理窗口；完整脚本视图可访问 workspace／window 对象 | 使用脚本就需要安装、启用、卸载及版本维护；普通 D-Bus 不能替代完整状态流。窗口管理、fake-input、keystate、screencast 等被接口过滤，需正确应用标识与 `X-KDE-Wayland-Interfaces` 权限声明；输入法连接另有限制。声明存在仍不证明部署环境允许。不得关闭检查来满足基线 [K1][K2][K8] |

KWin 的 JS 工作区能力不等于任意 C++ 效果或输入插件能力；Wayfire 的 IPC
回调也不是通用场景图远程接口。若完整复刻必须依赖未公开内部 API、权限例外或
自编插件，应作为 owner 另选的可选范围，而不是悄悄加入最低基线。

## 首个新增目标的建议与 owner 决策

| 目标 | 有利证据 | 首次支持的主要成本／缺口 | 本次建议 |
| --- | --- | --- | --- |
| niri | 26.4.0 的窗口、工作区、焦点、MRU 与明确 ID 动作；官网有 layer-shell、锁、捕获与 portal 路径；基本控制不需自编插件 | 滚动几何、动态工作区、layer 焦点空值；捕获身份关联未知；配置和普通键接入需另做 | **首选，限最低基线**；首先接受文字窗口列表和外部设置，不承诺完整 Overview |
| Wayfire | 0.10 有可核对的状态／事件／控制和虚拟输出；0.11 增加捕获、标准窗口列表及输出布局变化事件 | 插件集合是实际依赖；wset 网格与变换几何不同；0.11 的单窗捕获组合证据不足 | #61 与 niri 同等比较；若插件契约和关键交互更可靠，可由 owner 改选 |
| Sway | 1.12 的树、工作区、动作、事件及 ext 捕获身份链证据完整；基本控制无需插件 | 树形焦点不是完整全局 MRU；输入录制、单按 Super、配置迁移仍需处理；不能以“wlroots”省略验证 | 保留为低风险备选；这不是否定其能力，也不是固定排在 niri／Wayfire 后实现 |
| KWin | 脚本和 KDE 扩展有完整桌面管理基础，portal／PipeWire 能力明确 | KDE 权限、脚本部署、KScreenLocker、全局桌面与现有 Plasma 服务协调，首次范围更大 | 不建议作为首个目标；普通应用在 KWin 上运行是另一个较小范围 |

选择 niri 是产品取舍：按这里的最低基线，基本窗口／工作区控制不需要新增内部
插件，并能尽早检验当前最明显的固定几何和整数工作区假设。它**不是**协议覆盖
最多或迁移工作最少的已测结论；若 owner 优先要求完整预览身份链，Sway 1.12
的现有源码证据反而更强。若要求保留效果与可扩展策略，Wayfire 更值得优先验证。

owner 需要决定是否接受：文字 Windows 作为首次 Overview、显示／输入设置由
目标桌面管理、普通组合键代替特殊 Super 行为，以及锁使用原生服务的许可范围。
owner 先决定是否接受基线和候选建议，再另行开展 #61 的 niri／Wayfire 配对
实验；根据结果确认首个实现目标及精确产品／插件／portal 版本。该后续任务
需回答的缺口包括：窗口身份与焦点、工作区语义、几何、层表面、权限、断线和锁。
这里没有开始实验、给出新接口或自动开启后续实现。未收到 owner 决定前，本文仅为建议，不更改支持声明。

## 本次检查

文档覆盖 FT58 的 23 行，并保留了协议、专有命令、插件、权限与未知项的区别。
固定版本源文件和官方文档已阅读；覆盖、链接和测试结果如下。

### 仓库检查结果

仅修改本文、FT58 清单的后续链接与架构页链接，没有 Go／Blueprint 改动，
不新增测试，无需 `make i18n` 或 `make generate`。未安装工具或依赖，
没有非固定版本安装；复用父构建目录中的 Go 模块和构建缓存，禁止联网补取依赖。

使用 Go **1.26.1**；全量执行原有 `make test`，用 `GOFLAGS="-count=1 -v"`
禁用测试结果缓存并记录跳过项。测试 HOME、XDG config／data／state／cache／
runtime 和日志都位于父构建目录的独立临时位置。此前 TMPDIR 也位于该目录；
最终全量复核取消 TMPDIR，使用系统默认临时目录，避免 Unix socket 路径过长。
子进程使用清空后重建的环境，未继承 Wayland、DISPLAY、Hyprland 或真实 D-Bus
地址；session／system bus 地址都指向不存在的隔离 socket，测试按需启自己的
私有 bus。测试 PATH 排除合成器程序，因而不执行 `Hyprland --verify-config`；
Python 会话测试使用自身的假 compositor／systemctl。没有连接真实服务、修改
网络配置、凭据、真实运行时目录或 voxtype，也未启动 UI／嵌套会话。

| 检查 | 结果 |
| --- | --- |
| 此前 `make test` | **退出码 2**。Python **15 tests / OK**（0.339s）；`go vet ./...` 通过；`CGO_ENABLED=1 go test -race ./...` **91 包通过、12 包失败、52 个顶层测试失败、48 个顶层测试跳过** |
| 失败原因 | 临时目录在构建目录内，部分测试的 Unix socket 路径过长，报 `bind: invalid argument`；私有 dbus-daemon 也出现 `EOF`。没有把这些结果记为通过 |
| 仅复核上述 12 个失败包 | 同样隔离环境，以测试运行器持有的目录 fd 提供短 TMPDIR 入口，实际文件仍在父构建目录内；`CGO_ENABLED=1 go test -race -count=1 -v` **退出码 0，12 包通过，0 失败，9 跳过**。消除了相同包的 socket／D-Bus 失败；这不是全量 `make test` 通过 |
| 最终全量 `make test`（取消 TMPDIR） | **退出码 0**。Python **15 tests / OK**（0.340s）；`go vet ./...` 通过；`CGO_ENABLED=1 go test -race ./...` **103 包通过、33 包无测试文件、0 失败**；顶层测试 **1164 通过、48 跳过**，另有 **6 个子测试跳过**。此前 socket／D-Bus 路径错误未再出现 |
| 文档检查 | FT58 的 23 行名称和顺序完全对应，五个合成器列齐全；本地文件／标题链接、47 个引用定义及 `git diff --check` 均通过。46 个外部来源 URL 均返回 HTTP 200；HTTP 成功只说明链接可访问，不证明兼容性 |

失败后复核的包为 `internal/` 下的 `apps/connect`、`apps/files/chooser`、
`apps/files/disks`、`apps/files/fm1`、`apps/files/remote`、`dbusexport`、`hypr`、
`keyring`、`portal`、`sni`、`system`、`term/host`。

最终全量测试的 48 个顶层跳过项与此前相同，如下；不将缺少依赖解释成
兼容性结论。另有 6 个子测试跳过：`vt.TestSnapshot` 下的 scrollback、cleared、
wrapped、alternate 缺 libinua-vt.so；`workshop.TestSessionJS` 的 node、bun
不在测试 PATH。前四项的父测试也计入上述 48 项。

| 路径 | 跳过项 | 原因 |
| --- | --- | --- |
| [viewer_test.go](../internal/apps/viewer/viewer_test.go) | `TestTurbo` | 缺 libturbojpeg |
| [hyprconf_test.go](../internal/hyprconf/hyprconf_test.go)、[displays_test.go](../internal/hyprconf/displays_test.go) | `TestRenderVerifies`、`TestRenderDisplaysVerifies` | 为遵守不执行合成器的范围，Hyprland 不在测试 PATH；另有测试内的条件验证分支也未执行 |
| [host_test.go](../internal/term/host/host_test.go) | `TestTitleAndFolder`、`TestGitEvents`、`TestAnswers`、`TestClipboardEvent`、`TestRemember`、`TestAgents`、`TestNotify`、`TestSnapshotAttach` | 缺 libinua-vt.so |
| [shellint_test.go](../internal/term/shellint/shellint_test.go) | `TestBash`、`TestZsh` | 缺 libinua-vt.so |
| [vt_test.go](../internal/term/vt/vt_test.go) | `TestText`、`TestWideAndWrap`、`TestTitleAndPwd`、`TestResize`、`TestFrame`、`TestFrameDirty`、`TestFrameScrolled`、`TestFrameGrows`、`TestFramePassword`、`TestKeys`、`TestKeyModes`、`TestSelect`、`TestSelectClicks`、`TestSelectWrapped`、`TestKeysym`、`TestPaste`、`TestAnswers`、`TestClipboardWrite`、`TestScroll`、`TestMouse`、`TestSearch`、`TestPrompts`、`TestInputAfterRedraw`、`TestTail`、`TestNotifications`、`TestDefaults`、`TestLinks`、`TestPalette`、`TestSnapshot` | 缺 libinua-vt.so |
| [host_test.go](../internal/term/host/host_test.go)、[pty_test.go](../internal/term/pty/pty_test.go) | `TestHelperReport`、`TestHelperDetach`、`TestInterruptHelper` | 由父测试启动的 helper，顶层按设计跳过 |
| [shellint_test.go](../internal/term/shellint/shellint_test.go) | `TestFish` | 缺 fish |
| [isodownload_test.go](../internal/vm/isodownload_test.go) | `TestISODownloadRealImage` | 未设置真实 ISO 下载地址 |
| [workshop_test.go](../internal/workshop/workshop_test.go) | `TestJSModule` | 测试 PATH 没有 node |

定向复核的 9 个跳过来自 term/host：上述 8 个引擎测试与 `TestHelperReport`。
五个合成器的真实协议协商、焦点／滚动／窗口移动、多屏和输出热插拔、捕获与
权限、锁恢复、录制中崩溃、portal、虚拟输出／远程编码仍全部未测。

## 来源索引

下列链接直接指向上文使用的文档或源文件，不以协议 XML 或搜索摘要充当实现
证据。固定版本范围见开头；niri 官网、KDE 开发文档、Arch 手册为审阅日的滚动
文档，不能据此假定旧产品版本已支持所有行为。外部辅助程序的安装版本、实际
协议协商和权限结果均未知；不以可执行文件存在代替验证。

[H]: hyprland-coupling.md
[S1]: https://sources.debian.org/data/main/s/sway/1.12-1/sway/sway-ipc.7.scd
[S2]: https://sources.debian.org/data/main/s/sway/1.12-1/sway/server.c
[S3]: https://sources.debian.org/data/main/s/sway/1.12-1/sway/input/input-manager.c
[S4]: https://man.archlinux.org/man/sway-output.5.en
[S5]: https://man.archlinux.org/man/sway.5.en
[S6]: https://man.archlinux.org/man/sway-input.5.en
[S7]: https://sources.debian.org/data/main/s/sway/1.12-1/sway/commands/create_output.c
[S8]: https://sources.debian.org/data/main/s/sway/1.12-1/sway/lock.c
[N1]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/lib.rs.html
[N2]: https://docs.rs/niri-ipc/26.4.0/niri_ipc/struct.Workspace.html
[N3]: https://niri-wm.github.io/niri/Workspaces.html
[N4]: https://niri-wm.github.io/niri/Screencasting.html
[N5]: https://niri-wm.github.io/niri/Security-Model.html
[N6]: https://niri-wm.github.io/niri/Layer%E2%80%90Shell-Components.html
[N7]: https://niri-wm.github.io/niri/Configuration:-Outputs.html
[N8]: https://niri-wm.github.io/niri/Configuration:-Input.html
[N9]: https://niri-wm.github.io/niri/Important-Software.html
[N10]: https://niri-wm.github.io/niri/Configuration:-Key-Bindings.html
[N11]: https://niri-wm.github.io/niri/Configuration:-Introduction.html
[N12]: https://docs.rs/niri-ipc/26.4.0/niri_ipc/struct.WindowLayout.html
[N13]: https://niri-wm.github.io/niri/IPC.html
[W1]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules.cpp
[W2]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules-common.hpp
[W3]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/src/core/core.cpp
[W4]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/src/view/layer-shell/layer-shell.cpp
[W5]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/protocols/session-lock.cpp
[W6]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/src/core/output-layout.cpp
[W7]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/vswitch/vswitch.cpp
[W8]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-utility-methods.hpp
[W9]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/wm-actions/wm-actions.cpp
[W10]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/wayfire.ini
[W11]: https://wayfire.org/2026/07/24/Wayfire-0-11.html
[W12]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/wayfire-portals.conf
[W13]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-events.hpp
[W14]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/ipc.cpp
[K1]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/wayland_server.cpp
[K2]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/scripting/workspace_wrapper.h
[K3]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/wayland/layershell_v1.cpp
[K4]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/wayland/plasmawindowmanagement.cpp
[K5]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/plugins/screencast/screencastmanager.cpp
[K6]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/plugins/eis/eisbackend.cpp
[K7]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/wayland/outputmanagement_v2.cpp
[K8]: https://invent.kde.org/plasma/kwin/-/raw/v6.5.2/src/org.kde.KWin.xml
[K9]: https://develop.kde.org/docs/plasma/kwin/api/
[P1]: https://man.archlinux.org/man/xdg-desktop-portal-wlr.5.en
[P2]: https://invent.kde.org/plasma/xdg-desktop-portal-kde/-/raw/v6.5.2/data/kde.portal
