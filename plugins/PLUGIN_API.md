# prowlrview plugin API (v1.0)

Two runtimes, same event surface:

- **Lua** (gopher-lua) — hot-reload, no build step, great for one-off scripts
- **WASM** (wazero) — sandboxed, any language, for heavier scanners

Drop Lua scripts into `~/.config/prowlrview/plugins/`. Reload on `SIGHUP` or `r` in the TUI. WASM plugins are loaded from the same directory (`*.wasm`).

---

## Quick start

```sh
prowlrview init                # enable all plugins + themes
prowlrview proxy :8888         # start MITM, fires on_request/on_response
# trust ~/.config/prowlrview/ca.crt in your browser/system to MITM HTTPS
```

---

## Events

| Event         | Fires when                                | Payload fields                                             |
|---------------|-------------------------------------------|------------------------------------------------------------|
| `on_request`  | proxy intercepts an outbound HTTP request | `{method, host, path, url, headers, body}`                 |
| `on_response` | proxy receives a response                 | `{status, url, headers, body, duration_ms}`                |
| `on_finding`  | a finding node is added to the graph      | `{id, rule, severity, url, source, detail, confidence}`    |
| `on_node`     | any node is added or updated              | `{id, kind, label, parent, severity, detail, confidence, tags}` |
| `on_tick`     | every 5 seconds                           | `{now, node_count, finding_count}`                         |

---

## Globals (Lua)

### Graph

```lua
-- Add or merge a node. confidence is optional (0.0–1.0, default 1.0).
graph:upsert(id, kind, label, parent, severity [, confidence])

-- Add a colored tag string to an existing node.
graph:tag(node_id, tag_name, color_hex)

-- Promote a node's severity (never demotes).
graph:raise(node_id, severity)

-- Return all graph nodes as a list of tables {id, kind, label, severity, ...}.
graph:nodes()

-- Return all finding nodes as a list.
graph:findings()

-- Return direct children of a node.
graph:children(node_id)
```

`severity` values: `"info"`, `"low"`, `"medium"`, `"high"`, `"critical"`.

### Notifications

```lua
notify(msg)   -- status bar toast + log pane
log(msg)      -- log pane only (debug)
```

### HTTP (active-scan plugins only)

Both calls are rate-limited by the plugin's `[rate]` config and scope-checked before dispatch.

```lua
local resp = http.get(url)
-- resp.status (number), resp.body (string), resp.headers (table)

local resp = http.post(url, body, content_type)
```

### Filesystem (graph plugins)

Sandboxed to `~/` — paths outside the user home dir are rejected.

```lua
fs.write(path, content)   -- write string to file; creates parent dirs
local s = fs.read(path)   -- read file as string; returns nil on missing
```

### Config

Each plugin's `plugin.toml` `[config]` section is exposed as a `config` global table:

```lua
-- plugin.toml:
-- [config]
-- probe = "false"
-- vault = "/home/user/vault"

local should_probe = config.probe == "true"
local vault_path   = config.vault
```

---

## Request / response objects

```lua
on_request(function(req)
  req.method       -- "GET", "POST", …
  req.host         -- "api.target.com"
  req.path         -- "/api/users/42?foo=bar"
  req.url          -- full URL string
  req.headers      -- table: lowercase name → first value

  req:header("Authorization")       -- get header value
  req:set_header("X-Custom", "val") -- mutate outbound header
  req:body()                        -- read body as string
  req:replace_body("new body")      -- replace body + fix Content-Length
  req:block("reason")               -- 403 short-circuit; stops further plugins
end)

on_response(function(resp)
  resp.status      -- 200, 404, …
  resp.url         -- string
  resp.headers     -- table: lowercase name → first value
  resp.body        -- string

  resp:header("Content-Type")       -- get header value
  resp:body()                       -- same as resp.body
  resp:matches("pattern")           -- Go regex against body; true/false
end)
```

---

## Plugin manifest (`plugin.toml`)

```toml
name     = "idor-hunter"
version  = "0.2.0"
author   = "kdairatchi"
license  = "MIT"
summary  = "Flags numeric-ID and UUID API paths as IDOR candidates."
category = "active-scan"
events   = ["on_request", "on_response"]
tags     = ["idor", "authz", "api"]

[rate]
rpm   = 30
burst = 5

[config]
probe = "false"
```

`[rate]` is only enforced for `active-scan` category plugins. `[config]` values are always strings; parse booleans and numbers in Lua.

---

## WASM ABI

Export a single function: `prowlrview_handle(event_ptr i32, event_len i32) -> result_ptr i32`.

- Input: JSON-encoded event (UTF-8, written to WASM linear memory before the call).
- Output: pointer to a JSON-encoded result written into WASM memory by the plugin. The host reads until it finds a null byte.
- Full schema: `plugins/wasm/schema.json`.
- TinyGo and Rust examples: `plugins/wasm/README.md`.

WASM plugins are loaded from `*.wasm` files in the plugin directory alongside Lua plugins. They share the same event surface but cannot call `http.*` or `fs.*` directly — use the host's import functions declared in `schema.json`.

---

## Conventions

- Plugins are pure event handlers. Avoid mutable module-level state unless you need to correlate `on_request` data in a later `on_response` (e.g., storing `Origin` headers for CORS checks).
- Use `graph:upsert` to create nodes; use `graph:tag` for labels and metadata.
- Findings added by plugins automatically get `source = "plugin:<name>"`.
- Use `log()` for debug output. Use `notify()` only for actionable findings — one notify per actual hit, not per request checked.
