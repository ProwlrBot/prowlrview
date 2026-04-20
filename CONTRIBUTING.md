# Contributing to prowlrview

Thanks for your interest. prowlrview is the core TUI/proxy/graph engine. For **plugins**, see the companion repo: [prowlrview-plugins](https://github.com/ProwlrBot/prowlrview-plugins).

## Scope of this repo

- Core TUI (tview)
- Adapters for tool output (nuclei, httpx, subfinder, katana, flaw, dalfox, gau, waybackurls, SARIF, …)
- Graph data structure
- MITM proxy engine
- Lua + WASM plugin host
- Recon pipeline runner
- Session management
- Caido import/export bridge
- Theme engine

## Scope of the plugins repo

- Per-vuln-class detectors (IDOR, SSRF, JWT, CORS, XSS, …)
- Passive scanners
- Graph decorators
- Scope guards
- Community themes

If in doubt about where something belongs: if it has no business logic and only wires Lua/WASM into events, it goes in core. If it IS business logic, it goes in plugins.

## Dev setup

```sh
git clone https://github.com/ProwlrBot/prowlrview
cd prowlrview
go build -o bin/prowlrview ./cmd/prowlrview
go test ./...
```

## Commit style

Short, imperative, lowercase prefix: `ui:`, `adapter:`, `graph:`, `plugin:`, `proxy:`, `runner:`, `session:`, `caido:`, `wasm:`, `docs:`.

## Adding an adapter

1. Add detection in `internal/adapter/adapter.go` `detect()`.
2. Add a `fromFoo` parser. Use `fromURL()` shared helper for tools that emit bare URL strings.
3. Add a test case to `internal/adapter/adapter_test.go`.

Adapters currently supported: nuclei, httpx, subfinder, katana, flaw, dalfox, gau, waybackurls, SARIF, generic `{id, label}` fallback.

## Style

No AI slop. No filler comments. Tests over prose.
