# prowlrview

> k9s for bug bounty — proxy + live graph + plugin host, one binary.

**Status:** v0.1 scaffold. Not ready for use yet.

## What it is

prowlrview is a terminal-first hunting cockpit that unifies three things bounty hunters juggle across separate tools:

1. **MITM proxy** (mitmproxy/Caido-style) — intercept, rewrite, replay
2. **Live attack-surface graph** — nodes/edges from any JSONL/SARIF-emitting tool
3. **Plugin host** — Lua (hot-reload) + WASM (sandboxed) for request rewriters, passive scanners, graph decorators

Everything streams through one TUI. One binary. Any CLI that emits JSONL plugs in.

## Modes

```
prowlrview proxy              # MITM on :8080, TUI flow view
prowlrview pipe               # stdin JSONL/SARIF → live graph
prowlrview watch DIR          # tail results dir, build living graph
prowlrview run recon.yml      # orchestrate recon pipeline into graph
```

## Why not mitmproxy / Caido / BloodHound?

| Tool       | Proxy | Graph | TUI | Plugins    | CLI sync |
|------------|-------|-------|-----|------------|----------|
| mitmproxy  | ✅    | ❌    | ok  | Python     | ❌       |
| Caido      | ✅    | ~     | GUI | JS         | ❌       |
| BloodHound | ❌    | ✅    | ❌  | ❌         | ❌       |
| k9s        | ❌    | ❌    | ✅  | Lua        | k8s only |
| prowlrview | ✅    | ✅    | ✅  | Lua + WASM | any JSONL |

## Adapters (planned)

SARIF · nuclei JSONL · httpx JSON · katana JSONL · subfinder · dalfox · flaw · gau · waybackurls · custom.

## Plugin API (Lua sketch)

```lua
-- plugins/idor-hunter.lua
on_request(function(req)
  if req.path:match("/api/.*/%d+") then
    graph:tag(req.host, "idor-candidate", "orange")
  end
end)

on_finding(function(f)
  if f.rule == "sqli" and f.severity == "high" then
    notify("🔴 SQLi: " .. f.url)
  end
end)
```

## Themes

TOML themes in `~/.config/prowlrview/themes/`. Ships with: `prowlr`, `paper`, `mono`, `terminal`, `solarized`.

## Roadmap

- [x] v0.1 — scaffold + tview UI + JSONL adapter + themes
- [ ] v0.2 — mitm proxy mode (goproxy)
- [ ] v0.3 — Lua plugin host (gopher-lua)
- [ ] v0.4 — WASM plugins (wazero) + registry
- [ ] v0.5 — Obsidian canvas export + recon orchestrator
- [ ] v1.0 — website, docs, demo

## Build

```sh
go build -o prowlrview ./cmd/prowlrview
```

## Security

Report suspected vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## License

MIT © kdairatchi / ProwlrBot
