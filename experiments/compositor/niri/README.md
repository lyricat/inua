# niri experiment

**目标：niri 26.04 / niri-ipc 26.4.0。真实会话：UNTESTED。**
先按[公共说明](../README.md#手动嵌套运行的隔离准备untested)启动单独的嵌套
实例，使用这里的 `config.kdl`。不用 `niri-session` 或 `--session`，不导入
主会话服务。程序仅用 Python 3 标准库；不依赖 `niri msg` 或任何 Go 包。

## 操作

在第二个终端，将 `FT68_RUN` 设为刚才创建的实验目录。下面的 socket 文件
是嵌套 niri 的子进程写出的，不是从当前终端的 `NIRI_SOCKET` 自动取得。
核对嵌套进程日志、包版本和配置副本；把实际版本填入 `FT68_VERSION`。

```sh
export FT68_VERSION='niri 26.04; niri-ipc target 26.4.0'
FT68_SOCKET="$(cat "$FT68_RUN/niri.socket")"
probe=(python3 experiments/compositor/niri/probe.py
  --nested-socket "$FT68_SOCKET" --version "$FT68_VERSION"
  --config "$FT68_RUN/config.kdl")
"${probe[@]}" snapshot > "$FT68_RUN/niri-initial.jsonl"
"${probe[@]}" --seconds 60 watch > "$FT68_RUN/niri-events.jsonl"
```

这些是 Bash 数组，公共参数放在子命令之前。窗口模式默认 Mod 为 Alt。
watch 运行时，在嵌套窗口内用
Mod+Return 打开两个无私人内容的 foot 窗口；改标题、切换焦点、关闭一个，再
用 Mod+J/K 切换工作区。另一个终端可执行一次 snapshot，取真实 ID 后调用：

```sh
# 先设置 WINDOW_ID / WORKSPACE_ID 为刚查询到的原生 id，不能用 idx 或行号。
"${probe[@]}" focus "$WINDOW_ID" > "$FT68_RUN/niri-focus.jsonl"
"${probe[@]}" switch "$WORKSPACE_ID" > "$FT68_RUN/niri-switch.jsonl"
```

程序拒绝未知 ID，不改为当前窗口／工作区。对确已关闭窗口再发 focus，应报未知
目标；这只验证本地预检，不能冒充服务端拒绝实测。预检后目标才消失时，错误回复
或无法观察到目标焦点也会失败。动作完成后不自动恢复旧焦点。

## Wire 与证据对应

| 操作／观察 | 精确 JSON 或事件 | 来源／本次证据 |
| --- | --- | --- |
| 版本 | `"Version"` → `{"Ok":{"Version":"…"}}` | [Request/Response 定义][types]；假服务端 |
| 枚举窗口 | `"Windows"` → `{"Ok":{"Windows":[…]}}` | [Window][types]；允许空列表、可空 workspace_id；保留原始 layout |
| 枚举工作区 | `"Workspaces"` → `{"Ok":{"Workspaces":[…]}}` | [Workspace][workspace]；保留 id、idx、output、active/focused |
| 订阅 | `"EventStream"` → `{"Ok":"Handled"}`，后续逐行事件 | [socket 客户端][socket]；两个独立连接，不向事件连接发动作 |
| 初始／整体替换 | `WorkspacesChanged{workspaces}`、`WindowsChanged{windows}` | [Event][types]；完整数组，空数组有效；测试保留接收顺序 |
| 打开／改标题／关闭 | `WindowOpenedOrChanged{window}`、`WindowClosed{id}` | [Event][types]；原样记录；每秒重读用于比较 |
| 焦点／归属 | `WindowFocusChanged{id}`（可空）、`WorkspaceActivated{id,focused}`、`WorkspaceActiveWindowChanged{workspace_id,active_window_id}` | [Event/state][state]；跨实体可短暂不一致 |
| 布局／MRU | `WindowLayoutsChanged{changes}`、`WindowFocusTimestampChanged{id,focus_timestamp}` | [Event][types]；保留数据，不用时间戳伪造完整焦点历史 |
| 聚焦窗口 | `{"Action":{"FocusWindow":{"id":ID}}}` | [Action][types]；假服务端核对完整 u64，含大于 2^53 的 ID |
| 切换工作区 | `{"Action":{"FocusWorkspace":{"reference":{"Id":ID}}}}` | [WorkspaceReferenceArg][types]；不用 `Index` 或名字代替身份 |
| 动作回复 | `{"Ok":"Handled"}` / `{"Err":"…"}` | [Reply][types]；随后检查目标的 is_focused；拒绝、丢回复和未达成均不重试 |

每帧 UTF-8 JSON 加换行；分片／粘连由缓冲读取处理。未知事件变体仅记录，
不据此更新模型。快照不是从事件复制出来的，因此不能误称完整 event reducer。
初始完整事件的顺序只按实收日志判断，不能从假服务端的顺序推出实际顺序。

## 必须补做的观察

以下每项目前均 **UNTESTED**，记录时应同时附配置散列、原始日志和人工结论：

1. 无窗口启动：Windows 为空；收到初始工作区／窗口事件。中途开窗时比较事件
   与查询的相对顺序，记录跨实体引用暂时不存在的情况，不擅自修正 ID。
2. 放入多列使部分窗口移出可见区域，查看 `pos_in_scrolling_layout`、
   `tile_pos_in_workspace_view` 与实际画面。当前工作区成员不一定在屏幕上，
   不把这些位置当成 Hyprland 全局坐标或截图矩形。
3. 使用末尾空工作区、清空中间工作区、重排／移屏（有多个嵌套输出时）：id 应
   与 idx 分开记录。动态工作区消失不能变成一个由客户端预留的数字槽位。
4. 观察两个屏的 active workspace 与唯一 focused workspace；layer 表面取得
   焦点时可能没有 focused window。此原型不创建 layer 表面，需另行安排该测试。
5. 显式聚焦和切换后，分别检查 acknowledgement、后续状态与画面；记录滚动、
   指针 warp、焦点被其他客户端改变或请求未达成。没有 GUI 检查就不能记“视觉通过”。
6. 只结束自己启动的嵌套实例，保留探针的 invalidated / failed；检查无旧状态动作。
   niri 重启可能换 IPC 路径，此时旧路径重试应失败，明确选择新路径再运行探针。
   同路径传输断开／恢复已由假 socket 覆盖，真实同路径重连仍须单独验证；
   不把新进程的一次成功 snapshot 记成原连接自动恢复。

[工作区文档][semantics]说明每屏动态工作区和末尾空工作区；配置官网是滚动文档。
FT67 评审标出的 press/release 分组绑定属 next release，不用于本样例。
本实验没有工作区创建、关闭动作、跨屏移动、截图、锁或输入注入命令。

[types]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/lib.rs.html
[socket]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/socket.rs.html
[state]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/state.rs.html
[workspace]: https://docs.rs/niri-ipc/26.4.0/niri_ipc/struct.Workspace.html
[semantics]: https://niri-wm.github.io/niri/Workspaces.html
