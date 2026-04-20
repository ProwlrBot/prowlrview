# prowlrview

Terminal hunting cockpit for bug bounty. One binary: MITM proxy + live attack-surface graph + Lua plugin host.

**Version:** v1.0.0  
**Build:** `go build -o prowlrview ./cmd/prowlrview`  
**Module:** `github.com/ProwlrBot/prowlrview`

---

## What it is

prowlrview unifies three things you currently juggle across separate tools:

1. **MITM proxy** — HTTP/HTTPS intercept via [goproxy](https://github.com/elazarl/goproxy) with auto-generated CA, CONNECT MITM, per-flow graph nodes, and a web dashboard
2. **Live attack-surface graph** — in-memory DAG (nodes = hosts/endpoints/findings, edges = parent→child) fed by any tool that emits JSONL or SARIF
3. **Lua plugin host** — each `*.lua` in `~/.config/prowlrview/plugins/` runs in its own gopher-lua state with an instruction cap; plugins hook request/response/finding/node/tick events and can read+mutate graph state

TUI built with [tview](https://github.com/rivo/tview) on [tcell](https://github.com/gdamore/tcell).

---

## What works now (v1.0.0)

All of the following is compiled and tested code:

- **TUI** — tree (surface) + graph view (`g` to toggle), findings table, detail pane, log pane, filter input, status bar
- **`pipe` mode** — reads JSONL/SARIF from stdin, parses live, updates graph at 2Hz
- **`watch` mode** — inotify-based via fsnotify; tails new `*.jsonl`/`*.json`/`*.sarif` files as tools write them
- **`replay` mode** — loads a `.snapshot.jsonl` file into the graph
- **`proxy` mode** — MITM proxy; TUI adds a flow pane; every request/response fires through Lua plugins
- **`web` mode** — proxy + HTML dashboard with live SSE (`/events`), `/api/flows`, `/api/graph`, `/api/stats`, `/ca.crt`
- **`run recon.yml`** — YAML recon pipeline orchestrator; sequential and parallel stages; stdout tee→graph
- **`findings`** — dump scored findings as JSON or Markdown table
- **`snapshot`** — save/diff graph snapshots across sessions
- **`sync`** — push findings to prowlrbot DB with `h1-anom5x` attribution
- **`caido-import`** / **`caido-push`** — import Caido session export or push a finding back to Caido
- **`session`** — named sessions in `~/.local/share/prowlrview/sessions/`; `new|list|switch|active|delete`
- **`chrome` subcommand** — isolated Chrome profile wired to the proxy; no manual cert trust needed
- **`ca` subcommand** — generate/show/export MITM CA cert; WSL-aware (`win:Downloads` path)
- **Plugin system** — Lua (gopher-lua) + WASM (wazero) plugins; per-plugin TOML config; `[rate]` limiter; `http.get/post`, `fs.write/read`, `graph:nodes/findings/children` globals; fsnotify hot-reload on `SIGHUP` or `r`
- **Plugin registry** — `prowlrview plugin search <tag>`, `install <name>`, `update`; resolves from `manifest.json`
- **Adapters** — nuclei, httpx, subfinder, katana, flaw, dalfox, gau, waybackurls, SARIF, generic fallback
- **Export** — DOT, Mermaid, Obsidian Canvas, JSONL snapshot
- **Themes** — 5 built-in + user TOML themes; `t` cycles in TUI

Tests exist and pass for: graph upsert/prune/severity/score semantics, snapshot round-trip + diff, DOT/Mermaid/Canvas export, proxy round-trip, plugin load + event dispatch.

---

## Plugin status

12 plugins ship in `prowlrview-plugins`. All are functional as of v0.2.0:

| Plugin | Category | Status |
|--------|----------|--------|
| `secret-sniffer` | passive-scan | Functional — 10 patterns: AWS, GitHub PAT, Slack, JWT, private key, Stripe, Azure SAS, Twilio, npm token, Google API |
| `jwt-inspector` | passive-scan | Functional — real base64url decode; alg=none works; RS256 flag; exp missing/long-lived |
| `cors-misconfig` | passive-scan | Functional — wildcard+creds, null-origin, reflected-origin+credentials (critical case) |
| `idor-hunter` | active-scan | Functional — numeric-ID + UUID/GUID paths; optional adjacent-ID probing |
| `sqli-heuristic` | active-scan | Functional — 7 error-string signatures + confidence scoring |
| `ssrf-probe` | active-scan | Functional — query params, POST/JSON bodies, `//` protocol-relative URLs |
| `waf-fingerprint` | recon | Functional — 14 signatures; stacked WAF detection; `on_node` hook |
| `takeover-hunter` | recon | Functional — 17 service fingerprints; `on_node` hook for subfinder nodes |
| `chain-detector` | graph | Functional — 8 chain rules; `on_tick` sweep via `graph:findings()` |
| `scope-guard` | graph | Functional — reads scope from `plugin.toml` `[config]`; no hardcoded values |
| `obsidian-exporter` | graph | Functional — real `fs.write()` for daily note + per-finding Obsidian notes |
| `graph-decorator` | graph | Functional — 14 tech stacks; `/.git`, `/swagger`, `/graphql` flagging |

9 additional plugins are still open (see `prowlrview-plugins/AUDIT.md` gap analysis): `xss-reflector`, `open-redirect-detector`, `path-traversal-lfi`, `ssti-detector`, `host-header-injection`, `xxe-surface`, `oauth-oidc-misconfig`, `cache-poisoning-markers`, `prototype-pollution-surface`.

TOML themes in `~/.config/prowlrview/themes/`. Ships with: `prowlr`, `paper`, `mono`, `terminal`, `solarized`.

---

## Roadmap

| Version | Target | Status |
|---------|--------|--------|
| **v0.1** | TUI, JSONL adapter, MITM proxy, Lua plugin host, web dashboard, export | **complete** |
| **v0.2** | Proxy→plugin→graph pipeline; graph Lua globals; `on_node` dispatch; per-plugin TOML config; hot-reload | **complete** |
| **v0.3** | Active scan rate limiting; `http.get/post` globals; `idor-hunter` mutation; OOB SSRF; finding scoring; `prowlrview findings` | **complete** |
| **v0.4** | Graph view in TUI (`g`); `graph:nodes/findings/children`; `chain-detector` + `scope-guard` + `obsidian-exporter` production-ready; snapshot/diff | **complete** |
| **v0.5** | inotify-based watch; `prowlrview run recon.yml` orchestrator; dalfox/gau/waybackurls adapters; prowlrbot sync | **complete** |
| **v1.0** | WASM (wazero); Caido import/push; named sessions; plugin registry CLI; `prowlrview-plugins` CI harness | **complete** |

---

## Install

**From source (only option right now):**

```sh
git clone https://github.com/ProwlrBot/prowlrview
cd prowlrview
go build -o prowlrview ./cmd/prowlrview
sudo mv prowlrview /usr/local/bin/
```

Requires Go 1.22+. The `go.mod` declares `go 1.26.2`; any recent toolchain works.

---

## Usage

```
prowlrview init                              create config dirs + enable all plugins/themes
prowlrview pipe                              stdin JSONL/SARIF → live graph
prowlrview watch DIR                         inotify-based: tail new *.jsonl/*.sarif as tools write
prowlrview replay SNAP.jsonl                 replay a saved graph snapshot
prowlrview proxy [:port]                     MITM proxy → TUI with flow pane (default :8888)
prowlrview web [:webPort] [:proxyPort]       proxy + HTML dashboard with live SSE (default :8889 + :8888)
prowlrview run recon.yml                     YAML recon pipeline orchestrator
prowlrview findings [--json|--md]            dump scored findings
prowlrview snapshot save NAME                save a named graph snapshot
prowlrview snapshot diff NAME1 NAME2         show what changed between snapshots
prowlrview sync                              push findings to prowlrbot DB
prowlrview caido-import FILE                 ingest a Caido session export JSON
prowlrview caido-push FINDING_ID             push a finding to Caido API
prowlrview session new|list|switch|active|delete
prowlrview chrome [proxyAddr] [URL]          isolated Chrome through the proxy
prowlrview ca [show|install|export DEST]
prowlrview plugin list|enable NAME|disable NAME|enable-all|install NAME|search TAG|update
prowlrview theme  list|enable NAME|enable-all
prowlrview version
```

---

## Quickstart: proxy mode

```sh
# Generate CA cert (done once)
prowlrview ca install

# WSL: copy cert to Windows
prowlrview ca export win:Downloads
# then on Windows: double-click prowlrview-ca.crt → Install → Local Machine → Trusted Root

# Start proxy + TUI
prowlrview proxy :8888

# OR: proxy + browser pre-wired (no cert install needed)
prowlrview chrome :8888 https://target.example.com

# OR: proxy + web dashboard
prowlrview web :8889 :8888
# TUI on stdout, dashboard at http://127.0.0.1:8889
```

Point your browser to `127.0.0.1:8888`. Every flow appears in the TUI flow pane and the graph. Plugins fire on every intercepted request and response.

---

## Quickstart: pipe mode

```sh
# nuclei findings → graph
nuclei -jsonl -l hosts.txt -severity high,critical | prowlrview pipe

# httpx discovery
httpx -json -l hosts.txt | prowlrview pipe

# subfinder + httpx + katana chained
subfinder -d target.com -silent -oJ \
  | httpx -json -silent \
  | tee >(katana -jsonl) \
  | prowlrview pipe

# watch a results directory (polling, 1s interval)
prowlrview watch ~/hunts/target.com/

# replay a saved snapshot
prowlrview replay prowlrview-20260420-153000.snapshot.jsonl
```

**Sample JSONL lines for testing:**

```json
{"template-id":"cve-2024-1234","info":{"name":"RCE in widget","severity":"critical"},"host":"api.target.com","matched-at":"https://api.target.com/v1/upload"}
{"url":"https://api.target.com/admin","input":"api.target.com","status_code":200,"title":"Admin","tech":["Nginx","WordPress"]}
{"host":"beta.target.com","source":"crtsh"}
```

---

## TUI keybindings

| Key | Action |
|-----|--------|
| `q` / `Ctrl-C` | quit |
| `t` | cycle theme (prowlr → cyberpunk → dracula → nightshade → solarized → user themes) |
| `g` | toggle graph view (hierarchical DAG) / tree view |
| `f` | toggle follow (auto-select newest finding) |
| `s` | cycle sort: severity → recent → alpha |
| `/` | focus filter input (fuzzy substring match on labels) |
| `Esc` / `Enter` | return focus to tree from filter |
| `e` | export menu: DOT / Mermaid / Obsidian Canvas / Snapshot JSONL |
| `r` | reload plugins from disk (hot-reload) |
| `?` | show help in detail pane |

---

## Plugin system

### Loading

On startup, `newApp()` calls `plugin.LoadDir(~/.config/prowlrview/plugins/)`. Every `*.lua` in that directory gets its own gopher-lua `LState` with:

- Standard libs: `base`, `table`, `string`, `math` (no `io`, `os`, `package`, `debug`)
- Instruction cap: 1,000,000 instructions per callback (prevents runaway loops)
- Registered types: `pv.request`, `pv.response` with method bindings

### Hooks

```lua
on_request(function(req)   ... end)   -- fires on every proxied request
on_response(function(resp) ... end)   -- fires on every proxied response
on_finding(function(f)     ... end)   -- fires when a finding node is added
on_node(function(n)        ... end)   -- fires when any node is added or updated
on_tick(function(t)        ... end)   -- fires every 5 seconds
```

### Request object (proxy mode only)

```lua
on_request(function(req)
  req.method       -- string: "GET", "POST", etc.
  req.host         -- string: "api.target.com"
  req.path         -- string: "/api/users/42?foo=bar"
  req.url          -- string: full URL
  req.headers      -- table: lowercase header names → first value

  req:header("Authorization")          -- get header value
  req:set_header("X-Custom", "val")    -- mutate outbound header
  req:body()                           -- read body as string (buffers once)
  req:replace_body("new body")         -- replace body + fix Content-Length
  req:block("reason")                  -- short-circuit with 403; stops further plugins
end)
```

### Response object (proxy mode only)

```lua
on_response(function(resp)
  resp.status      -- number: 200, 404, etc.
  resp.url         -- string
  resp.headers     -- table: lowercase header names
  resp.body        -- string (buffered)

  resp:header("Content-Type")          -- get header
  resp:body()                          -- read body (same buffer as resp.body)
  resp:matches("pattern")              -- Go regex match against body; falls back to substring
end)
```

### Graph API

```lua
graph:upsert(id, kind, label, parent, severity [, confidence]) -- add or merge a node
graph:tag(node_id, tag_name, color_hex)                        -- add a colored tag
graph:raise(node_id, severity)                                 -- promote severity (never demotes)
graph:nodes()                                                  -- list all nodes
graph:findings()                                               -- list all finding nodes
graph:children(node_id)                                        -- list direct children

notify("message")   -- status bar toast + log pane
log("message")      -- log pane only
```

`severity` values: `"info"`, `"low"`, `"medium"`, `"high"`, `"critical"`. `confidence` is 0.0–1.0 (default 1.0); affects `Score()` used for findings sorting.

Active-scan plugins also get `http.get(url)` / `http.post(url, body, ct)` — rate-limited per `[rate]` config. Graph plugins get `fs.write(path, content)` / `fs.read(path)` — sandboxed to `~/`.

### Example plugins

**`idor-hunter.lua`** — flags numeric-ID API paths, checks JSON responses for email fields:

```lua
on_request(function(req)
  if req.path:match("/api/.*/%d+") or req.path:match("/users/%d+") then
    graph:tag(req.host, "idor-candidate", "#ff6c11")
    notify("IDOR candidate: " .. req.method .. " " .. req.path)
  end
end)
```

**`secret-sniffer.lua`** — passive regex over responses for AWS keys, GitHub PATs, private keys, JWTs.

**`scope-guard.lua`** — blocks out-of-scope requests with a synthetic 403:

```lua
local scope = { "*.prowlrbot.com", "target.example.com" }
on_request(function(req)
  if not in_scope(req.host) then
    req:block("out-of-scope per scope-guard.lua")
  end
end)
```

**`graph-decorator.lua`** — paints nodes by detected tech stack (WordPress, Laravel, Nginx, Cloudflare).

Full examples in `plugins/examples/`.

### Managing plugins

```sh
prowlrview plugin search cors        # search manifest.json by tag or name
prowlrview plugin install cors-misconfig
prowlrview plugin update             # pull latest manifest + update installed plugins
prowlrview plugin list               # show loaded plugins and status
prowlrview plugin enable NAME        # symlink into config dir
prowlrview plugin disable NAME       # remove symlink
```

Manual install: drop `*.lua` files directly into `~/.config/prowlrview/plugins/`. WASM plugins go in the same directory as `*.wasm`. Set `PROWLRVIEW_PLUGINS_REPO` to override the default repo location.

---

## Themes

Five themes ship as Go code in `internal/theme/theme.go`: `prowlr`, `cyberpunk`, `dracula`, `nightshade`, `solarized`.

User themes: drop a `.toml` into `~/.config/prowlrview/themes/`. Minimal format:

```toml
name = "mytheme"
background = "#0a0a0a"
foreground = "#e0e0e0"
border     = "#444444"
accent     = "#00ff88"
title      = "#ff44aa"

[severity]
critical = "#ff0000"
high     = "#ff8800"
medium   = "#ffff00"
low      = "#00ff00"
info     = "#888888"
```

Three TOML files ship in `themes/`: `prowlr.toml`, `cyberpunk.toml`, `nightshade.toml`. Run `prowlrview init` to symlink them into your config dir.

---

## Export

Press `e` in the TUI for a modal:

| Format | File | Notes |
|--------|------|-------|
| DOT | `prowlrview-<ts>.dot` | Graphviz digraph; `dot -Tsvg` to render |
| Mermaid | `prowlrview-<ts>.mmd` | Paste into Obsidian, GitHub, Notion |
| Obsidian Canvas | `prowlrview-<ts>.canvas` | Drag into your vault — nodes laid out in a grid |
| Snapshot JSONL | `prowlrview-<ts>.snapshot.jsonl` | Replay with `prowlrview replay` |

---

## Adapters (auto-detected)

| Tool | Detection key | What gets added |
|------|--------------|-----------------|
| nuclei | `template-id` field | host node, endpoint node, finding node with severity |
| httpx | `status_code` + `input` | host node, endpoint node with title+tech detail |
| subfinder | `host` + `source` | host node, parent domain node |
| katana | `endpoint` field | host node, endpoint node; referrer edges between URLs |
| dalfox | `type` = `G` + `data` | host node, finding node with `rule=xss` |
| gau | bare URL lines | host + endpoint nodes |
| waybackurls | bare URL lines | host + endpoint nodes |
| flaw (Crystal) | `rule` + `file` | asset node, finding node |
| SARIF | `ruleId` field | finding node |
| generic | `id` field | asset node |

Unknown shapes are silently skipped.

---

## Build and test

```sh
go build -o prowlrview ./cmd/prowlrview
go test -race ./...
go vet ./...
```

CI (`ci.yml`) runs vet + test + build on every push to main, uploads `bin/prowlrview` as an artifact.

`cmd/plugin-check/main.go` validates plugin syntax (reads and reports Lua parse errors without running plugins).

---

## Config paths

| Purpose | Path |
|---------|------|
| CA cert | `~/.config/prowlrview/ca.crt` |
| CA key | `~/.config/prowlrview/ca.key` |
| Plugins (Lua) | `~/.config/prowlrview/plugins/*.lua` |
| Plugins (WASM) | `~/.config/prowlrview/plugins/*.wasm` |
| Themes | `~/.config/prowlrview/themes/*.toml` |
| Plugins repo cache | `~/.cache/prowlrview/plugins-repo/` |
| Sessions | `~/.local/share/prowlrview/sessions/<name>/` |
| Active session symlink | `~/.local/share/prowlrview/sessions/active` |

Override base with `$XDG_CONFIG_HOME` / `$XDG_CACHE_HOME`. Override the plugins repo location with `$PROWLRVIEW_PLUGINS_REPO`.

---

## Security

Report suspected vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

MIT © kdairatchi / ProwlrBot
