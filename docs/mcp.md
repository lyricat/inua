# MCP: inua's tools

inua serves its desktop tools over the
[Model Context Protocol](https://modelcontextprotocol.io): AI agents on
this computer (Claude Code, Codex, any MCP client) and inua's own
assistant use them to see the windows, open apps, change settings and
more. [apps.md](apps.md#mcp-inuas-tools-for-the-assistant-and-for-agents)
says how it looks to the user; this page lists the interface.

## The server

| | |
|---|---|
| Command | `inua mcp` (it becomes `inua-assistant --mcp`, keeping its process) |
| Transport | stdio: JSON-RPC 2.0, one message a line; stderr is the server's log |
| Server name | `inua` (title "inua desktop shell"), the release as its version |
| Capabilities | `tools` only: no resources, prompts or sampling |
| Turned on by | **Settings → AI → MCP** (`[mcp] enable` in config.toml); off, `inua mcp` exits with an error |

Connect Claude Code with `claude mcp add inua -- inua mcp`; any other
agent: a stdio server whose command is `inua mcp`.

The tools are those the policy offers when the agent connects (see
below). Settings are read again at each call: turning MCP off, or
tightening the policy, takes effect at the next call, which is then
refused.

## Tools

Arguments are JSON objects, as each tool's `inputSchema` says. A result
is one text item: a string (often `ok`) or JSON. A failure is a result
with `isError: true` and the reason as its text, never a protocol error.

**Risk** says what the user is asked:

- **read**: runs without asking; offered to every policy.
- **read, asks**: may hold secrets or private messages (the clipboard,
  its history, the notifications); asks whatever the policy.
- **change**: asks under the policy *Ask before changes*; runs under
  *Change without asking*; not offered to *Look only*.
- **change, asks**: closes, ends, clears or powers off something; always
  asks.

The annotations say the same: `readOnlyHint` for reads,
`destructiveHint` for the tools that always ask.

### Windows

A `window` argument is a window's id from `list_windows`, or words of
its title or app (case ignored); empty is the focused window. Words that
match several windows are an error naming them, to pass an id.

| Tool | Arguments | Result | Risk |
|---|---|---|---|
| `list_windows` | | `[{id, title, app, workspace, focused?}]`: the mapped windows | read |
| `focus_window` | `window?` | the window | change |
| `move_window` | `window?`, `workspace` (a number or a name) | the window; it moves without following | change |
| `switch_workspace` | `workspace` (a number or a name) | `ok` | change |
| `close_window` | `window?` | the window; its app may ask to save | change, asks |
| `list_workspaces` | | `{workspaces: [{id, name, monitor, windows, shown?, focused?}], monitors: [{name, width, height, scale, focused?, workspace}]}`; ids are the compositor's strings (Hyprland's numbers as text); scratchpads left out | read |

### Apps

| Tool | Arguments | Result | Risk |
|---|---|---|---|
| `list_apps` | `query?`: words in the name, kind, comment or keywords | `[{Name, ID, About}]`, at most 40 | read |
| `open_app` | `app`: a name or desktop id; words that match one app are enough | `{opened: name}` | change |

### The system

These go through the running shell; without it they fail with "the shell
is not running".

| Tool | Arguments | Result | Risk |
|---|---|---|---|
| `system_status` | | `{battery: {percent, charging}, wifi: {available, on, network, connection}, bluetooth: {available, on, connected: [names]}, volume: {percent, muted}, brightness_percent, power_saver, focus, time}` | read |
| `set_system` | `setting`: `wifi`, `bluetooth`, `mute`, `focus`, `saver` take `value` `on`/`off`; `volume`, `brightness` take `0`–`100` | `ok` | change |
| `lock_screen` | | `ok` | change |
| `screenshot` | `of`: `window` (the focused one), `screen`, or `area` (the user drags it out; the default) | `ok`; saved to the screenshot folder and copied | change |
| `power` | `action`: `suspend`, `reboot`, `poweroff`, `logout` | `ok` | change, asks |
| `show_panel` | `panel`: `launcher`, `overview`, `control_center`, `calendar`, `clipboard`, `notifications`, `settings`; `page?`: a Settings page (`appearance`, `bar`, `launcher`, `ai`, `notifications`, `clipboard`, `keyring`, `shortcuts`, `keyboard`, `mouse`, `about`) | `ok` | change |
| `media_status` | | `{available, player, title, artist, playing, can_play_pause, can_next, can_previous}`: the player the control center shows | read |
| `media_control` | `action`: `play_pause`, `next`, `previous` | `ok` | change |

### Notifications and the clipboard

| Tool | Arguments | Result | Risk |
|---|---|---|---|
| `list_notifications` | | `{focus, notifications: [{id, app, summary, body, time, actions?: [{key, label}]}]}`, newest first, at most 50 | read, asks |
| `dismiss_notification` | `id` | `ok`; as its × | change |
| `notification_action` | `id`; `action?`: a key from its `actions`, `default` (as a click) by default | `ok` | change |
| `clear_notifications` | | `ok` | change |
| `notification_history` | `query?`: words in the app, summary or body; `limit?`: 20 by default, up to 200 | `[{id, app, summary, body, time}]`, newest first, dismissed ones too; an error when the history is off (Settings → Notifications → **Keep a history**, off by default) | read, asks |
| `clipboard_read` | | `{text}`, cut at 20,000 bytes | read, asks |
| `clipboard_write` | `text` | `ok` | change |
| `clipboard_history` | `query?`: words in the text or the app; `limit?`: 20 by default, up to 100 | `[{id, type, text?, cut?, path?, mime?, source?, time}]`, newest first; text over 2,000 characters is cut; an image is its file | read, asks |
| `clipboard_history_copy` | `id` | `ok`; the entry is on the clipboard again | change |
| `clipboard_history_delete` | `id` | `ok` | change |
| `clipboard_history_clear` | | `ok` | change, asks |

### Processes, Tailscale, weather

| Tool | Arguments | Result | Risk |
|---|---|---|---|
| `processes` | `by`: `cpu` (the default, measured over half a second) or `memory`; `limit`: 1–30, 8 by default | `[{pid, Name, User, cpu_percent, memory_mb}]` | read |
| `end_process` | `pid`; `force`: kill instead of asking to quit | `ok`; only the user's own processes | change, asks |
| `tailscale_status` | | `{state, this_device, devices: [{Name, Address, OS, Online, exit_node?, offers_exit?}]}` | read |
| `tailscale_set` | `connect`: true or false; or `exit_node`: a device's name, or `none` | `ok` | change |
| `weather` | `place`: a city | `{place, now: {temp_c, feels_c, sky, humidity, wind_kmh}, hours: [{time, temp_c, sky}] (12), days: [{date, low_c, high_c, sky, rain_percent}] (4)}`, from Open-Meteo | read |

The assistant's other tools, `web_search`, `url_fetch`, `read_file` and
`bash`, are not served: agents have their own.

The same tools are the functions of workshop apps
([workshop.md](workshop.md#inuas-desk-functions)), by the same names.

## Policy

`[mcp] policy` (**Settings → AI → MCP → What agents may do**):

| Policy | Offered | Asks |
|---|---|---|
| `read` (Look only) | the reads, but not those that ask | nothing |
| `ask` (Ask before changes, the default) | every tool | changes, and the tools that always ask |
| `allow` (Change without asking) | every tool | only the tools that always ask |

For agents, `focus_window`, `switch_workspace`, `open_app`,
`screenshot`, `show_panel` and `media_control` count as changes (the
assistant runs them without asking, since the user asked it by typing).

## Asking

A call that asks waits for inua's dialog. It names the agent (from the
client's `clientInfo` title or name), what started `inua mcp`
(`claude ← zsh`) and the call ("Closing Firefox"):

- **Deny**: the result is an error, "The user did not allow it."
- **Allow once**: this call runs.
- **Allow for this session**: this tool runs without asking until the
  agent disconnects.

Without a shell to ask, or with no answer in 10 minutes, the call is
denied.

## The record

Every call is a line in `~/.local/state/inua/mcp-calls.jsonl`:

```json
{"time":"2026-10-15T10:12:03+09:00","agent":"claude-code","chain":"claude ← zsh","tool":"close_window","doing":"Closing Firefox","decision":"allowed"}
```

`decision` is `ran` (no asking), `allowed`, `denied` or `refused` (MCP
off, or the policy does not offer the tool now); `error` is set when the
tool failed.

## inua's assistant

The assistant starts the same server for each question, through Mister
Morph's MCP client, with `INUA_MCP_CLIENT=assistant`. The server takes
that only when its parent is `inua-assistant` itself; then:

- it runs whether MCP is on or off, and offers every tool, whatever the
  policy;
- the tools that always ask still ask; changes ask when the assistant's
  **Ask before changes** is on;
- the dialog asks "Let inua's assistant do this?" with **Deny** and
  **Allow** (this call only); a denied call's error begins `declined:`,
  which the assistant's window shows as not done;
- the record names the agent "inua's assistant".

The tools reach the model as `mcp_inua__<tool>`, hidden until its
`tool_search` finds them; what it found stays for the conversation.
