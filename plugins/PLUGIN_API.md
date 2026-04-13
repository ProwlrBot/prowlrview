# prowlrview plugin API (draft, v0.3 target)

Two runtimes, same event surface:

- **Lua** (gopher-lua) — hot-reload, no build step, great for one-off scripts
- **WASM** (wazero) — sandboxed, any-language, for heavy passive scanners

Drop scripts into `~/.config/prowlrview/plugins/`. Re-read on `SIGHUP` or `r` in the TUI.

## Events

| Event         | Fires when                                | Payload                                     |
|---------------|-------------------------------------------|---------------------------------------------|
| `on_request`  | proxy intercepts an outbound HTTP request | `{method, host, path, headers, body}`       |
| `on_response` | proxy receives a response                 | `{status, headers, body, duration_ms}`      |
| `on_finding`  | adapter or plugin adds a `finding` node   | `{id, rule, severity, url, source, detail}` |
| `on_node`     | any node is added/updated                 | `{id, kind, label, parent, severity}`       |
| `on_tick`     | every 5s                                  | `{now, node_count, finding_count}`          |

## Globals (Lua)

```lua
graph:tag(node_id, tag, color)       -- add a colored tag to a node
graph:upsert(id, kind, label, parent, severity)
graph:raise(finding_id, severity)    -- promote severity
notify(msg)                          -- status bar toast
log(msg)                             -- debug log pane
req:header(k, v)                     -- rewrite outbound header
req:replace_body(s)
resp:matches(pattern)                -- regex against body
```

## Conventions

- Plugins are pure event handlers — no global state across events.
- Use `graph:tag` for discoveries rather than mutating nodes directly.
- Findings added by plugins get `source = "plugin:<name>"`.

## WASM ABI

Export `prowlrview_handle(event_ptr, event_len) -> result_ptr`. Events and results are JSON. Exact schema in `plugins/wasm/schema.json` once v0.4 lands.
