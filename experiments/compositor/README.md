# niri / Wayfire disposable IPC experiments (FT68)

这两个 Python 标准库程序只验证一个小范围：窗口／工作区枚举、事件、按原生
标识聚焦、切换已有工作区。它们不参与 `make build`、安装或 shell 运行，
**不是 compositor 支持实现**。不导入 inua 运行代码，也没有统一窗口模型。

2026-11-05：本机只完成固定来源核对和假 Unix socket 测试。
**全部真实 niri / Wayfire 观察均为 UNTESTED**（交付时；其后已在 niri 26.04 和
Wayfire 0.11 的真实会话中运行，结果见 EVIDENCE.md 的“真实会话运行”一节）。分别参见
[niri 操作说明](niri/README.md)、[Wayfire 操作说明](wayfire/README.md)、
[证据与测试记录](EVIDENCE.md)。

## 输入和版本边界

仓库基线为 `d6a2e5a2`。FT68 owner 线程授权了两个 disposable prototype，
覆盖旧 issue #61 的等待条件；不因此开放 #62–64。
FT67 的矩阵已 ready、未 shipped，本文仍把它当 **pending proposal**，不导入该分支。
PR67 的提供版 diff 仅作问题参考；
没有合并、依赖、修改它，也不把其中 Debian 0.9 的运行记录转作本次证据。

| 目标 | 固定取样 | 本次验证层次 |
| --- | --- | --- |
| niri | 产品 26.04；`niri-ipc` **26.4.0** 的 `lib.rs`、`socket.rs`、`state.rs` | 发布的 IPC 类型／客户端源码＋假服务端；真实产品未运行 |
| Wayfire | 产品 **0.10.0**；Debian 上游源码镜像 **0.10.0-1** | IPC 服务端、ipc-rules、vswitch 源码＋假服务端；真实产品未运行 |

这些是目标版本，不是经过验证的最低版本。0.9 的 PR 记录和 0.11 的发布说明
不能替代 0.10 的协议检查。每次探针要求 `--version`、`--config` 和
`--nested-socket`；niri 还查询服务端 Version，Wayfire 查询 build/API 信息与
`core/plugins`。`--version` 是操作者记录，不是认证；程序保留实际返回值，
不把任意版本字符串视为兼容证明。不同版本须重新核对来源和实际行为。

## 探针行为

- `snapshot` 只读一次；`watch` 默认观察 30 秒，每秒重新读取一次原始快照。
  事件逐条记录，不拿有竞态的增量去修改另一个状态副本。
- `focus` / `switch` 是独立的显式动作。先查询目标是否存在，再发送一次，
  随后重读至观察到目标状态或超时。查询、动作和复核共用一个命令连接；
  连接断开就失败，不在动作中重连。目标消失可能发生在预检之后，仍须处理回复。
- `watch` 先订阅、再取快照。命令与事件是两个连接，没有跨连接全序。
  初始事件可能在快照读取期间排队，日志中的先后是**客户端接收顺序**，不是
  compositor 处理顺序或动作因果关系。`snapshot-begin` / `snapshot` 标出查询窗口。
- niri 流自带初始全量状态，日志保留 `WorkspacesChanged`、`WindowsChanged`；
  独立查询只是对照。Wayfire watch 只有确认回复，没有初始状态或同步屏障。
  两边的多项查询都标 `atomic:false`，不能称一致快照。
- 每代连接均重新核对版本／方法并取快照。断线输出 `invalidated`、`state:null`；
  历史日志只作历史，不能继续操作。最多重试 `--reconnects` 次（默认 2），
  只连显式指定的 socket。不会找另一个实例，不重放任何动作。
  `epoch-begin` 标记新一代开始；`generation` 只在本次进程内有意义，不是服务端实例 ID。
- 请求／完整帧默认限时 3 秒。安静的事件流不会因 3 秒无事件而断线；收到半帧后
  才受帧超时限制。每条消息上限 1 MiB，Wayfire 发出的请求额外扣除 4 字节头。
  watch 总时长和动作观察时限在一轮读取后检查，可能多出一轮查询／帧读取的时间。
- 缺 socket、权限拒绝、缺方法、非法 ID、截断／畸形回复均以非零退出码和日志
  表达；有效 `[]` 是空桌面，`null` 或错误回复不能替代数组。未知 niri 事件
  变体原样记录，不据此合成状态。没有通用原始命令入口。

失败日志另有 `category`、`error_type` 和原始 `reason`：`unavailable` 表示缺 socket／
方法，`denied` 是系统明确拒绝访问，`malformed` 是帧、包络或所用字段无效，
`disconnected` 和 `timeout` 分别表示断开与超时。服务端错误是 `rejected` 并保留
错误文本；协议没有统一错误码，不能凭字符串猜出授权政策。权限拒绝不重连。
动作已经确认但未观察到目标状态时为 `unconfirmed`。
非法或未知动作目标为 `invalid-target`，不会发送动作。

输出为 JSON Lines，含单调时钟相对时间、序号、请求／回复和原始标识。
`action-acknowledged` 不是完成；`action-observed` 只表示随后查询看到了条件，
不证明是该动作导致。已经满足条件的目标也是如此。错误或中断后的动作结果
可能不确定，**不要自动重试**。探针不恢复焦点、不修改 warp 或 compositor 配置。

配置只读取显式文件并记录 SHA-256／字节数；散列不能证明该文件已加载，也不
覆盖 include 文件。使用本目录无 include 的样例；另存包版本、样例副本、启动日志
和渲染后端说明。日志可能含测试标题、PID、路径，公开前人工去除本机信息；
不要将真实桌面或私人窗口日志提交到仓库。

## 手动嵌套运行的隔离准备（UNTESTED）

以下命令供有目标 compositor 和 `foot` 的机器手动使用，本次没有执行。
不安装依赖、不用 session wrapper、不导入 systemd 环境、不启动 inua。
样例只启动自己拥有的嵌套 compositor；请在现有 Wayland 桌面的终端操作。
先从仓库根目录选 **一个**目标并准备目录：

```sh
export FT68_TARGET=niri                  # 另一次实验改为 wayfire
export FT68_RUN="$(mktemp -d "$PWD/../ft68-run.XXXXXX")"
cp "experiments/compositor/niri/config.kdl" "$FT68_RUN/config.kdl"
cp "experiments/compositor/wayfire/wayfire.ini" "$FT68_RUN/wayfire.ini"
```

下面的 Python 进程保留目录 fd，子进程使用指向它的短路径，避免 Unix socket
路径超过长度限制。实际文件仍在刚创建的目录；**不是 `/proc/self/fd`**。
该进程退出后别名失效。保留此终端，从另一终端运行目标说明中的探针。

```sh
python3 - <<'PY'
import os
from pathlib import Path
import subprocess

run = Path(os.environ['FT68_RUN']).resolve()
target = os.environ['FT68_TARGET']
assert target in ('niri', 'wayfire')
parent_display = Path(os.environ['WAYLAND_DISPLAY'])
if not parent_display.is_absolute():
    parent_display = Path(os.environ['XDG_RUNTIME_DIR']) / parent_display
for name in ('home', 'config', 'data', 'state', 'cache', 'runtime', 'tmp'):
    (run / name).mkdir(mode=0o700, exist_ok=True)
fd = os.open(run / 'runtime', os.O_RDONLY | os.O_DIRECTORY)
runtime = f'/proc/{os.getpid()}/fd/{fd}'
env = dict(PATH=os.environ['PATH'], HOME=str(run / 'home'),
           XDG_RUNTIME_DIR=runtime, XDG_CONFIG_HOME=str(run / 'config'),
           XDG_DATA_HOME=str(run / 'data'), XDG_STATE_HOME=str(run / 'state'),
           XDG_CACHE_HOME=str(run / 'cache'), TMPDIR=str(run / 'tmp'),
           WAYLAND_DISPLAY=str(parent_display), FT68_RUN=str(run),
           DBUS_SESSION_BUS_ADDRESS=f'unix:path={run}/no-session-bus',
           DBUS_SYSTEM_BUS_ADDRESS=f'unix:path={run}/no-system-bus')
if target == 'niri':
    command = ['niri', '--config', str(run / 'config.kdl')]
else:
    env.update(WLR_BACKENDS='wayland', _WAYFIRE_SOCKET=runtime + '/wayfire.sock')
    (run / 'wayfire.socket').write_text(env['_WAYFIRE_SOCKET'] + '\n')
    command = ['wayfire', '-c', str(run / 'wayfire.ini')]
with (run / 'compositor.log').open('w') as log:
    try:
        subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT, check=True)
    finally:
        os.close(fd)
PY
```

niri 的窗口模式与 session 模式区别见[官方启动文档][start]。
Wayfire 样例通过 wlroots Wayland backend 嵌套；构建是否含该 backend、GPU／
软件渲染是否可用仍须在运行机器确认。启动失败就保留错误，不改成 DRM 会话。
版本通过那台机器的包管理器和 `niri --version` / `wayfire --version` 记录；
这里不假定二进制存在。配置中的 Mod+Return 打开 `foot`；只创建无私人内容的
测试窗口。不得将主会话 socket 填入探针。`--nested-socket` 只是显式选择，
探针无法认证它真的是嵌套实例。

## 与 Hyprland 的差异及对后续设计的限制

| 方面 | 当前 Hyprland（FT58） | niri | Wayfire 0.10 |
| --- | --- | --- | --- |
| 窗口身份 | 实例内 IPC 地址；捕获用专用映射 | 原生 u64，Python 不转 float；窗口存活期稳定 | IPC view ID；不等于 Wayland object ID、foreign-toplevel handle 或捕获标识 |
| 工作区身份 | 普通正整数，现有 UI 按单输出分组 | 稳定 id 与每屏 idx 分离，动态末尾空工作区，工作区可移屏 | workspace-set index、二维格点和当前附着输出分离；网格宽高来自源数据 |
| 几何 | 现有 Overview 假定全局矩形 | 滚动列／tile；可空、带小数的 workspace-view 坐标 | pending geometry、base geometry、变换后 bbox 分别保留；不能当成同一个矩形 |
| 初始同步 | 先连事件、再查询 | 流发全量状态，但不同实体更新不原子 | watch 无快照／序列号；多查询和事件之间没有事务 |
| 焦点 | classic/Lua 地址动作 | focused window 可空，active workspace 不等于全局 focused | view-focused 的 view 可空；focus-request 接受不保证最终焦点 |
| 丢失／恢复 | 当前实现仍留旧快照 | 本实验让旧代失效，重新查询 | 同左；方法、插件和 wset 必须重查 |

这些事实要求后续设计把身份与显示顺序、请求确认与状态观察、空值与断线分开。
不能用“统一整数工作区”提前冻结接口。捕获身份链、关闭动作、layer-shell、锁、
portal、输入和普通快捷键接入都不在本实验实现中。真实多屏、输出热插拔、窗口
布局、XWayland、焦点抢占／指针移动、权限策略也全部 UNTESTED。

后续接口至少应满足以下约束；这里不定义或实现该接口：

1. 身份带目标通道和连接代际。niri ID、Wayfire view ID 与捕获 handle 不互换；
   重连后重新取得身份，旧动作不能排队重放。名称、标题、PID 不作关联键。
2. 工作区身份与展示位置分开。niri 使用 id，idx 仅作每屏顺序；Wayfire 保留
   wset index 和 x/y，输出是当前附着关系。仅有 output-id+x/y 的动作不能保证
   查询后 wset 被换走时仍指向原目标，调用结果必须允许不确定。
3. 数据有效性、能力和空值独立表示。未同步、失联、缺方法、拒绝、畸形数据
   都不能返回成功的空数组；合法的无焦点与未知窗口格点也不能被猜补。
4. 查询携带非原子范围，动作区分已确认、已观察和结果不确定。跨连接事件不能
   充当事务屏障，观察到条件也不证明因果；不得回退到当前窗口或自动重发。
5. 几何标出坐标空间和可用性，active 与 focused 分开。没有初始窗口归属或
   可靠输出事件时应暴露缺口，不能据当前 viewport 或变换后的 bbox 制造关系。

## Go / no-go 与 pending FT67 的修订建议

| 目标 | 本轮决定 | 解除限制需要的证据 |
| --- | --- | --- |
| niri | **GO：继续嵌套验证这一小范围；NO-GO：现在宣布最低基线已支持或冻结生产接口**。保持 FT67 的首选候选建议，但不是已测排名 | 两个初始状态事件、滚动／动态工作区、显式 ID 动作、空焦点和重连实际日志；最低基线还缺 layer-shell、输出联接与可靠锁等 |
| Wayfire | **GO：在固定插件组合上做同等嵌套验证；NO-GO：现在宣称完整窗口工作区模型／生产支持** | 确认方法和插件，观察切换未执行但返回 ok 的情况，补足初始窗口格点归属及输出事件缺口；不能沿用 PR67 的 handle 转 IPC ID |

对 FT67 只提出下列增补，不修改尚未合入的矩阵：

1. niri 26.04 的 press/release 分组绑定不能按滚动文档当作已发布能力。FT67 review
   标出其 **Since: next release**，应记 development-only；本实验只用普通组合键。
2. Wayfire 0.10 `list-views` 提供 wset，但没有窗口的明确初始格点；
   `view-workspace-changed` 只能证明某次移动的目的坐标。最低基线若要求完整的
   初始窗口→工作区关系，当前证据不足，不能由几何或当前 viewport 猜补。
3. Wayfire `output-added/removed` 的发出路径不等于可订阅能力；本版本 signal map
   未注册它们。周期读取能观察当前输出集合，不能恢复短暂中间事件。
4. 最低基线继续采用文字窗口列表、原生工作区导航、外部设置和普通快捷键。
   动作确认必须允许“不确定／未观察到”；重连后旧身份、纹理与待执行动作都失效。

[start]: https://niri-wm.github.io/niri/Getting-Started.html
