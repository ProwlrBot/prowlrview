# prowlrview roadmap

---

## v0.1 — Scaffold

**Status:** complete

What shipped:
- `internal/ui` — tview/tcell TUI, theme cycling, 5 built-in themes + user TOML themes, pane layout (tree / findings / detail / log / flow)
- `internal/graph` — in-memory DAG node/edge store; `graph.go` + `export.go` (DOT, Mermaid, Obsidian Canvas, JSONL snapshot) + `snapshot.go` (load + `Diff`)
- `internal/proxy` — goproxy MITM with auto-generated CA, CONNECT MITM, per-flow graph nodes, web dashboard (`/api/flows`, `/api/graph`, `/api/stats`, `/events` SSE), CA install/export including WSL→Windows path
- `internal/plugin` — gopher-lua host (`host.go`), full req/resp userdata objects (`objects.go`), load/install/manifest scaffolding
- `internal/adapter` — JSONL/SARIF auto-detect; nuclei, httpx, subfinder, katana, flaw, gau, waybackurls, dalfox field mapping
- `cmd/prowlrview/main.go` — subcommand router: proxy, pipe, watch, replay, web, chrome, ca, plugin, theme, version, findings, snapshot, run, sync, caido-import, caido-push, session
- `plugins/PLUGIN_API.md` — full event contract
- `prowlrview-plugins` repo — 12 plugins, each with `plugin.toml` + `main.lua`; CI test harness; `manifest.json` index
- 5 TOML themes in `themes/`
- `plugin-check` binary — CI Lua syntax + manifest schema validator

---

## v0.2 — Plugin Engine Complete

**Status:** complete

What shipped:
- Proxy traffic flows through Lua plugins live: `host.FireRequest` / `host.FireResponse` called from `mitm.go` on every intercepted request/response
- Lua globals registered: `graph:upsert`, `graph:tag`, `graph:raise`, `notify`, `log`; bound to live graph and TUI status bar
- `on_node` event wired: fires when any node is added/updated via `graph.OnUpsert` observer
- Per-plugin TOML config: host reads `plugin.toml` `[config]` section, exposes as `config` global table in Lua
- Hot-reload: `fsnotify` watches plugin directory; `SIGHUP` or `r` key reloads changed `.lua` files without dropping proxy connections
- All 12 plugins loadable and event-dispatching against live HTTP traffic
- Integration test fixtures added in `plugin/testdata/`

---

## v0.3 — Active Scan Layer

**Status:** complete

What shipped:
- `idor-hunter`: numeric-ID + UUID/GUID path detection; optional adjacent-ID probing via `http.get` (opt-in via `config.probe`); response diff; deduplication
- `sqli-heuristic`: error-string oracle + time-delta baseline + reflection check; findings carry `confidence` float
- `ssrf-probe`: protocol-relative `//` detection; POST form-encoded + JSON body scanning; 19 suspicious param names; OOB callback URL support
- Per-plugin rate limiter from `plugin.toml`: `[rate] rpm = 30 burst = 5`; `golang.org/x/time/rate` token-bucket, enforced in host before `on_request` dispatch for active-scan category plugins
- `http.get(url)` / `http.post(url, body, ct)` Lua globals — rate-limited, scope-checked
- `graph:upsert` accepts optional 6th `confidence` (0.0–1.0) arg
- Finding severity scoring: `Node.Score()` = `(severity+1) * confidence`; TUI finding pane sortable by score
- `prowlrview findings` CLI subcommand: dump scored findings as JSON or Markdown table

---

## v0.4 — Graph Intelligence

**Status:** complete

What shipped:
- `internal/ui/graphview.go`: hierarchical force-layout graph rendered in TUI using BFS depth-map; nodes colored by severity; `─`/`│`/`╰`/`╭` edge lines; `g` key to toggle graph/tree view
- `graph:nodes()` / `graph:findings()` / `graph:children(id)` Lua globals for graph traversal
- `chain-detector` production-ready: label+id+detail.rule matching; 8 chain rules; `on_tick` sweep via `graph:findings()`
- `scope-guard` production-ready: reads `config.scope` (comma-separated wildcard patterns) from `plugin.toml`
- `obsidian-exporter` production-ready: `fs.write(path, content)` / `fs.read(path)` Lua globals (sandboxed to `~/`); writes daily note + per-finding Obsidian notes
- `graph-decorator` extended: 14 tech stacks; `/.git` → high severity; `/swagger`, `/graphql` flagged
- Graph snapshot/diff: `prowlrview snapshot save <name>` and `prowlrview snapshot diff <a> <b>`
- Session pre-load: `newApp()` loads last saved snapshot on startup if present

---

## v0.5 — Recon Integration

**Status:** complete

What shipped:
- Adapters for subfinder, httpx, nuclei, katana, dalfox, gau, waybackurls fully implemented; each maps tool-specific JSON fields to canonical graph node schema
- `prowlrview watch DIR` inotify-based via `fsnotify` — watches for new `*.jsonl`/`*.json`/`*.sarif` files and streams into graph as tools write them; no polling
- `internal/runner/runner.go`: `Pipeline`/`Stage` YAML loader, `Run()` with sequential and parallel stages, stdout tee→graph + optional output file, `{{target}}` templating
- `prowlrview run recon.yml` wired to runner; `examples/recon.yml` ships as sample 3-stage pipeline
- katana crawl output feeds graph edges: URL→URL directed edges with HTTP method and status annotations
- `prowlrview sync` pushes finding nodes to prowlrbot findings DB via local socket or HTTP with `h1-anom5x` attribution header
- `takeover-hunter` and `waf-fingerprint` hook `on_node` to probe subdomain nodes from subfinder adapter

---

## v1.0 — Production

**Status:** complete

What shipped:
- `internal/wasm/host.go`: wazero WASM runtime; `prowlrview_handle(event_ptr, event_len) → result_ptr` ABI; JSON event/result schema in `plugins/wasm/schema.json`; example plugin docs in `plugins/wasm/README.md`
- `internal/caido/caido.go`: `Import(path, g)` ingests Caido export JSON; `Push(endpoint, findingID, g)` sends finding to Caido API
- `internal/session/session.go`: named sessions in `~/.local/share/prowlrview/sessions/<name>/`; `prowlrview session new|list|switch|active|delete`; symlink-based active session; isolated graph + plugin state per session
- Web UI: `/events` SSE endpoint (added in v0.1 scaffold) drives live graph + findings updates in browser dashboard
- Plugin registry CLI: `prowlrview plugin search <tag>`, `prowlrview plugin install <name>`, `prowlrview plugin update`; resolves from `manifest.json` in prowlrview-plugins repo
- `prowlrview-plugins` CI: standalone gopher-lua harness in `cmd/test-harness/main.go`; mocks graph/http/fs APIs; asserts `expect.json` per plugin; runs on push/PR via `.github/workflows/plugin-ci.yml`
- Lua `fs.write` / `fs.read` sandboxed to `~/` — obsidian-exporter and other graph plugins can do real file I/O
