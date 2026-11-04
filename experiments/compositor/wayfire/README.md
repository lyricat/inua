# Wayfire experiment

**目标：Wayfire 0.10.0，源码包 0.10.0-1。真实会话：UNTESTED。**
先按[公共说明](../README.md#手动嵌套运行的隔离准备untested)启动独立实例，
使用本目录 `wayfire.ini`。这不是 PR67 的 0.9 配置或 shell 接入。

## 前提与操作

`ipc` 提供 Unix socket；`ipc-rules` 注册名字以 `window-rules/` 开头的方法，
**不是启用 `window-rules` 插件就够了**。切换工作区另需 `vswitch`。
样例中的 command/autostart/place/move/resize/decoration 用于手动窗口实验，
不属于探针协议。默认 3×2 网格，禁用 XWayland，不启动 wf-shell 或 inua。

第二个终端设置 `FT68_RUN`，与启动终端指向同一个目录。记录实际发行包、
插件包和 `wayfire --version` 输出，然后填写 `FT68_VERSION`。

```sh
export FT68_VERSION='Wayfire 0.10.0; source target 0.10.0-1'
FT68_SOCKET="$(cat "$FT68_RUN/wayfire.socket")"
probe=(python3 experiments/compositor/wayfire/probe.py
  --nested-socket "$FT68_SOCKET" --version "$FT68_VERSION"
  --config "$FT68_RUN/wayfire.ini")
"${probe[@]}" snapshot > "$FT68_RUN/wayfire-initial.jsonl"
"${probe[@]}" --seconds 60 watch > "$FT68_RUN/wayfire-events.jsonl"
```

watch 运行时只在嵌套窗口里用 Mod+Return 启动两个 foot。改标题、聚焦、关闭
测试窗口，保存事件。另一个终端取新 snapshot，选原生 view ID 和 output ID：

```sh
# 先设置 VIEW_ID / OUTPUT_ID 为这次查询实际返回的值。
"${probe[@]}" focus "$VIEW_ID" > "$FT68_RUN/wayfire-focus.jsonl"
"${probe[@]}" switch "$OUTPUT_ID" 1 1 > "$FT68_RUN/wayfire-switch.jsonl"
```

`switch OUTPUT X Y` 使用零基坐标；3×2 的合法范围是 x=0..2、y=0..1。
不会将“工作区 4”换成全局整数 ID。输出／workspace-set 和窗口可能在查询之间
改变；目标未知或越界即失败。动作确认之后还会查 focused-view 或输出 viewport；
默认观察时限为 3 秒，每轮查询结束后检查；仍未达成则标为未确认。
可在公共参数设置 `--timeout`，不自动重发。

## Wire、方法和事件

所有请求为 `{"method":"…","data":{…}}`；先发 **4 字节本机字节序 uint32
长度**，再发 UTF-8 JSON。源码直接读写 native uint32，不能声称跨架构协议
固定为 little-endian；AF_UNIX 两端在同一主机。错误对象含 `error`；动作和 watch
确认是 `{"result":"ok"}`。数组回复不包 result。依据 [ipc.cpp][transport]、
[method repository][repository]，假 socket 检查了分片、粘连、空帧、长度上限、
截断、JSON 错误和断线。

| 方法 | data / 回复 | 插件／源码 |
| --- | --- | --- |
| `list-methods` | `{}` → `{"methods":[…]}` | [ipc repository][repository]；每代重新查询，缺方法不当空状态 |
| `wayfire/configuration` | `{}` → api-version、build-commit、build-branch 等 | [ipc-rules utility][utility]；不是完整产品版本握手 |
| `wayfire/get-config-option` | `{"option":"core/plugins"}` → result/value/default | [utility][utility]；只读当前配置，不加载插件 |
| `window-rules/list-views` | `{}` → 数组，含 id、role、mapped、wset-index、geometry 等 | [ipc-rules][rules]、[序列化][common]；列表也包含非普通窗口 |
| `window-rules/list-outputs` | `{}` → 数组，含 id、wset-index 和 workspace{x,y,grid_width,grid_height} | [rules][rules]、[common][common] |
| `window-rules/list-wsets` | `{}` → 数组，含 index、output-id、二维网格 | [rules][rules]、[common][common]；网格描述即格点集合，不制造全局工作区编号 |
| `window-rules/get-focused-view` | `{}` → `{"result":"ok","info":VIEW_OR_NULL}` | [rules][rules]；无焦点不是失败 |
| `window-rules/events/watch` | `{"events":[…]}` → ok，然后事件帧 | [events][events]；没有初始全量或完成屏障；选择列表见 `probe.py` 的 EVENTS |
| `window-rules/focus-view` | `{"id":VIEW_ID}` → ok/error | [rules][rules]；服务端检查 toplevel，再调用 default_wm focus_request；mapped 是本探针额外预检 |
| `vswitch/set-workspace` | `{"output-id":OUTPUT_ID,"x":X,"y":Y}` → ok/error | [vswitch][vswitch]；不发可选 view-id，不移动任何窗口 |

view ID 虽通过 JSON uint64 读取，查找函数接收 uint32；output 查找接收 int32，
见[helpers][helpers]。探针拒绝超出这些范围的动作参数，不允许截断为另一个 ID。
`wset-index:-1`、未附着集合的 `output-id:-1` 保持原样，不能解析为无符号数或
替换成当前集合。workspace-set 的 `index` 不等于某些事件里的内部 `get_id()`；
分析时保留嵌套 `wset-data`，不要按数字相等跨字段联接。[序列化][common]、[事件][events]

| 事件组 | 原始字段／意义 | 初始同步限制 |
| --- | --- | --- |
| view-mapped / unmapped / title-changed / app-id-changed / focused | view，focused 可 null | 没有开始时的全量；初始 list-views 必需 |
| view-set-output / wset-changed | view、output 或 old-wset/new-wset | wset 是网格集合，不是格点 |
| view-geometry-changed | old-geometry、view | pending geometry 和变换 bbox 不互换 |
| view-workspace-changed | view、from{x,y}、to{x,y} | 仅该次移动；不能追补窗口出现之前的归属 |
| output-gain-focus / output-wset-changed | output 或 output-data/new-wset-data | 不等于输出集合完整快照 |
| wset-workspace-changed | previous-workspace、new-workspace、output-data、wset-data | 当前 viewport 改变，不是所有窗口改换归属 |
| view-minimized / fullscreen / sticky | view | sticky 使“窗口只在一个格点”更不可假定 |

表中事件均来自 [ipc-events.hpp][events]，本次只验证请求格式、包络和假服务端
接收顺序。`output-added` / `output-removed` 虽有发送代码，却未加入该版本的
signal_map；显式订阅会报 Event not found，省略 events 得到的集合也不含它们。
因此本探针不订阅这两个名字，每秒重读输出。输出变化可能直到下一轮才可见，
短暂变化可能丢失；不是可靠热插拔实现。

`vswitch/set-workspace` 在能力获取失败时仍可能返回 ok；`add_direction` 的结果
也没有传回调用方。[源码][vswitch]因此要求复查实际 viewport，不能把 ok 当完成。
插件卸载后的旧方法清单同样不能保证动作可用，错误回复直接失败。

## Foreign-toplevel 边界与待测项目

本实验**只用 IPC view ID**。wlr foreign-toplevel 的 Wayland object ID 属于
一条 Wayland 连接，不是 IPC view ID；标题、app-id、PID 也不是可靠映射键。
没有映射证据就不能将 handle 送进 focus-view。0.11 的 ext-toplevel／捕获
发布说明不证明 0.10 的管理或捕获身份链。本程序不绑定任何 Wayland 协议；
这一小范围只需 IPC 插件，不需 foreign-toplevel 插件。

以下真实行为全部 **UNTESTED**：

1. 空列表、非 toplevel／未 mapped view 的实际出现方式；窗口初始格点归属。
   `list-views` 没有该格点字段，本实验不从 geometry 或当前 viewport 猜测，
   也不丢弃尚未收到 workspace-change 的窗口。
2. 初始查询期间开关窗口／切换 viewport 的事件顺序；丢失、重复与动画结束时间。
3. 聚焦、指针移动、插件拒绝焦点、切换是否最终生效；样例缺 vswitch 时用独立
   新嵌套实例启动，观察 availability=false 和 switch 失败，不热改主会话插件。
4. 两屏不同 wset、未附着集合、grid 尺寸变化、输出增删；不承诺周期重读能捕获
   中间状态。样例仅提供单个嵌套输出和 3×2 网格。
5. 只退出自有嵌套实例，观察 invalidated；重新启动后显式选择新 socket，不能
   继续用旧代 ID。公共启动片段的目录 fd 属于启动进程，重启后短路径也会变。
   同路径传输恢复需另做真实测试；已通过的同路径重连只是假服务端证据。
6. XWayland、层表面、portal、捕获、锁屏和权限；没有一个能由 IPC 测试推导通过。

[transport]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/ipc.cpp
[repository]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/wayfire/plugins/ipc/ipc-method-repository.hpp
[helpers]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/wayfire/plugins/ipc/ipc-helpers.hpp
[rules]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules.cpp
[common]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules-common.hpp
[events]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-events.hpp
[utility]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-utility-methods.hpp
[vswitch]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/vswitch/vswitch.cpp
