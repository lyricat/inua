# 按用户功能审计 Hyprland 耦合

本次调查对应 #59（FT58），审阅日期为 2026-11-04，初次源码基线为 `4ad7126`；
第二轮修订已合并 `origin/master` 的 `89e121e`，并复核剪贴板与听写路径。
owner 已允许此次调查进入
factory；#60–64 不在此次范围。本文只记录依赖、已有行为和待验证的替代方向，
不定义接口、不改变运行行为，也不承诺其他合成器的兼容性。

后续 #60（FT67）的[合成器能力比较与最低支持基线](compositor-capabilities.md)
沿用本清单，比较 Hyprland、Sway、niri、Wayfire 和 KWin；其中的退化行为和
首个新增目标均为待 owner 决定的建议，不改变本文记录的现有实现。

证据分为四类：**源码确认**指已追到调用方和底层实现；**推断**指从实现推导的
迁移风险；**候选**指值得继续调查的协议或行为调整；**未知**指本次没有运行
证据。源码和模拟服务测试不能证明
真实合成器上的互操作性。以下实现描述均为源码确认，替代栏全部是候选，未在
其他合成器上验证。协议 XML 只用于核对协议语义和版本，不作为服务端支持证明。

后续 [FT68 niri／Wayfire 独立 IPC 实验](../experiments/compositor/README.md)
记录固定版本来源、假 socket 检查和待做的嵌套步骤；不改变本文的支持范围，
也不把 pending FT67 基线视为已实现。真实 compositor 验证仍未完成。

## 功能与依赖矩阵

源码链接相对本文件；每项同时给出符号，便于代码移动后重新查找。
“现有退化”是已实现行为，“可考虑”是尚未实现的取舍。

| 用户功能 | UI／状态 → 数据、动作、事件的源码和符号 | 依赖与语义 | 替代方向（未验证） | 限制／现有退化 |
| --- | --- | --- | --- | --- |
| 窗口列表、标题、运行中的应用 | [shell.go](../internal/ui/shell.go) `hyprHandler.OnSnapshot`, `shellUI.apply` → [store.go](../internal/hypr/store.go) `Store.session`, `refresh` → [snapshot.go](../internal/hypr/snapshot.go) `Client.Query`, `BuildSnapshot`；[launcher.go](../internal/shell/launcher.go) `searchWindows`；[dock.go](../internal/shell/dock.go) `Dock`, `AppIndex.ForClass` | IPC 的地址、PID、class、title、mapped、workspace、monitor；`focusHistoryID` 决定 MRU。class 用来匹配 desktop entry，并不直接等于 desktop 文件名。事件触发全量重读 | foreign-toplevel 的列表／动作，加目标合成器提供的工作区关系与焦点信息；见下文协议边界 | 只过滤未 mapped 的窗口，不统一过滤 hidden；地址只在实例内有意义。断线保留快照，不自动清空 Dock／标题 |
| Bar 工作区、滚轮切换、各屏标题 | [bar.go](../internal/ui/bar.go) `bar.setHypr`, `scrollWorkspaces` → [bar.go](../internal/shell/bar.go) `BarWorkspaces`, `BarTitle`；[overview.go](../internal/shell/overview.go) `Overview.BarWorkspaces` → [shell.go](../internal/ui/shell.go) `switchWorkspace` | 正整数 ID 是普通工作区；每个工作区归属一个 monitor；active 与 focused 不同；非焦点屏标题取其可见工作区中最近聚焦的窗口 | `ext-workspace-v1` 的分组与能力信息，另补窗口归属和焦点关系 | Bar 保留空工作区，Overview 不保留。GDK connector 找不到 IPC monitor 时 Bar 显示全部普通工作区。数字 ID 与字符串名字在 Lua 中不是同一种选择器 |
| 多屏 Bar、背景、Dock、弹层位置 | [shell.go](../internal/ui/shell.go) `start`, `focusedMonitor`, `bindHandlers`；[bar.go](../internal/ui/bar.go) `syncBars`, `dropBar`, `bar.connector`；[apply.go](../internal/ui/apply.go) `syncWallpaper` | GDK monitor 对象决定表面生命周期，connector 名与 Hyprland monitor 名联接；IPC focused monitor 决定弹层目标。新 GDK monitor 的 connector 可能稍后才出现 | 保留 GDK 的输出生命周期；另查目标合成器的焦点屏定义与输出标识 | `focusedMonitor` 找不到时返回 nil，位置由调用方／合成器决定。无输出时应用 `Hold` 保活，无 Bar；不等于能在无输出情况下完成所有交互 |
| 屏幕模式、缩放、镜像、合盖 | [settings_displays.go](../internal/ui/settings_displays.go) `showDisplays`, `changeDisplay`, `layOutDisplays` → [monitors.go](../internal/display/monitors.go) `Outputs`, `Output.Rule`, `ValidScale` → [displays.go](../internal/hyprconf/displays.go) `renderDisplays`, `renderDisplaysLua`, `Lids`；[display.go](../internal/display/display.go) `System.PanelsOff` | `j/monitors all` 含禁用输出；配置用 connector 或 `desc:`；缩放按 1/120 寻找整逻辑像素。合盖来自内核／logind，设备名和配置应用仍依赖 Hyprland | 输出管理协议或目标合成器配置；保留独立的物理合盖检测 | 隐藏 `INUA-VIRTUAL`, `FALLBACK`, `HEADLESS-*`。IPC 失败只记日志，再显示“No displays”及连接提示；无可用通用输出配置后端 |
| Launcher 聚焦、取消和窗口命令 | [launcher.go](../internal/ui/launcher.go) `cancelLauncher`, `executeLauncher`, `runCommand` → [launcher.go](../internal/shell/launcher.go) `Launcher.Show`, `Cancel`, `Execute`, `Done` → [shell.go](../internal/ui/shell.go) `focusWindow`, `dispatchEither` | 打开时记住地址；取消仅恢复快照中仍存在的窗口；聚焦可走 classic 或 Lua。移动、关闭、浮动、全屏、居中等命令仍有 classic 路径 | foreign-toplevel 的激活／关闭请求；其他动作需单独调查目标合成器控制能力 | 选中窗口后异步发请求并立即 `Done(nil)`；聚焦失败只记日志，**不会保持 Launcher 并显示错误**。模型能检测快照中已消失的窗口，但不能覆盖请求期间的消失 |
| Dock 点击窗口时保持指针位置 | [dock.go](../internal/ui/dock.go) `focusWindowHere`, `focusQuietly`；[dock.go](../internal/shell/dock.go) `DockTarget` | 临时修改 `cursor:no_warps`：Lua 用 `pcall` 后恢复；classic 查询选项后发 `[[BATCH]]keyword…dispatch…keyword…` | 目标合成器的无指针跳转激活行为 | classic 只在回复含精确子串 `"int": 0` 时包围修改，且批处理只检查传输错误，未逐条校验回复。并非通用 Wayland 聚焦事务 |
| Overview 浏览、搜索、MRU、每屏 New 卡片 | [overview.go](../internal/ui/overview.go) `showOverview`, `renderOverviewView`, `buildOverview` → [overview.go](../internal/shell/overview.go) `Overview.Show`, `refresh`, `WorkspaceSlot`, `ReserveWorkspace` | Lua 能力探测决定是否启用；按工作区 ID 收集窗口，按 monitor 分组；MRU 历史与 `focusHistoryID`，打开期间冻结次序；只显示有窗口的普通工作区，另给每屏 New | 工作区枚举加窗口归属；可考虑先保留文字列表，缺少能力时不提供 New／拖放 | Lua 不可用时 `showOverview` 打开 Launcher 的 Windows 模式；Bar 的 Overview 手势直接不执行。无窗口但有输出时有 New；无卡片时显示“No workspaces”或“No matches” |
| Overview 激活、拖放、新工作区、关闭／强制结束 | [overview_manage.go](../internal/ui/overview_manage.go) `moveOverviewWindow`, `activateOverview`, `guardOverviewMouse`；[overview.go](../internal/ui/overview.go) `createWorkspaceOnMonitor`, `dispatchOverview`；[overview.go](../internal/shell/overview.go) `BeginMove`, `settleMoves`, `projected` | `hl.dsp.focus`, `window.move`, `workspace.move`, `window.close/kill`；激活时先聚焦目标屏；拖放先以 `window.move(..., follow=false)` 移动窗口，再单独请求 `workspace.move` 将目标工作区路由到目标屏；临时取消识别到的 Super+鼠标移动／缩放绑定。正整数 ID 预留与复用 | 需要目标合成器的窗口归属、非跟随移动、工作区创建／屏幕路由；不是列表协议即可替代 | 跨屏移动是多次请求，可能部分成功。UI 暂时投影目的位置，5 秒有效期在下一次模型刷新时结算；没有独立的超时回滚定时器。激活失败保留 Overview，仅记日志 |
| Overview 实时预览 | [overview.go](../internal/ui/overview.go) `updateOverviewCapture`, `queueOverviewFrame`, `drainOverviewFrames` → [stream.go](../internal/capture/hyprland/stream.go) `Start`, `client.init`, `read`, `capture`；[schedule.go](../internal/capture/hyprland/schedule.go) `captureSchedule.next` | 独立 Wayland 连接；wlr foreign handle → Hyprland mapping → 64 位 IPC 地址 → Hyprland export；SHM 像素送回 GTK。不是截取屏幕上可见矩形 | `ext-foreign-toplevel-list` 加 `ext-image-capture-source`／`ext-image-copy-capture`；还需证明与窗口／工作区模型的身份关联 | 无协议不改走截图；保留旧纹理或空预览及标题。Overview 每秒重建失败的流；无目标／关闭时停止。权限、遮挡、不可见工作区及 XWayland 的实际捕获效果未验证 |
| Dock 避让、自动隐藏及 Overview 上方 Dock | [dock.go](../internal/ui/dock.go) `syncDock`, `newDock`, `bindDockHandlers`, `syncOverviewDock`；[overview.go](../internal/ui/overview.go) `overviewDockInsets`；[layershell.go](../internal/layershell/layershell.go) `AutoExclusiveZone`, `SetExclusiveZone`, `SetLayer` | 常显 Dock 请求自动 exclusive zone；自动隐藏时 zone=0，以 pointer enter/leave 控制 Revealer。Overview 可升 Dock 到 Overlay 并为其留出内边距 | 继续使用 layer-shell；若要求精确浮动窗口避让或智能隐藏，需要额外合成器策略／几何信息 | 当前没有按应用窗口与 Dock 相交来隐藏或移动窗口的逻辑。exclusive zone 是合成器的布局提示，不保证每个浮动／全屏窗口避让；无 layer-shell 时不创建 Dock |
| 普通快捷键及设置录制 | [shortcuts.go](../internal/ui/shortcuts.go) `hyprBinds`, `setShortcut`, `startRecording`, `stopRecording` → [keys.go](../internal/hyprconf/keys.go) `ParseBinds`, `renderShortcuts`, `renderShortcutsLua`, `RecordSubmap` | 读 `j/binds`；保存替换键、unbind/bind 和命令；录制时进入 `inua-record` submap，退出 reset。配置重载后重读生效值 | 目标合成器的绑定配置；可考虑独立快捷键注册服务，但须确认释放、冲突、录制和救援键语义 | 无 client 时录制 UI 仍可运行，但不能禁用外部快捷键；IPC 失败只记日志。绑定由合成器持有，不依赖 shell 持续读键盘 |
| 单按 Super、Super+Tab、数字槽位 | [overview_keys.go](../internal/ui/overview_keys.go) `probeLua`, `installOverviewKeys`, `overviewKeyScript`, `overviewNativeKeyScript`, `overviewReleaseKeyScript`, `overviewEvent`, `pauseOverviewKeys` | `eval` 安装 `hl.on("input.keyboard.key")`；133/134 是 Super，槽位用键码 10–19；通过 `custom>>inua-overview,…` 回传。连接成功／configreloaded 重新安装 | 合成器内按键观察、修饰键状态、绑定冲突规则及自定义消息 | 依次退为 4 ms `hl.is_key_down` 轮询、释放绑定／shadowing；前者可能漏极短按键。仅 Lua 探测成功不证明所有脚本能力存在；安装失败记日志。退出停止 listener/timer，不恢复全部被替换绑定 |
| 截图、取色、窗口选择、录屏 | [screenshot.go](../internal/ui/screenshot.go) `openShot`, `shootNow`, `logicalSize`, `shownWorkspaces`, `recordArgs`；[screenshot.go](../internal/shell/screenshot.go) `ShotWindows`, `Rect.Pixels`；[shotpick.go](../internal/ui/shotpick.go) `startPickerStream`, `captureWindow`；[record.go](../internal/ui/record.go) `startScreenRecording` | `j/clients`／`j/activewindow` 和 monitor 几何；`grim -o/-g`；`wf-recorder`；窗口卡片使用同一 Hyprland export 流 | 屏幕捕获协议／portal 加窗口捕获，需另证坐标、交互选择与权限语义 | 工具或请求失败通常记日志／通知；窗口 picker 的流失败只记日志，无 Overview 的每秒重试。单窗口捕获有超时。`openShot` 对 nil client 未防护，不能宣称无 IPC 时完整降级 |
| Portal 截图与屏幕共享选择 | [portal.go](../internal/portal/portal.go) `Export`, `NewRequest`；[portal_share.go](../internal/ui/portal_share.go) `askShare`, `portalShare`；[portalconf.go](../internal/portalconf/portalconf.go) `Dirs.conf`；[xdph.go](../internal/portalconf/xdph.go) `setSharePicker` | inua 服务部分 D-Bus portal；ScreenCast 捕获仍由 xdg-desktop-portal-hyprland 做。picker 返回 `screen:NAME`, `window:0x…`, `region:NAME@x,y,w,h`；默认路由 `hyprland;gtk` | 目标桌面的 portal 后端及其选择器协议 | portal 的 D-Bus 接口并未消除后端地址与输出名耦合；用户取消可返回取消，缺协议没有通用 ScreenCast 替代实现 |
| 锁屏、锁定期间输出变化、重启恢复 | [lock.go](../internal/ui/lock.go) `lockScreen`, `bindLockHandlers`, `lockEnded`, `lockMarked`；[bar.go](../internal/ui/bar.go) `syncBars`；[layershell.go](../internal/layershell/layershell.go) `LockSupported`, `AssignLockWindow`；[hyprconf.go](../internal/hyprconf/hyprconf.go) `Render`, `RenderLua` | ext-session-lock，经 gtk4-layer-shell；锁状态 marker；Hyprland `misc.allow_session_lock_restore`；库 1.2 的 monitor signal，旧库通过 GDK 列表分配锁表面 | ext-session-lock 加目标合成器的锁恢复政策 | 无协议／无认证 helper 就报错，无普通窗口假锁。锁持有时暂停创建普通 Bar，解锁再同步；崩溃后的可恢复性不是协议对任意合成器的保证 |
| 手机触控板／键盘 | [input.go](../internal/apps/connect/input.go) `remoteInput.input`, `packet` → [vinput.go](../internal/vinput/vinput.go) `Open`, `Input.init`, `drain`, `Err` | 独立 Wayland 连接；seat、虚拟指针、虚拟键盘全部需要；XKB keymap 经 memfd 上传 | 同协议且获目标合成器许可，或目标桌面授权输入机制 | 无任一 manager 时整次 Open 失败；发送失败关闭连接，下一个输入包再开；不自动重放失败输入；多 seat 未建模 |
| 剪贴板历史的复制与粘贴 | [clipboard.go](../internal/ui/clipboard.go) `openClipboard`, `pasteClipboard` → [dock.go](../internal/ui/dock.go) `focusQuietly` | `wl-copy` 写入选中内容；正常粘贴先恢复打开面板时的窗口焦点，再用 classic `sendshortcut`／Lua `send_shortcut` 按地址发送 Ctrl+V，已识别终端加 Shift | 单独调查目标桌面的剪贴板与授权输入；保留指定窗口粘贴还需焦点和窗口身份能力 | `copy` 只复制；有 client 但目标地址为空时也只复制。`client == nil` 时已有 `wtype` 回退，向当前焦点发送粘贴键，不恢复或锁定地址目标；工具失败只记日志。详见下文 |
| 听写插入及提示位置 | [voice.go](../internal/ui/voice.go) `dictationTyper.paste`, `applyVoiceState`；[dictation.go](../internal/ui/dictation.go) `placeDictation`, `activePlace`, `dictationFailed` | 插入时查询 `j/activewindow`，以 `wl-paste`／`wl-copy` 暂存和写入文本，再按地址发送粘贴快捷键；提示定位读取窗口／屏幕几何，录音状态切换 Hyprland submap | 单独调查听写的目标窗口、授权输入、提示定位和录音快捷键；手动粘贴是可考虑的退化 | 无 client 或无活动窗口地址即返回错误，**没有 `wtype` 回退**；错误显示在听写卡片，否则发通知／记日志。不是 vinput 的手机输入路径，也不能从转写引擎可用推断插入可用 |
| 通知激活、文件选择器父窗口 | [notify.go](../internal/ui/notify.go) `activationToken`；[appid.go](../internal/gtkx/appid.go) `SetAppID`, `SetForeignParent` | GDK launch context 的激活 token；`wayland:HANDLE` 的 xdg-foreign 父窗口；应用 ID 还用于 Hyprland window rule | 保留 GDK／标准扩展路径，核对目标权限与父子关系 | token 可为空；不支持的 parent 返回 false，非 Wayland 上相应操作无效。实际协议协商版本由 GDK 决定 |
| 应用重开、设置附加面板、Mail 平铺 | [app.go](../internal/appui/app.go) `bringHere` → [bring.go](../internal/hypr/bring.go) `BringHere`, `WindowOf`, `PlaceWindow`；[settings_panel.go](../internal/ui/settings_panel.go) `showPanel`；[panel.go](../internal/lightui/panel.go) `sidePanel.init`, `open`；[room.go](../internal/hypr/room.go) `RoomRight`；[window.go](../internal/mailui/window.go) `tileWindow` | 按 PID 查 mapped window，移动到当前工作区；读 `reserved`、outer gap、变换与缩放估算右侧空间；classic 移动／缩放／settiled | 应用正常 present／对话框布局；保留同样放置行为则需合成器控制 | 这些调用不经过 shell store，错误多为日志或放弃增强；PID 首个匹配、classic dispatcher 兼容性需单独核对 |
| Assistant／MCP／Workshop 的 desk API | [desktop.go](../internal/desk/desktop.go) `Local` → [tools.go](../internal/desk/tools.go) `windows`, `windowTools`, `workspaces`；[watch.go](../internal/desk/watch.go) `WatchTool` | 直接读 IPC 并执行 classic dispatcher，不走 Store；窗口 focus 用 `focusHistoryID==0`；窗口主题订阅是轮询，不是事件流 | 必须连同 UI 之外的控制消费者一起调查；保留工具的明确错误与动作语义 | 无 Hyprland 回调时读操作报错；工作区视图过滤负 ID。首轮轮询只建立基线，读失败跳过，不发断线状态 |
| 登录、初次配置、输入设置 | [inua-session](../scripts/inua-session) 顶层启动流程；[main.go](../cmd/inua/main.go) `checkHyprland`, `checkSource`；[bootstrap.go](../internal/bootstrap/bootstrap.go) `Install`；[hyprland.go](../internal/bootstrap/hyprland.go) `FindShellStart`, `FindSource`；[input.go](../internal/ui/input.go) `applyHyprland` → [hyprconf.go](../internal/hyprconf/hyprconf.go) `Apply` | 启动 start-hyprland／Hyprland；检查 shell 自启动；生成 classic／Lua 配置、设备规则、绑定、手势、窗口和图层规则；D-Bus／systemd 环境传播 | 目标合成器的会话入口、配置与规则语法；独立调查 portal／输入法／锁屏服务 | 不存在跨合成器启动分支。没有加载生成文件时设置不生效；首次与故障行为见后文 |
| 无物理输出时远程桌面启动 | [sharing.go](../internal/ui/sharing.go) `startSharing`, `followMonitors`, `syncVirtual` → [share.go](../internal/apps/remotedesk/share.go) `Sharing.SyncVirtual` → [virtual.go](../internal/hypr/virtual.go) `SyncVirtualScreen`；[instance.go](../internal/hypr/instance.go) `InstanceOf`, `SessionInstance` | 通过 Hyprland lock 文件找实例；`output create headless`, `output remove`, `keyword monitor` 创建并设定 `INUA-VIRTUAL`；monitor/config/reconnect 后再同步 | 目标合成器专有的虚拟输出生命周期；普通输出配置协议不足以创建输出 | 必须事先启用虚拟屏。实际实现把 `j/monitors all` 中任何非虚拟／非 FALLBACK 输出都当作 other，**禁用但仍连接的物理屏也阻止创建**；不能仅按函数前部注释理解 |

## 状态、错误与生命周期

### 首次连接与 IPC 重连

[client.go](../internal/hypr/client.go) `PathsFromEnv` 必须拿到
`HYPRLAND_INSTANCE_SIGNATURE`，再在运行时目录中定位 `.socket.sock` 与
`.socket2.sock`。shell 没有 signature 时只记录错误，不启动 Store；之后不会
自动寻找新实例。`Client.Request` 每次新建 Unix socket，默认超时 2 秒；
`Dispatch`／`LuaDispatch` 要求回复恰为 `ok`。

[store.go](../internal/hypr/store.go) 的实际顺序是：

1. `Store.session` 启动 `readEvents`，确认事件 socket 已连接。
2. 顺序读取 `j/monitors`, `j/workspaces`, `j/clients`, `j/activewindow`。
   这四个请求不是原子事务；期间收到的事件排队，随后可触发再次读取。
3. `BuildSnapshot` 成功后，以 `reflect.DeepEqual` 判断是否发布，再报连接成功。
   首次结果若等于零值快照，也可能不调用 `OnSnapshot`。
4. 每个原始事件先 `track` 再 `OnEvent`；首个状态事件启动 15 ms 定时器，后续
   事件合并，但不会不断向后延长这个窗口。`Refresh` 可显式请求重读。
5. 读事件或请求／解码失败会结束当前 session，报断线后按 250 ms 起、翻倍至
   5 秒重试；上一次 session 超过 5 秒后重置 backoff。重连仍使用原 socket
   路径，并不重新解析 signature。取消 context 才结束 `Run`。

[events.go](../internal/hypr/events.go) `stateEvents` 包括窗口开关、标题、焦点、
移动、工作区创建／销毁／命名／路由、special workspace、屏幕增减、全屏、
floating、pin、urgent 及部分 v2 事件。不是每个事件都刷新：`configreloaded`,
`custom`, `screencast`, `openlayer`, `activelayout`, `submap` 不在这张表中。
[shell.go](../internal/ui/shell.go) `hyprHandler.OnEvent` 另处理配置、Overview
自定义事件和录屏遮蔽密码；输出配置／虚拟屏处理只显式匹配非 v2 的 monitor
事件。只有 v2 事件的服务端是否足够，不能由 Store 的覆盖推出。

断线不清除 Store 快照、urgent map 或 UI 快照。`urgent` 仅由事件设置，只有
对应 `activewindowv2`／`closewindow` 删除；断线期间漏掉的清除不会由快照重建
自动补足。UI 将 `connected=false` 送给 Launcher，但 `Launcher.Message` 在
已有结果时先返回空字符串，因此旧窗口列表可能仍显示且没有断线提示。
重新连上同一路径会全量重读并安装 Overview 按键；这不证明合成器重启、换实例
或 GTK Wayland 连接丢失也能恢复。

### 空窗口、空输出与协议缺失

| 条件 | 源码中的行为 | 不能据此承诺的行为 |
| --- | --- | --- |
| 没有窗口、`j/activewindow` 为 `{}` | `BuildSnapshot` 接受空活动窗口；Launcher Windows 模式提示无窗口；Dock 仍可有固定应用；Bar 仍可有空工作区；Overview 每屏保留 New | 没窗口不代表 IPC 失败，也不应与无输出合并 |
| GDK 没有输出 | `start` 先 `app.Hold`；`syncBars` 删除消失的屏幕表面，新输出再创建；锁定时先建锁表面 | 无输出时截图、picker、Overview 的所有交互完成／取消路径没有运行验证 |
| IPC 无 monitor 或显示查询失败 | `showDisplays` 最终显示“No displays”；`showShot` 无对应 GDK 表面时清理图片并返回 | 显示设置不能区分“真实无屏”与“查询失败”；不能称所有调用都有可用退化 |
| gtk4-layer-shell 动态库缺失 | `Run` 的 preload／Load 失败返回 1，除非明确使用 `NoLayer` | “协议缺失会退成普通窗口”不包含“库缺失” |
| 库存在但无 layer-shell | [shell.go](../internal/ui/shell.go) `start` 用普通窗口模式并记日志；`syncDock` 不建 Dock；[apply.go](../internal/ui/apply.go) `syncWallpaper` 不建背景 | 普通窗口模式不是完整桌面替代，锁屏也被拒绝 |
| Lua 探测失败 | Overview 转到 Launcher Windows 模式；普通动作中存在 classic 分支 | 没有逐项探测所有 dispatcher，也没有统一版本转换 |
| export／mapping／foreign-toplevel／SHM 缺失 | 预览 worker 返回具体错误；Overview 重试，picker 只记日志；文字与已有纹理仍由 UI 保留 | 不会自动切到另一捕获协议或 portal |
| Wayland 流中的单个窗口消失或帧超时 | `capture` 丢弃失败帧；750 ms 无帧时销毁该帧请求并以后重试；其他连接错误结束流 | 独立流重建由调用方决定；不能笼统写成 capture 包自身会重连 |

预览每 16 ms 尝试调度一个窗口，正常目标间隔 120 ms；有优先目标时其他窗口
间隔 360 ms。这是调度目标，不是保证帧率。SHM 缓冲最多保留四个，帧按地址
合并后交 GTK；关闭时 Stop 取消 worker，正在进行的读取仍受超时约束。
关闭 Overview 不立即清空所有纹理；下次打开才按存活窗口清理。这改善再次打开
的显示，但断线／地址复用时旧图像身份是否仍正确没有测试证明。

### 输出变化与 Overview 表面

[bar.go](../internal/ui/bar.go) `syncBars`／`dropBar` 随 GDK 输出列表增减
Bar、背景和 Dock；屏幕移除也关闭挂在该 Bar 上的面板。
Overview 的工作区卡片随 IPC 快照更新，但每屏视图集合只在
[overview.go](../internal/ui/overview.go) `showOverview` 中按当时的 monitor
建立；`renderOverview` 只重绘已有视图，`closeOverview` 才销毁它们。
[shell.go](../internal/ui/shell.go) `bindHandlers` 的输出变化回调不增删
Overview 视图。因此打开期间插入新屏，不会为它新建 Overview 表面；移除屏幕
也没有对应的应用层视图清理。原表面由 GTK／合成器如何处理仍待实测，不能从
Bar 的热插拔处理推断 Overview 具有相同的生命周期保证。

### 快捷键录制与退出

[shortcuts.go](../internal/ui/shortcuts.go) `startRecording` 请求进入空 submap，
并设置 15 秒的 GTK 定时器；`recordKey` 在完成或取消时调用 `stopRecording`。
[settings.go](../internal/ui/settings.go) `bindSettingsHandlers` 的
`onRecordTimeout` 及 `closeSettings` 也调用它，请求 `reset`。
这是 shell 内的退出路径，不是合成器端的超时恢复：进程崩溃或 IPC 断线时，
不能保证 reset 到达；`OnConnection` 也不重发录制的 reset。现有测试没有证明
这些故障下绑定会恢复，需与普通绑定的持久性分开考虑。

### 剪贴板与听写的不同失败路径

`pasteClipboard` 先关闭历史面板，再调用 `wl-copy`。`copy` 操作到此结束，
即使没有 Hyprland client，也会尝试写入剪贴板；`wl-copy` 缺失或执行失败时，
记录 `clipboard copy` 错误并停止，不发送粘贴键。

粘贴有两条路径。有 client 且目标地址非空时，等待 150 ms 释放面板键盘焦点，
再通过 `focusQuietly` 聚焦原窗口，并向该地址发送快捷键。有 client 但目标地址
为空时只复制；聚焦或发送失败时只记日志，**不会切换到 `wtype`**。只有 `client == nil`
才调用 `wtype -M ctrl -k v -m ctrl`；若保存的 class 被识别为终端，则加入
Shift。此分支不按地址聚焦，快捷键由发送时的当前焦点接收，不能保证回到打开
面板前的窗口，也不能保证获得其 class 来识别终端。缺少 `wtype` 或执行失败时
记录 `clipboard paste` 错误；已成功写入的剪贴板内容仍保留，面板不会重新打开。
这些是源码中的已有分支，外部工具的协议支持及真实焦点行为未在此次验证。

`dictationTyper.paste` 则在写入剪贴板之前要求 Hyprland client 和活动窗口地址；
目标是插入时查询到的窗口，而非开始录音时保存的窗口。它不调用
`focusQuietly`，直接按查询到的地址发送 Ctrl+V（终端加 Shift）。`wl-copy`
失败会返回错误；读取旧文本的 `wl-paste` 错误被忽略，只有读到非空旧文本时才
在发送后等待 400 ms 尝试恢复，恢复失败也被忽略。无 client、IPC 失败或发送
失败都不会调用 `wtype`。提示卡片的定位查询失败时改用角落位置，不代表听写
插入也有可用退化。

### 初次运行、配置和会话

[config/hypr/hyprland.conf](../config/hypr/hyprland.conf) 是 Hyprland 0.53 风格的
样例：preferred/auto 输出、桌面标识、工具包后端、环境传播、`exec-once` 启动
shell、可选 hypridle／fcitx5、绑定、blur／动画及窗口／layer 规则。
[config/README.md](../config/README.md) 描述用户接入方法；版本号是产品要求，
不是 Wayland interface version。

`bootstrap.Install` 首次写样例，已有不同文件默认不覆盖；`-force` 备份后替换。
已有 `hyprland.lua` 时跳过 classic 主配置。`FindShellStart`／`FindSource` 是文本
扫描，会跟随 source／require，但不是执行 Lua 后的有效配置证明。
`checkHyprland` 将缺少 shell 启动视为错误，缺少生成配置的加载只警告。

`scripts/inua-session` 按存在的 classic `hyprland.conf` 做启动检查；没有该文件
就让 Hyprland 自己解析默认配置。它没有对 Lua-only 配置做同等的启动前检查。
它尝试 `inua config apply`（失败被忽略）、配置 portal 搜索目录并 try-restart
portal，然后启动可选 `inua-remote --await-session`，最后 exec 合成器。
启动前虚拟屏的目的，是解决没有首帧时 compositor 尚未执行 exec-once 的情况；
这点是脚本所依赖的行为，未在此次运行验证。

`hyprconf.Apply` 使用已知设备加 `j/devices` 生成规则；有 client 但请求失败时
返回错误，不写新文件；没有 client 时可凭已记录设备和合盖信息离线生成。
内容不变不写；变更原子替换后查询 `misc:disable_autoreload`，只有关闭自动重载
才显式 `reload`。写成功后的查询／reload 仍可能失败，因此“保存文件”不等于
“合成器已应用”。`showSourceWarning` 在设置中提示未加载／被后续配置覆盖。

[hyprconf.go](../internal/hyprconf/hyprconf.go) `ConfigFile`、`RunsLua` 与 UI 的
`probeLua` 是不同调用路径：不要把它们概括成一个统一版本开关。
[session.go](../internal/portalconf/session.go) `ActivationDisplay` 还传播
`WAYLAND_DISPLAY` 和实例环境给 D-Bus／systemd，供后端进程连接正确会话。
远程桌面和终端重新激活还会用 `SessionInstance` 按 lock 文件检查进程；shell
Store 自身不采用这种重新发现方式。

## 协议与版本边界

这里的“绑定”指源码实际提交的 interface version，“最低”指所调用请求需要的
协议下限，“未知”指外部库代为协商、源码未给出数值。名称后缀 `_v1` **不是**
实际绑定版本；库版本和合成器产品版本也不是协议版本。

| 协议／通道 | 分类、源码和实际绑定 | 最低要求／未知点 |
| --- | --- | --- |
| `wl_display`, `wl_registry`, `wl_shm`, SHM pool／buffer | Wayland core；[stream.go](../internal/capture/hyprland/stream.go) `client.init`, `newBuffer`；SHM 显式绑定 1，display／registry 用基础 sync/get_registry/bind 请求 | 此直接捕获路径使用 core v1 操作；不是 GTK 所有渲染协议的完整清单 |
| `wl_seat` | Wayland core；[vinput.go](../internal/vinput/vinput.go) `Input.init` 显式绑定 1 | 这里只将 seat 传给虚拟设备；多个同名 global 会覆盖，实际选中 seat 的策略不是用户可配置的 |
| `wl_output`／xdg-output、xdg-shell、fractional-scale、viewport、text-input | GDK／GTK 提供 monitor、普通窗口、缩放与输入法；[bar.go](../internal/ui/bar.go) `syncBars`、[appid.go](../internal/gtkx/appid.go) `toplevel`，样例不设置 GTK_IM_MODULE | 本仓库不直接 bind，绑定及最低协商版本未知。`ValidScale` 的 1/120 运算不能证明绑定了 fractional-scale v1；connector 名相等是代码假设 |
| `zwlr_layer_shell_v1` | wlr 扩展，不是 Wayland core；[layershell.go](../internal/layershell/layershell.go) `Load`, `Supported` 经动态库协商，绑定版本未知 | 基础 anchors／exclusive zone／exclusive keyboard 属于 v1；动态 `set_layer` 是 v2，`KeyboardOnDemand` 的语义下限为 v4。是否由外部库在旧版本模拟／退化未知，不能用 `Supported()` 推出完整能力。[XML](https://raw.githubusercontent.com/swaywm/wlr-protocols/master/unstable/wlr-layer-shell-unstable-v1.xml) |
| `ext_session_lock_manager_v1` | wayland-protocols 的 staging 扩展；[layershell.go](../internal/layershell/layershell.go) `LockSupported` 经库协商 | 所需 lock 协议为 v1，实际协商未观测。代码注释的 1.1／1.2 分别是库提供 lock／monitor signal 的版本。客户端死亡后锁仍在，恢复政策由合成器决定。[XML](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/ext-session-lock/ext-session-lock-v1.xml) |
| xdg-activation／xdg-foreign | wayland-protocols 扩展，经 GDK；[notify.go](../internal/ui/notify.go) `activationToken`、[appid.go](../internal/gtkx/appid.go) `SetForeignParent` | 实际绑定、xdg-foreign 变体及运行时支持未知；没有直接协议客户端 |
| `zwlr_foreign_toplevel_manager_v1` | wlr 扩展；`client.init` 绑定 `min(advertised, 3)` | 捕获客户端仅用 toplevel／closed／destroy 这部分 v1 语义；并未用它为 shell 提供窗口状态或工作区。[XML](https://raw.githubusercontent.com/swaywm/wlr-protocols/master/unstable/wlr-foreign-toplevel-management-unstable-v1.xml) |
| `hyprland_toplevel_mapping_manager_v1` | Hyprland 扩展；`client.init` 绑定 1，`client.read` 用 `get_window_for_toplevel_wlr`（opcode 1） | 最低 1；收到 address_hi 与低 32 位组成完整 IPC 地址；必须用同一 Wayland 连接拥有的 handle。[XML](https://raw.githubusercontent.com/hyprwm/hyprland-protocols/main/protocols/hyprland-toplevel-mapping-v1.xml) |
| `hyprland_toplevel_export_manager_v1` | Hyprland 扩展；`client.init` 绑定 `min(advertised, 2)` | `client.capture` 无条件用 opcode 2，即 since=2 的 `capture_toplevel_with_wlr_toplevel_handle`，所以真正最低为 2。源码没有拒绝 advertised=1，也没有 v1 请求回退；这是版本检查缺口，未在此修复。[XML](https://raw.githubusercontent.com/hyprwm/hyprland-protocols/main/protocols/hyprland-toplevel-export-v1.xml) |
| `zwlr_virtual_pointer_manager_v1`, `zwp_virtual_keyboard_manager_v1` | 分别为 wlr 和虚拟键盘扩展；`Input.init` 两者都绑定 1 | 实现用 v1 的 create、motion/button/axis/frame、keymap/key/modifiers；权限与暴露取决于合成器。没有仅键盘或仅指针的降级 |
| grim、wf-recorder、wl-copy／wl-paste、wtype、xdph | 外部进程持有实际协议连接；[screenshot.go](../internal/ui/screenshot.go)、[record.go](../internal/ui/record.go)、[clipboard.go](../internal/ui/clipboard.go)、[voice.go](../internal/ui/voice.go)、[desktop.go](../internal/desk/desktop.go)、[portal_share.go](../internal/ui/portal_share.go) | 没有源码内协议 bind/version 证据；这些程序的具体构建、捕获／数据控制／输入协议版本未知，不能把可执行文件存在当作协议可用 |
| Hyprland request/event sockets | 非 Wayland；`Client.Request`, `readEvents` | 无协议版本握手；文本／JSON 字段与 dispatcher 语法就是契约。Lua 通过 `eval` 的 `ok` 探测；README 的 0.53／Overview 的 0.55+ 不能代替逐项能力检查 |

## 替代方向能说明什么

这些是协议文本确认的范围，**不是其他合成器已支持这些功能的结论**：

- [ext-foreign-toplevel-list v1](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/ext-foreign-toplevel-list/ext-foreign-toplevel-list-v1.xml)
  提供窗口 handle、标识和基本列表信息；它刻意把更多状态／动作留给其他扩展。
  不能独自替代本项目所用的全局几何、工作区关系、MRU 和任意控制。
- [wlr foreign-toplevel management](https://raw.githubusercontent.com/swaywm/wlr-protocols/master/unstable/wlr-foreign-toplevel-management-unstable-v1.xml)
  提供标题、app_id、状态、输出及激活／关闭等动作；本项目当前只把其 handle
  用于预览。扩展使用范围不等于完整的 Hyprland IPC 替代。
- [ext-workspace v1](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/ext-workspace/ext-workspace-v1.xml)
  有输出组、工作区状态及能力事件；工作区不必一屏一个集合。创建、激活、删除、
  重新归组受能力限制，不能假定任意数字 ID、即时创建，或自动获得窗口归属。
- [ext-image-capture-source v1](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/ext-image-capture-source/ext-image-capture-source-v1.xml)
  可从输出或 ext foreign toplevel handle 创建捕获源；
  [ext-image-copy-capture v1](https://raw.githubusercontent.com/wayland-mirror/wayland-protocols/main/staging/ext-image-copy-capture/ext-image-copy-capture-v1.xml)
  定义向客户端缓冲捕获图像。是否授权、隐藏窗口是否继续更新、与管理用窗口 ID
  如何关联，都还需要目标合成器证据。
- [wlr-output-management](https://raw.githubusercontent.com/swaywm/wlr-protocols/master/unstable/wlr-output-management-unstable-v1.xml)
  可作为模式与配置方向调查，但协议明确不让客户端创建／销毁 head；不能替代
  `output create headless`。持久化配置、镜像、失败恢复也需要另行核对。

此次没有选定目标合成器，因此它们的实现版本、启用条件、授权模式和完整功能
组合均未知。减少功能（例如只显示标题、使用外部屏幕设置、手动粘贴）是可考虑
的产品取舍，**不是当前已经实现的跨合成器 fallback**。

## 隐含假设与迁移风险

本节的代码行为是源码确认，迁移后可能出现的问题是推断，尚无运行验证。

| 假设／合成器内部能力 | 源码证据与风险 |
| --- | --- |
| 全局几何可读取且可直接缩放 | `Toplevel.Raw["at"/"size"]` 为 JSON 浮点数组；Overview 按 monitor 原点换算，缺几何／完全落在屏外时铺满卡片。`renderOverviewView` 用 `max(scale,1)`，没有截图 `logicalSize` 的 transform 交换；旋转、低于 1 的缩放及跨屏窗口存在需要实测的差异 |
| 窗口层叠顺序可从焦点历史推算 | `ShotWindows` 排全屏、浮动、平铺，再按 `focusHistoryID`；这是客户端推算，不是服务端完整 stacking list。pin／special workspace／遮挡语义可能不同 |
| 工作区是正整数、单一输出归属 | Bar／Overview 排除非正 ID；Overview 按 workspace ID 统计，即使 client.monitor 尚未一致；预留 ID 在非 MRU 模式从 11 起。工作区名字与数字选择器混用，切换到不同工作区模型时不能只换类型名 |
| 动作完成会产生足够事件 | Store 不周期轮询；没有被列为 state event 的几何变化不保证立即刷新。成功 `ok` 不是后续状态一致性的证明。desk 使用独立轮询，不能继承 Store 的重连／urgent 语义 |
| 可观察全局按键并修改绑定 | Overview 的 raw key listener、held-key 查询、shadowing、submap、自定义事件与鼠标绑定保护在 compositor 内运行。普通 GTK 键盘事件只覆盖自己的焦点表面，不能据此替代这些行为 |
| 可强制聚焦、路由窗口且不移动指针 | Lua／classic 聚焦、静默移动和 `cursor:no_warps` 配置修改；API 名称相似不意味着权限、焦点抢占政策和无指针跳转语义相同 |
| Dock／Bar 独占区域就是完整避让 | [bar.go](../internal/ui/bar.go) `hold`／`release` 保留原独占区，避免展开的 panel 把工作区挤满；Dock 只请求区域，没有窗口碰撞算法。若要精确避让，需要布局／场景图策略，不能只依赖列表事件 |
| 背景模糊、动画、层次和窗口样式跟随 namespace/class | `hyprland.conf` 和 `Render`／`RenderLua` 写 blur、popup blur、no_anim、window rule、锁恢复。GTK CSS 无法代替合成器对其他表面的背景采样或放置政策 |
| 会话、捕获、输入与 IPC 指向同一实例 | shell IPC 路径来自 signature，capture/vinput 来自 WAYLAND_DISPLAY，GDK 自己连显示；实例发现还读取 Hyprland lock 文件。环境不一致时，地址与 handle 不能相互验证 |
| 多步动作可以局部失败而 UI 仍可理解 | Overview 跨屏移动、classic focus batch、配置写入后 reload、虚拟屏创建后设模式均非跨请求事务。需要先定义期望的失败行为，再谈替换；本审计不提出接口 |

当前明确需要合成器内部政策或专用控制通道的部分，是全局输入观察、工作区与
窗口路由、虚拟输出创建、精确窗口几何／布局、无指针跳转聚焦、背景模糊与锁
恢复。它们可能由目标合成器的现有扩展提供；此次没有证明必须写插件，也没有
证明标准协议可以全部满足。

## 已有测试与缺失覆盖

下表是现有测试的源码证据，不把测试名等同于真实桌面验证，也不为文档新增测试。

| 方面 | 已有路径与测试符号 | 尚未证明 |
| --- | --- | --- |
| IPC 状态与重连 | [hypr_test.go](../internal/hypr/hypr_test.go) `TestBuildSnapshot`, `TestBuildSnapshotNoActiveWindow`, `TestBuildSnapshotKeepsDisabledMonitorWithWorkspace`, `TestStoreInitialSnapshotAndEvents`, `TestStoreIgnoresNonStateEvents`, `TestStoreUrgentTracking`, `TestStoreReconnectRebuildsState`, `TestStoreManualRefresh` | 初始查询中途变更的精确竞争、解析失败后恢复、断线 urgent 残留、实例 signature 改变、实际 Hyprland 的所有事件组合 |
| Bar／窗口／Dock | [bar_test.go](../internal/shell/bar_test.go) `TestBarWorkspaces`, `TestBarTitle`；[launcher_test.go](../internal/shell/launcher_test.go) `TestCancelReturnsFocus`, `TestExecuteClosedWindow`, `TestWindowMessages`；[dock_test.go](../internal/shell/dock_test.go) `TestDock`, `TestDockSkipsFcitxWindows` | GTK 焦点与异步失败提示、no_warps 恢复、exclusive zone／浮动／全屏避让、connector 延迟与多屏拔插 |
| Overview 模型 | [overview_test.go](../internal/shell/overview_test.go) `TestOverviewMRUFrozenAndSharedWithBar`, `TestOverviewHidesEmptyWorkspacesButBarShowsThem`, `TestOverviewSelectionSurvivesWorkspaceRemoval`, `TestOverviewNewReservationsDoNotCollide`, `TestOverviewPendingMoveOccupiesNewCardOnTargetMonitor`, `TestOverviewCountsWindowByWorkspaceIDEvenWhenClientMonitorDiffers`, `TestOverviewNumberSlotsStayOnFocusedMonitorWithMRUOff` | Lua 脚本实际事件／按键释放与冲突、GTK 多屏 OnDemand、打开期间输出增减、移动失败／投影过期的界面效果、布局与 capture 的完整调用路径 |
| 预览 | [schedule_test.go](../internal/capture/hyprland/schedule_test.go) `TestCaptureSchedule`；[shrink_test.go](../internal/capture/hyprland/shrink_test.go) `TestShrinkCopiesAndFlipsWithoutResizing`, `TestShrinkFitsDisplayBoxAndAverages`, `TestShrinkRejectsBadFrames` | 没有 stream.go 协议往返测试；版本 1 export、global 移除、mapping 失败、断线重建、不可见窗口、SHM 帧生命周期与真实格式 |
| 显示／虚拟屏／实例 | [display_test.go](../internal/display/display_test.go) `TestPanelsOff`, `TestOutputs`, `TestValidScale`；[virtual_test.go](../internal/hypr/virtual_test.go) `TestSyncVirtualScreen`, `TestSyncVirtualScreenRefused`；[instance_test.go](../internal/hypr/instance_test.go) `TestInstanceOfTheSessionStarted`, `TestSessionInstanceSkipsGoneSession` | 真实 DRM／logind 热插拔、无首帧登录、锁屏时屏幕增加、虚拟屏与编码器重新初始化 |
| 配置与会话 | [bootstrap_test.go](../internal/bootstrap/bootstrap_test.go) `TestInstallFresh`, `TestInstallKeepsUserFiles`, `TestSamplesBesideLuaConfig`, `TestFindLua`；[hyprconf_test.go](../internal/hyprconf/hyprconf_test.go) `TestApplyWithoutHyprland`, `TestRenderVerifies`, `TestRenderLua`, `TestApplyLua`, `TestRenderShortcuts`, `TestRenderLockRestore`；[displays_test.go](../internal/hyprconf/displays_test.go) `TestRenderDisplaysVerifies`；[test-session.py](../scripts/test-session.py) `SessionTest` | Python 使用假 compositor／systemctl；verify-config 仅在安装 Hyprland 时运行，不证明真实应用、Lua 有效配置、会话环境和权限；未覆盖录制中崩溃／断线后的 submap 恢复 |
| 截图、portal、输入 | [screenshot_test.go](../internal/shell/screenshot_test.go) `TestShotWindows`, `TestRectPixels`；[shotpick_test.go](../internal/shell/shotpick_test.go) `TestPickGroups`；[screenshot_test.go](../internal/portal/screenshot_test.go) `TestScreenshot`；[portalconf_test.go](../internal/portalconf/portalconf_test.go) `TestEnableWritesDirectory`；[vinput_test.go](../internal/vinput/vinput_test.go) `TestKeysym`, `TestKeymap` | portal 测试是假 D-Bus／回调，不证明 xdph 共享；vinput 没有协议往返测试；无输出／无 client 截图结束路径、各工具协议协商未验证 |
| desk 与应用辅助 | [tools_test.go](../internal/desk/tools_test.go) `TestWindowTools`；[history_test.go](../internal/desk/history_test.go) `TestListWorkspaces`；[watch_test.go](../internal/desk/watch_test.go) `TestWatch`；[bring_test.go](../internal/hypr/bring_test.go) `TestBringHere`, `TestWindowOf`；[hypr_test.go](../internal/hypr/hypr_test.go) `TestRoomRight` | 假数据不证明 classic/Lua 兼容性、实际 PID／class 匹配、应用重开／多窗与未知合成器 |
| 剪贴板与听写插入 | [dictation_paste_test.go](../internal/ui/dictation_paste_test.go) `TestDictationPastes` 以假 Hyprland socket 和注入的剪贴板检查 Lua 地址目标、终端 Shift、旧文本恢复，以及未执行 `wtype` | 未覆盖剪贴板面板的 nil client／`wtype` 分支、工具缺失、焦点恢复；听写的 nil client、classic 分支、失败与真实输入法也未由此测试证明 |

本次检查结果见末节。运行时缺口保持为调查结论，不在文档分支修复。

## 本次检查记录

### 停止前的检查

源码基线与本地已有 `origin/master` 都是 `4ad7126`，不需要合并。
使用 Go 1.26.1；只改本文和架构页链接。未新增自动测试，未运行 UI、嵌套会话或
其他合成器，未安装／启动外部合成器；没有运行时互操作性结论。
没有非固定版本依赖安装。未修改 Go／Blueprint，因此无需 generate／i18n。

两次均执行 `make test`，以 `GOFLAGS=-v` 显示跳过项；make 实际依次执行
`python3 scripts/test-session.py`、`go vet ./...`、
`CGO_ENABLED=1 go test -race ./...`。使用独立临时 HOME、XDG config/data/state/
cache/runtime、Go 缓存与临时目录；清除会话环境，D-Bus 指向隔离的不存在地址，
测试需要的服务由现有 `dbustest` 建私有 bus。所有文件都留在构建目录内。

| 检查 | 精确结果 |
| --- | --- |
| 首轮 `make test` | 退出码 **2**；Python **15 tests / OK**（0.351s）；`go vet ./...` 通过；race 测试 **23 个包失败、124 个顶层测试失败、46 个跳过**。主要失败为临时路径过长，Unix socket `bind: invalid argument`／私有 dbus-daemon `EOF`；另有 `internal/clipboard.TestFindSource` 失败 |
| 缩短临时路径入口后的 `make test` | 退出码 **2**；Python **15 tests / OK**（0.356s）；`go vet ./...` 通过；race 测试 **5 个包失败、54 个顶层测试失败、46 个跳过**。通过运行器持有的目录 fd 提供短入口，实际文件仍在原构建目录，未改测试代码 |
| 第二轮失败包 | `internal/apps/disk`（`TestDecide` 将短入口视为系统目录）；`internal/clipboard`（`TestFindSource` 预期 firefox，却识别到其他进程组）；`internal/upgrade`（`TestStillOld` 未匹配替换后的程序）；`internal/vm`、`internal/vmui`（路径校验拒绝短入口的符号链接组件） |
| 与此次审计直接相关的第二轮结果 | `internal/hypr`, `internal/capture/hyprland`, `internal/shell`, `internal/ui`, `internal/layershell`（无测试文件）, `internal/display`, `internal/hyprconf`, `internal/bootstrap`, `internal/portal`, `internal/portalconf`, `internal/desk`, `internal/vinput` 均无失败 |

首轮通过了第二轮受短入口影响的 disk、upgrade、vm、vmui 包。源码检查也能解释
这些差异：disk 按系统目录前缀分类，upgrade 比较 `/proc` 中程序的真实路径，VM
明确拒绝符号链接路径。`TestFindSource` 使用实际进程列表，隔离 HOME／D-Bus
不能隔离进程枚举；两轮均失败，未终止任何会话进程来规避。上述结果不能记为
整套测试通过，也没有在文档分支修改测试或运行代码来消除失败。

两轮相同的 46 个跳过项如下；helper 的顶层跳过是其测试设计，不是依赖缺失：

| 路径 | 跳过的测试符号 | 原因 |
| --- | --- | --- |
| [viewer_test.go](../internal/apps/viewer/viewer_test.go) | `TestTurbo` | 缺 libturbojpeg |
| [host_test.go](../internal/term/host/host_test.go) | `TestTitleAndFolder`, `TestGitEvents`, `TestAnswers`, `TestClipboardEvent`, `TestRemember`, `TestAgents`, `TestNotify`, `TestSnapshotAttach` | 缺 libinua-vt.so |
| [shellint_test.go](../internal/term/shellint/shellint_test.go) | `TestBash`, `TestZsh` | 缺 libinua-vt.so |
| [vt_test.go](../internal/term/vt/vt_test.go) | `TestText`, `TestWideAndWrap`, `TestTitleAndPwd`, `TestResize`, `TestFrame`, `TestFrameDirty`, `TestFrameScrolled`, `TestFrameGrows`, `TestFramePassword`, `TestKeys`, `TestKeyModes`, `TestSelect`, `TestSelectClicks`, `TestSelectWrapped`, `TestKeysym`, `TestPaste`, `TestAnswers`, `TestClipboardWrite`, `TestScroll`, `TestMouse`, `TestSearch`, `TestPrompts`, `TestInputAfterRedraw`, `TestTail`, `TestNotifications`, `TestDefaults`, `TestLinks`, `TestPalette`, `TestSnapshot` | 缺 libinua-vt.so |
| [host_test.go](../internal/term/host/host_test.go)；[pty_test.go](../internal/term/pty/pty_test.go) | `TestHelperReport`, `TestHelperDetach`, `TestInterruptHelper` | 供相应父测试启动的 helper，不单独运行 |
| [shellint_test.go](../internal/term/shellint/shellint_test.go) | `TestFish` | 缺 fish |
| [isodownload_test.go](../internal/vm/isodownload_test.go) | `TestISODownloadRealImage` | 未设置真实 ISO 下载测试地址 |
| [workshop_test.go](../internal/workshop/workshop_test.go) | `TestJSModule` | 测试 PATH 中没有 node |

文档内部链接、所引路径／符号、直接协议版本与 `git diff --check` 已检查。
未检查真实窗口聚焦、多屏几何、输出热插拔、锁恢复、预览权限与重连、快捷键
冲突、Dock 避让及任何替代合成器能力。它们仍是后续验证任务，不属于本次实现。

### 恢复构建后的复核

保留 `8a03af4` 的审计文档与架构链接；恢复后补充 Overview 的输出生命周期、
快捷键录制故障后的恢复边界，并明确 `follow=false` 属于窗口移动请求。
对照任务所有者的验收项核对了功能矩阵、源码路径／符号、协议版本、替代方向、
合成器内部需求和测试缺口；没有扩大到 #60–64。

使用 Go 1.26.1，在工作树外、父构建目录内重新建立临时 HOME、XDG config/
data/state/cache/runtime、Go 缓存和 TMPDIR。`env -i` 仅传入测试用 PATH、这些
目录、`GOTOOLCHAIN=local`、`GOFLAGS=-v` 和指向隔离不存在地址的两个 D-Bus
环境变量，再执行 `make test`。未继承真实 Wayland、Hyprland 或会话环境。

| 检查 | 结果 |
| --- | --- |
| 恢复后的 `make test` | 退出码 **2**；Python **15 tests / OK**（0.343s）；`go vet ./...` 通过；race 测试 **13 个包失败、61 个顶层测试失败、46 个跳过**。失败为 socket 路径过长、私有 dbus-daemon `EOF` 和 `TestFindSource` 的进程枚举断言 |
| 仅对上述 13 个失败包用短 TMPDIR 入口复核 | `CGO_ENABLED=1 go test -race -count=1 -v`，退出码 **1**；**12 个包通过，1 个包／1 个顶层测试失败，9 个跳过**。运行器通过持有的目录 fd 提供短入口，实际文件仍在父构建目录内；未更改测试源码 |
| 唯一仍失败的测试 | `internal/clipboard.TestFindSource`：预期 `firefox`，实际识别为 `start-hyprland` 所在进程组；隔离环境变量不会隔离进程列表，未操作该会话的进程 |

复核的包为 `internal/` 下的 `apps/connect`、`apps/files/chooser`、
`apps/files/disks`、`apps/files/fm1`、`apps/files/remote`、`clipboard`、
`dbusexport`、`hypr`、`keyring`、`portal`、`sni`、`system`、`term/host`。
短入口消除了这些包的 socket／D-Bus 失败；这不是整套 `make test` 通过。
全量运行的 46 个跳过项与停止前记录相同；定向复核的 9 项来自 `term/host`。

没有非固定版本安装或依赖清单变更；没有修改运行代码、Blueprint 或测试，
无需 generate／i18n。原恢复缓存未改动，新的测试日志和缓存均在工作树外。
未做 UI、真实合成器、输出热插拔或其他桌面的兼容性检查。

### 第二轮评审修订与全量复核

先以 merge 合并已有 `origin/master`（`89e121e`），无冲突；该提交修复
`internal/clipboard.TestFindSource` 对宿主进程的依赖。合并提交前执行
`make i18n`，翻译文件没有变化。随后仅修改本文：分别说明剪贴板的复制、按
地址粘贴、nil client 时的 `wtype` 当前焦点回退，以及听写插入必须有 Hyprland
client 的行为。未自行修改运行代码或测试，未改 Blueprint。

使用 Go 1.26.1。先 `unset TMPDIR`；`ft58_run` 指向父构建目录内新建的真实
临时目录，内有独立的 home、config、data、state、cache、runtime 子目录，
runtime 权限为 0700。`ft58_modules` 和 `ft58_cache` 分别指向父构建目录内
复用的 GOPATH 和 Go 构建缓存。完整命令如下：

```sh
env -i PATH=/usr/local/go/bin:/usr/bin:/bin \
  HOME="$ft58_run/home" \
  XDG_CONFIG_HOME="$ft58_run/config" XDG_DATA_HOME="$ft58_run/data" \
  XDG_STATE_HOME="$ft58_run/state" XDG_CACHE_HOME="$ft58_run/cache" \
  XDG_RUNTIME_DIR="$ft58_run/runtime" \
  GOPATH="$ft58_modules" GOCACHE="$ft58_cache" \
  GOTOOLCHAIN=local GOFLAGS=-v \
  DBUS_SESSION_BUS_ADDRESS="unix:path=$ft58_run/no-session-bus" \
  DBUS_SYSTEM_BUS_ADDRESS="unix:path=$ft58_run/no-system-bus" \
  make test > "$ft58_run/make-test.log" 2>&1
```

`env -i` 不继承真实 Wayland、Hyprland 或其他会话环境；两个 D-Bus 地址均
不存在，测试使用自己的私有 bus。`TMPDIR` 未传入，测试临时文件使用系统默认
临时目录；没有短路径别名或目录 fd 入口，未操作真实窗口、服务或凭据。

`make test` 退出码为 **0**：Python **15 tests / OK**（0.327s），
`go vet ./...` 通过，`CGO_ENABLED=1 go test -race ./...` **103 个包通过**
（其中 18 个使用 Go 测试缓存），**0 个失败、46 个顶层测试跳过**。跳过项与
前述记录相同；`TestFindSource` 通过，未出现 socket 路径错误或私有
dbus-daemon `EOF`。138 个文档本地链接均存在，`git diff --check` 通过。

没有非固定版本安装或依赖清单变更。未验证真实桌面上的 `wtype` 焦点、听写
输入法交互、UI 或其他合成器；测试通过不改变本文对运行验证的限制。
