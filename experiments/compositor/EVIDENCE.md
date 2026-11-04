# FT68 证据与检查记录

2026-11-05，仓库基线 `d6a2e5a2`。本轮复用中断构建的草稿，编辑前已将
`experiments/` 和耦合清单备份到工作树外的父构建目录。
范围仅为本实验目录和[耦合清单的入口链接](../../docs/hyprland-coupling.md)。

**niri 与 Wayfire 的真实会话观察全部 UNTESTED。** 本次没有启动合成器、UI、
嵌套桌面或连接真实桌面 socket。下面分别记录发布源码、模拟传输和运行观察；
假服务端返回的数据不是合成器输出。未安装依赖，没有非固定版本安装或清单变更。
没有 Go／Blueprint 改动，无需 `make i18n` 或 `make generate`。

## 来源核对

目标为 niri 产品 **26.04 / niri-ipc 26.4.0** 和 Wayfire **0.10.0**
（Debian 上游源码镜像 **0.10.0-1**）。固定 URL 的内容重新取得并与中断构建
保留的源码逐字比较，下面 15 份文件一致。docs.rs 页面只取其中 Rust 源码作比较，
不比较页面导航。只读取源码，没有下载或执行合成器二进制。

| 编号 | 可检查的固定来源 | 核对位置与结论 |
| --- | --- | --- |
| N1 | [niri-ipc lib.rs][N1] | `Request`、`Reply`、`Response`；逐行 JSON，EventStream 独占连接；独立请求不原子 |
| N2 | [socket.rs][N2] | `Socket::send`、`read_events`；换行分帧、先 Handled 再事件；事件与命令用两个连接 |
| N3 | [state.rs][N3] | `EventStreamState::replicate`、`WorkspacesState`、`WindowsState`；完整初始数组及跨实体暂时不一致。replicate 中工作区在窗口前，但本次没有测服务端实际发送顺序 |
| W1 | [ipc.cpp][W1] | `handle_fd_incoming`、`send_json`、`ipc_plugin_t::init`；native uint32 长度，接收上限含头，发送上限不含头，`_WAYFIRE_SOCKET` 可显式指定 |
| W2 | [ipc-method-repository.hpp][W2] | `list-methods`、`call_method`、`json_ok/error`；数组、ok 与 error 的包络不同，方法可注销 |
| W3 | [ipc-helpers.hpp][W3] | `find_view_by_id(uint32_t)`、`find_output_by_id(int32_t)`；JSON uint64 输入不能安全地任意截断为查找 ID |
| W4 | [ipc-rules.cpp][W4] | `list_views/outputs/wsets`、`get_focused_view`、`focus_view`；原生 ID，空焦点是 info:null，focus 调 default_wm 请求 |
| W5 | [ipc-rules-common.hpp][W5] | `view_to_json`、`output_to_json`、`wset_to_json`；有 wset、网格、不同几何，无窗口初始格点字段；-1 表示无关联 |
| W6 | [ipc-events.hpp][W6] | `signal_map`、`on_client_watch` 和各回调；watch 无初始快照；15 个选定事件均在 map；output-added/removed 未在 map |
| W7 | [ipc-utility-methods.hpp][W7] | `get_wayfire_configuration_info`、`get_config_option`；build/API 信息及 core/plugins 只读查询，不是产品版本认证 |
| W8 | [vswitch.cpp][W8] | `request_workspace`、`set_capabilities`、`add_direction`；output-id+x+y；能力申请失败或 add_direction 失败仍可返回 ok |
| W9 | [main.cpp][W9] | `-c` 参数与子进程 WAYLAND_DISPLAY 设置；记录实际子显示名称，不硬编码 wayland-0 |
| W10 | [autostart.cpp][W10] | 读取 compound entries，autostart_wf_shell=false 禁止默认 panel/background 启动 |
| W11 | [autostart.xml][W11] | 样例 autostart 配置字段 |
| W12 | [wayfire.ini][W12] | core、command 的配置形式；实验样例省去上游示例的主会话服务启动项 |

niri 的 `Window`、`WindowLayout`、`Workspace`、`WorkspaceReferenceArg`、
`Action::FocusWindow/FocusWorkspace` 和所记录的 `Event` 均在 N1 中。
每个动作的精确 JSON、事件字段和所需插件见 [niri 对照表](niri/README.md#wire-与证据对应)
及 [Wayfire 对照表](wayfire/README.md#wire方法和事件)。这些表是源码证据，
不是每个字段均经过完整 schema 验证的声明；探针验证所用身份、状态和包络，
其他字段原样记录。

另读官方[工作区说明][N4]、[启动说明][N5]、[按键说明][N6]和
[26.04 发布页][N7]。前三者为滚动文档，不用于证明旧版新增能力。
按键页的 Press and Release Bindings 标为 “Since: next release”；
因此保留 FT67 review 的 development-only 限制，样例不使用 press/release 分组。
窗口模式默认 Mod=Alt；Wayfire 样例使用 Super。

源码检查纠正了草稿的一处归因：Wayfire `focus_view` 检查 toplevel，
**mapped 检查是本探针的额外预检**，不能写成该 IPC 方法的服务端保证。
此外，W6 的显式空 events 数组会触发发送函数的空集合分支；本实验不使用此
特殊路径，也不以它作为可靠输出热插拔方案。非空白名单和省略 events 都没有
output-added/removed 的订阅项。

## 各动作与事件的证据层次

下表“模拟通过”只表示 [test_probes.py](test_probes.py) 中的假 Unix 服务端
按固定格式驱动了探针，不能证明上游实现或桌面行为。

| 实验项 | niri 源码／模拟结果 | Wayfire 源码／模拟结果 | 真实会话 |
| --- | --- | --- | --- |
| 窗口与工作区枚举 | N1；空数组、u64 ID、id≠idx，模拟通过 | W4/W5；views、outputs、wsets 与网格、-1 哨兵，模拟通过 | 两者 UNTESTED |
| 订阅后查询 | N1/N2/N3；先 EventStream 确认，再 Workspaces/Windows，模拟通过 | W2/W6；先 watch 确认，再 views/outputs/wsets/focused-view，模拟通过 | 两者 UNTESTED |
| 初始事件与顺序 | WorkspacesChanged、WindowsChanged 的粘连帧按接收顺序保存，模拟通过 | view-focused:null 在快照期间排队，随后记录，模拟通过 | 两者 UNTESTED |
| 持续窗口／工作区事件 | N1 Event；完整事件清单见目标说明，部分包络与 ID 校验通过 | W6 的 15 个订阅名及回调字段已核对，workspace-change 坐标校验通过 | 打开、关闭、标题、布局、焦点、MRU、归属变化均 UNTESTED |
| 按窗口 ID 聚焦 | N1 FocusWindow；超过 2^53 的整数不损失，模拟通过 | W3/W4 focus-view；原生 view ID，模拟通过 | 两者 UNTESTED |
| 显式切换工作区 | N1 Id(u64)；不接受 idx 冒充 ID，模拟通过 | W8 output-id+x+y；越界和未知输出拒绝，模拟通过 | 两者 UNTESTED |
| 请求确认与状态观察 | Handled 后查 is_focused，模拟通过 | ok 后查 focused-view 或 output viewport，模拟通过 | 焦点政策、滚动、动画完成和指针行为均 UNTESTED |
| 拒绝、丢回复、未达成 | 两类动作各发送一次，无自动重试，模拟通过 | 两类动作各发送一次，无自动重试，模拟通过 | 实际拒绝原因和竞态均 UNTESTED |
| 断开与重连 | invalidated 清空状态，新 generation 重查版本、订阅和快照，模拟通过 | 同左，并重新查询方法、配置；旧方法集合清除，模拟通过 | 同路径恢复、重启换路径均 UNTESTED |

事件和查询没有跨连接序列号。日志只提供客户端读取顺序和查询区间，不能重建
合成器全局时间线。事件不会被合并进查询结果；每秒重读也可能漏掉短暂变化。
`action-observed` 只表示查询条件成立，不能证明因果或画面已经完成动画。

## 不支持、未知与未测

| 项目 | niri | Wayfire |
| --- | --- | --- |
| 本程序未实现 | 关闭／移动窗口、创建／移动工作区、Wayland 协议绑定、捕获、输入、锁、portal、生产接口 | 同左；不使用 foreign-toplevel，也不将 handle 换算成 IPC view ID |
| 在所选通道中不足 | 多次查询不是事务；无跨连接动作完成屏障 | 同左；窗口初始格点归属缺字段；所选 watch 白名单无可靠输出增删流 |
| 不能统一的语义 | 动态工作区 ID、每屏可变 idx、active 与 focused、滚动 tile/view 坐标 | wset index、输出和二维格点分开；sticky 和不同几何不能压成单一全局矩形 |
| 仍未知 | IPC 与捕获身份关联、逐动作不 warp、最低基线的完整组合 | 捕获身份链、完整初始窗口→格点关系、插件组合对动作效果的影响 |
| 全部未测 | 真实协议握手、嵌套配置加载、窗口生命周期、动态工作区、滚动、多屏、热插拔、权限、XWayland、崩溃恢复 | 真实插件加载、窗口生命周期、格点切换、焦点、动画、多屏、热插拔、权限、XWayland、崩溃恢复 |

缺 compositor 是本轮预期条件，不阻止交付源码核对和可运行探针，也不能转写成
真实验证通过。两者均 **GO：继续手动嵌套实验；NO-GO：宣布生产支持或冻结接口**。
具体 Hyprland 差异、身份／几何／事件／错误边界，以及解除限制所需证据见
[公共说明](README.md#与-hyprland-的差异及对后续设计的限制)。

## 自动检查

使用 Python **3.14.4**、Go **1.26.1**。命令在仓库根目录执行，运行器清空环境后
只传入测试 PATH、独立 HOME、XDG config/data/state/cache/runtime、Go 缓存、
TMPDIR 和两个指向不存在 socket 的 D-Bus 地址。测试需要 D-Bus 时只启动自己的
私有 bus。没有 Wayland、DISPLAY、Hyprland、niri 或 Wayfire 环境变量。

测试 PATH 排除 Hyprland/hyprctl/start-hyprland/niri/wayfire、gpg/gpg2/gpg-agent/pass、
voxtype、node/nodejs/bun。没有调用合成器或凭据工具，也没有操作 NetworkManager、
voxtype、inua 进程、用户设置或窗口。复用已有 Go 模块／构建缓存，设置
`GOPROXY=off GOSUMDB=off GOTOOLCHAIN=local`，不下载依赖。

TMPDIR 用运行器持有的 `/proc/<parent>/fd/<fd>` 短入口，所有实际临时文件、
日志、缓存均在父构建目录内。`disk.test`、`upgrade.test`、`vm.test`、`vmui.test`
检查真实路径或可执行文件位置，Go 的 `-exec` 运行器仅对这四个程序改用同一
TMPDIR 的真实路径，其余环境不变；没有改测试或跳过失败项。

实际执行的测试命令（均在上述环境内）：

```sh
python3 -m unittest discover -s experiments/compositor -p 'test_*.py' -v
# GOFLAGS="-count=1 -v -exec=$FT68_CHECK/go-test-exec.py"
make test
git diff --check
```

`make test` 实际执行 `python3 scripts/test-session.py`、`go vet ./...`、
`CGO_ENABLED=1 go test -race ./...`。`-count=1` 禁用测试结果缓存。

| 检查 | 本轮结果 |
| --- | --- |
| 探针 Python suite | 退出码 0；17 tests，5.635s，OK，无跳过 |
| 仓库 Python 会话测试 | 15 tests，0.349s，OK；使用原有假 compositor/systemctl |
| go vet | 通过 |
| Go race suite | 退出码 0；103 包通过，33 包无测试文件，0 包失败；1159 个顶层测试通过，53 个顶层跳过，另有 6 个子测试跳过 |
| diff 与文档链接 | `git diff --check` 通过；实验内部文件／标题链接检查通过 |
| 真实合成器／UI | 未运行，全部 UNTESTED |

17 项探针测试覆盖：分片／粘连 Unicode 帧；错误长度、截断、JSON／UTF-8／重复键；
半帧超时；空数组、null、重复／布尔 ID；完整原生动作 ID 和二维坐标；未知 ID／
越界不发动作；缺插件与错误包络；缺动作／watch 方法；两类动作拒绝／丢回复／
未达成且不重放；断线失效与新代快照；空闲 watch 不误超时；事件包络与顺序；
重连预算；显式 socket CLI；订阅先于非原子快照；访问拒绝不重试；失败分类。
访问权限的 OS 分支通过替换 socket.connect 抛 PermissionError 检查，即使以 root
运行也确定可复现；它不证明真实 socket 的权限策略。

跳过项分组如下，与 FT67 的结果分别记录；本轮多出的 5 项是主动排除 gpg/pass。

| 原因 | 范围 |
| --- | --- |
| PATH 排除 gpg/pass | apps/gpg 的 TestRoundTrip、TestKeyring；apps/pass 的 TestStore；keyring 的 TestPass；pinentry 的 TestLookupKey |
| PATH 排除 Hyprland | hyprconf 的 TestRenderVerifies、TestRenderDisplaysVerifies；配置验证条件分支也未运行 |
| 缺 libinua-vt.so | term/host 的 8 个引擎测试，term/shellint 的 Bash/Zsh，term/vt 的 29 个顶层测试；Snapshot 另有 4 个子项跳过 |
| 缺 libturbojpeg、fish | viewer 的 TestTurbo，term/shellint 的 TestFish |
| helper 设计 | term/host 的 TestHelperReport；term/pty 的 TestHelperDetach、TestInterruptHelper |
| 未设下载地址 | vm 的 TestISODownloadRealImage |
| PATH 排除 node/bun | workshop 的 TestJSModule，TestSessionJS 的 node/bun 子项 |

## 复现隔离环境

下面是本轮运行器的等价配置。先把 `FT68_MODULES`、`FT68_CACHE` 设为父构建目录内
已有的 Go 模块和构建缓存目录；不指向用户 HOME，也不使用缺失时的联网安装。
`FT68_CHECK` 是本次检查新建的目录。目录 fd 必须由等待子进程结束的父进程持有，
不能把 `/proc/self/fd` 字符串直接传给子进程。

```sh
export FT68_CHECK="$(mktemp -d "$PWD/../ft68-check.XXXXXX")"
# FT68_MODULES / FT68_CACHE: explicitly select existing build-owned caches.
python3 - <<'PY'
import os
from pathlib import Path
import shutil
import subprocess

root = Path(os.environ['FT68_CHECK']).resolve()
go = Path(shutil.which('go')).resolve()
for name in ('home', 'config', 'data', 'state', 'cache', 'runtime', 'tmp', 'bin'):
    (root / name).mkdir(mode=0o700)
excluded = {'Hyprland', 'hyprctl', 'start-hyprland', 'niri', 'wayfire',
            'gpg', 'gpg2', 'gpg-agent', 'pass', 'voxtype', 'node', 'nodejs', 'bun'}
for program in Path('/usr/bin').iterdir():
    if program.name not in excluded and program.is_file():
        (root / 'bin' / program.name).symlink_to(program)
runner = root / 'go-test-exec.py'
runner.write_text('''#!/usr/bin/python3
import os
from pathlib import Path
import sys
if Path(sys.argv[1]).name in {'disk.test', 'upgrade.test', 'vm.test', 'vmui.test'}:
    os.environ['TMPDIR'] = os.environ['FT68_REAL_TMP']
os.execv(sys.argv[1], sys.argv[1:])
''')
runner.chmod(0o700)
fd = os.open(root / 'tmp', os.O_RDONLY | os.O_DIRECTORY)
env = dict(PATH=f'{go.parent}:{root}/bin', HOME=str(root / 'home'),
           TMPDIR=f'/proc/{os.getpid()}/fd/{fd}', FT68_REAL_TMP=str(root / 'tmp'),
           GOPATH=os.environ['FT68_MODULES'], GOCACHE=os.environ['FT68_CACHE'],
           GOTOOLCHAIN='local', GOPROXY='off', GOSUMDB='off',
           GOFLAGS=f'-count=1 -v -exec={runner}', PYTHONDONTWRITEBYTECODE='1',
           DBUS_SESSION_BUS_ADDRESS=f'unix:path={root}/no-session',
           DBUS_SYSTEM_BUS_ADDRESS=f'unix:path={root}/no-system')
for key, name in [('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'),
                  ('XDG_STATE_HOME', 'state'), ('XDG_CACHE_HOME', 'cache'),
                  ('XDG_RUNTIME_DIR', 'runtime')]:
    env[key] = str(root / name)
try:
    for name, command in [
        ('probe-test', ['python3', '-m', 'unittest', 'discover', '-s',
                        'experiments/compositor', '-p', 'test_*.py', '-v']),
        ('make-test', ['make', 'test']), ('diff-check', ['git', 'diff', '--check'])]:
        with (root / (name + '.log')).open('w') as log:
            result = subprocess.run(command, env=env, stdout=log, stderr=subprocess.STDOUT)
        print(name, 'exit', result.returncode)
        if result.returncode:
            raise SystemExit(result.returncode)
finally:
    os.close(fd)
PY
```

运行日志留在检查目录，不提交含本机路径的原始日志。真实验证时另外记录每个
目标的版本、插件包、配置散列、请求／回复／事件 JSONL、人工观察和失败原因，
按目标 README 的步骤执行，不能将上述自动检查复制成真实观察。

## 对 pending FT67 与 PR67 的处理

owner thread 明确授权此次实验，覆盖 issue #61 旧等待条件；没有因此开始
#62–64。FT67 ready 但未 shipped，其最低基线和首选 niri 都仍是待采纳建议。
本次不修改矩阵，只建议加入 development-only 绑定限制、Wayfire 初始格点／
输出事件缺口，以及动作确认、空值和重连代际的接口约束。

PR67 提供版 diff 的 Wayfire 0.9 记录只提示了 IPC、插件、viewport 和
foreign-toplevel 的区别。其 Wayland reader 将连接内 object ID 写成 `wf:<id>`，
IPC client 又将该数字传给 focus-view，diff 没有证明两种身份相同。本实验直接
枚举 IPC view ID。没有导入、合并、修复 PR67，也没有把其截图或运行记录计入
本次 0.10 结果。

[N1]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/lib.rs.html
[N2]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/socket.rs.html
[N3]: https://docs.rs/niri-ipc/26.4.0/src/niri_ipc/state.rs.html
[N4]: https://niri-wm.github.io/niri/Workspaces.html
[N5]: https://niri-wm.github.io/niri/Getting-Started.html
[N6]: https://niri-wm.github.io/niri/Configuration:-Key-Bindings.html#press-and-release-bindings
[N7]: https://github.com/niri-wm/niri/releases/tag/v26.04
[W1]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/ipc.cpp
[W2]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/wayfire/plugins/ipc/ipc-method-repository.hpp
[W3]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc/wayfire/plugins/ipc/ipc-helpers.hpp
[W4]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules.cpp
[W5]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-rules-common.hpp
[W6]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-events.hpp
[W7]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/ipc-rules/ipc-utility-methods.hpp
[W8]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/vswitch/vswitch.cpp
[W9]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/src/main.cpp
[W10]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/plugins/single_plugins/autostart.cpp
[W11]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/metadata/autostart.xml
[W12]: https://sources.debian.org/data/main/w/wayfire/0.10.0-1/wayfire.ini
