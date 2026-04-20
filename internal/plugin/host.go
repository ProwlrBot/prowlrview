// Package plugin hosts Lua scripts that respond to graph/request/response events.
// Each plugin runs in its own lua.LState with an instruction-count cap so a
// runaway loop can't freeze the TUI.
package plugin

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/wasm"
	"github.com/fsnotify/fsnotify"
	lua "github.com/yuin/gopher-lua"
	"golang.org/x/time/rate"
)

const maxInstructions = 1_000_000 // per event callback

type Host struct {
	mu          sync.Mutex
	plugins     []*plugin
	wasmPlugins []*wasm.Plugin
	dir         string
	g           *graph.Graph
	log         func(string)
	notify      func(string)
}

type plugin struct {
	name    string
	path    string
	mu      sync.Mutex // serializes access to L; a single LState is not goroutine-safe
	L       *lua.LState
	limiter *rate.Limiter
}

func NewHost(g *graph.Graph, logFn, notifyFn func(string)) *Host {
	if logFn == nil {
		logFn = func(string) {}
	}
	if notifyFn == nil {
		notifyFn = func(string) {}
	}
	return &Host{g: g, log: logFn, notify: notifyFn}
}

// LoadDir loads every *.lua file in dir.
func (h *Host) LoadDir(dir string) error {
	h.dir = dir
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lua") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if err := h.LoadFile(p); err != nil {
			h.log(fmt.Sprintf("plugin load failed: %s: %v", e.Name(), err))
		}
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".wasm") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		wp, err := wasm.Load(context.Background(), p)
		if err != nil {
			h.log(fmt.Sprintf("wasm plugin load failed: %s: %v", e.Name(), err))
			continue
		}
		h.mu.Lock()
		h.wasmPlugins = append(h.wasmPlugins, wp)
		h.mu.Unlock()
		h.log("wasm plugin loaded: " + e.Name())
	}
	return nil
}

func (h *Host) LoadFile(path string) error {
	L := lua.NewState(lua.Options{SkipOpenLibs: true})
	for _, pair := range []struct {
		name string
		fn   lua.LGFunction
	}{
		{lua.BaseLibName, lua.OpenBase},
		{lua.TabLibName, lua.OpenTable},
		{lua.StringLibName, lua.OpenString},
		{lua.MathLibName, lua.OpenMath},
	} {
		if err := L.CallByParam(lua.P{Fn: L.NewFunction(pair.fn), NRet: 0, Protect: true}, lua.LString(pair.name)); err != nil {
			L.Close()
			return err
		}
	}
	registerTypes(L)
	h.injectAPI(L)

	// Build config table from sidecar plugin.toml before running the plugin so
	// top-level code can read `config` on load.
	configTbl := L.NewTable()
	tomlPath := filepath.Join(filepath.Dir(path), "plugin.toml")

	type rateConfig struct {
		RPM   int `toml:"rpm"`
		Burst int `toml:"burst"`
	}
	var pluginLimiter *rate.Limiter

	if raw, err := os.ReadFile(tomlPath); err == nil {
		var doc map[string]any
		if _, err := toml.Decode(string(raw), &doc); err == nil {
			if cfg, ok := doc["config"].(map[string]any); ok {
				for k, v := range cfg {
					switch x := v.(type) {
					case string:
						L.SetField(configTbl, k, lua.LString(x))
					case int64:
						L.SetField(configTbl, k, lua.LNumber(float64(x)))
					case float64:
						L.SetField(configTbl, k, lua.LNumber(x))
					case bool:
						L.SetField(configTbl, k, lua.LBool(x))
					}
				}
			}
			// Parse [rate] section for per-plugin rate limiting.
			var rc rateConfig
			if _, err := toml.Decode(string(raw), &rc); err == nil {
				if rc.RPM > 0 {
					burst := rc.Burst
					if burst <= 0 {
						burst = 1
					}
					pluginLimiter = rate.NewLimiter(rate.Every(time.Minute/time.Duration(rc.RPM)), burst)
				}
			}
		}
	}
	L.SetGlobal("config", configTbl)

	if err := L.DoFile(path); err != nil {
		L.Close()
		return err
	}
	h.mu.Lock()
	h.plugins = append(h.plugins, &plugin{name: filepath.Base(path), path: path, L: L, limiter: pluginLimiter})
	h.mu.Unlock()
	h.log("plugin loaded: " + filepath.Base(path))
	return nil
}

// Close shuts down all plugin states.
func (h *Host) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.plugins {
		p.L.Close()
	}
	h.plugins = nil
	for _, wp := range h.wasmPlugins {
		wp.Close(context.Background())
	}
	h.wasmPlugins = nil
}

// reloadFile closes and removes the named plugin then reloads it from disk.
func (h *Host) reloadFile(path string) {
	name := filepath.Base(path)
	h.mu.Lock()
	for i, p := range h.plugins {
		if p.name == name {
			p.L.Close()
			h.plugins = append(h.plugins[:i], h.plugins[i+1:]...)
			break
		}
	}
	h.mu.Unlock()
	if err := h.LoadFile(path); err != nil {
		h.log("hot-reload failed: " + name + ": " + err.Error())
	} else {
		h.log("hot-reload: " + name)
	}
}

// Reload closes all plugins and reloads them from the stored dir. Used by the
// manual `r` key binding so an explicit refresh always picks up edited files.
func (h *Host) Reload() {
	if h.dir == "" {
		return
	}
	h.mu.Lock()
	for _, p := range h.plugins {
		p.L.Close()
	}
	h.plugins = nil
	h.mu.Unlock()
	_ = h.LoadDir(h.dir)
	h.notify("plugins reloaded")
}

// Watch watches the plugin dir with fsnotify and reloads any *.lua file that
// is written or created. Blocks until ctx is cancelled.
func (h *Host) Watch(ctx context.Context) {
	if h.dir == "" {
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		h.log("fsnotify: " + err.Error())
		return
	}
	defer watcher.Close()
	if err := watcher.Add(h.dir); err != nil {
		h.log("fsnotify watch: " + err.Error())
		return
	}
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if strings.HasSuffix(event.Name, ".lua") &&
				(event.Op&fsnotify.Write != 0 || event.Op&fsnotify.Create != 0) {
				h.reloadFile(event.Name)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			h.log("fsnotify error: " + err.Error())
		}
	}
}

// Fire calls the given event callback on every plugin that registered it.
// payload is copied into a Lua table.
func (h *Host) Fire(event string, payload map[string]any) {
	for _, p := range h.snapshot() {
		p.mu.Lock()
		cb := p.L.GetGlobal("__pv_" + event)
		if cb.Type() != lua.LTFunction {
			p.mu.Unlock()
			continue
		}
		p.L.SetMx(maxInstructions / 1000)
		tbl := toLuaTable(p.L, payload)
		if err := p.L.CallByParam(lua.P{Fn: cb, NRet: 0, Protect: true}, tbl); err != nil {
			h.log(fmt.Sprintf("%s:%s error: %v", p.name, event, err))
		}
		p.mu.Unlock()
	}
	evt := wasm.Event{Kind: event, Payload: payload}
	for _, wp := range h.wasmSnapshot() {
		r, err := wp.Fire(context.Background(), evt)
		if err != nil {
			h.log(fmt.Sprintf("wasm:%s error: %v", wp.Name(), err))
			continue
		}
		if r.Notify != "" {
			h.notify(r.Notify)
		}
		if r.Log != "" {
			h.log(r.Log)
		}
	}
}

// snapshot returns the current plugin slice under the host lock so callers can
// iterate without holding it during long Lua executions.
func (h *Host) snapshot() []*plugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*plugin, len(h.plugins))
	copy(out, h.plugins)
	return out
}

// wasmSnapshot returns the current WASM plugin slice under the host lock.
func (h *Host) wasmSnapshot() []*wasm.Plugin {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*wasm.Plugin, len(h.wasmPlugins))
	copy(out, h.wasmPlugins)
	return out
}

// FireRequest dispatches an HTTP request through every plugin's on_request
// callback. Returns the (possibly mutated) Request struct so callers can
// inspect Blocked / Reason and use the modified r.R.
func (h *Host) FireRequest(r *http.Request) *Request {
	var out *Request
	for _, p := range h.snapshot() {
		p.mu.Lock()
		cb := p.L.GetGlobal("__pv_on_request")
		if cb.Type() != lua.LTFunction {
			p.mu.Unlock()
			continue
		}
		if p.limiter != nil && !p.limiter.Allow() {
			p.mu.Unlock()
			continue // skip this plugin for this request, don't block the proxy
		}
		p.L.SetMx(maxInstructions / 1000)
		tbl, req := pushRequest(p.L, r)
		if out == nil {
			out = req
		}
		err := p.L.CallByParam(lua.P{Fn: cb, NRet: 0, Protect: true}, tbl)
		p.mu.Unlock()
		if err != nil {
			h.log(fmt.Sprintf("%s:on_request error: %v", p.name, err))
			continue
		}
		if req.Blocked {
			out = req
			return out
		}
	}
	if out == nil {
		out = &Request{R: r}
	}
	return out
}

// FireResponse dispatches an HTTP response through every plugin's on_response.
func (h *Host) FireResponse(resp *http.Response) *Response {
	var out *Response
	for _, p := range h.snapshot() {
		p.mu.Lock()
		cb := p.L.GetGlobal("__pv_on_response")
		if cb.Type() != lua.LTFunction {
			p.mu.Unlock()
			continue
		}
		p.L.SetMx(maxInstructions / 1000)
		tbl, rs := pushResponse(p.L, resp)
		if out == nil {
			out = rs
		}
		err := p.L.CallByParam(lua.P{Fn: cb, NRet: 0, Protect: true}, tbl)
		p.mu.Unlock()
		if err != nil {
			h.log(fmt.Sprintf("%s:on_response error: %v", p.name, err))
		}
	}
	if out == nil {
		out = &Response{R: resp}
	}
	return out
}

// NodeObserver returns a func suitable for graph.Graph.OnUpsert. It fires
// on_node for every upserted node and additionally on_finding for findings.
func (h *Host) NodeObserver() func(*graph.Node) {
	return func(n *graph.Node) {
		detail := map[string]any{}
		for k, v := range n.Detail {
			detail[k] = v
		}
		payload := map[string]any{
			"id":         n.ID,
			"kind":       n.Kind,
			"label":      n.Label,
			"parent":     n.Parent,
			"severity":   n.Severity.String(),
			"source":     n.Source,
			"confidence": n.Confidence,
			"detail":     detail,
		}
		if n.Kind == "finding" {
			h.Fire("on_finding", payload)
		}
		h.Fire("on_node", payload)
	}
}

func (h *Host) injectAPI(L *lua.LState) {
	L.SetGlobal("plugin", L.NewTable())
	register := func(event string) lua.LGFunction {
		return func(L *lua.LState) int {
			fn := L.CheckFunction(1)
			L.SetGlobal("__pv_"+event, fn)
			return 0
		}
	}
	L.SetGlobal("on_request", L.NewFunction(register("on_request")))
	L.SetGlobal("on_response", L.NewFunction(register("on_response")))
	L.SetGlobal("on_finding", L.NewFunction(register("on_finding")))
	L.SetGlobal("on_node", L.NewFunction(register("on_node")))
	L.SetGlobal("on_tick", L.NewFunction(register("on_tick")))

	L.SetGlobal("notify", L.NewFunction(func(L *lua.LState) int {
		h.notify(L.CheckString(1))
		return 0
	}))
	L.SetGlobal("log", L.NewFunction(func(L *lua.LState) int {
		h.log(L.CheckString(1))
		return 0
	}))

	graphTbl := L.NewTable()
	// shift if called with colon syntax (graph:tag → first arg is the table)
	shift := func(L *lua.LState) int {
		if L.Get(1).Type() == lua.LTTable {
			return 1
		}
		return 0
	}
	L.SetField(graphTbl, "tag", L.NewFunction(func(L *lua.LState) int {
		s := shift(L)
		id := L.CheckString(1 + s)
		tag := L.CheckString(2 + s)
		if n, ok := h.g.Get(id); ok {
			h.g.Upsert(graph.Node{ID: n.ID, Kind: n.Kind, Label: n.Label, Parent: n.Parent,
				Severity: n.Severity, Source: n.Source, Tags: []string{tag}})
		}
		return 0
	}))
	L.SetField(graphTbl, "upsert", L.NewFunction(func(L *lua.LState) int {
		s := shift(L)
		conf := float64(L.OptNumber(6+s, 0))
		h.g.Upsert(graph.Node{
			ID:         L.CheckString(1 + s),
			Kind:       L.CheckString(2 + s),
			Label:      L.CheckString(3 + s),
			Parent:     L.OptString(4+s, ""),
			Severity:   graph.ParseSeverity(L.OptString(5+s, "info")),
			Confidence: conf,
			Source:     "plugin",
		})
		return 0
	}))
	L.SetField(graphTbl, "raise", L.NewFunction(func(L *lua.LState) int {
		s := shift(L)
		id := L.CheckString(1 + s)
		sev := graph.ParseSeverity(L.CheckString(2 + s))
		if n, ok := h.g.Get(id); ok {
			h.g.Upsert(graph.Node{ID: n.ID, Kind: n.Kind, Label: n.Label, Parent: n.Parent,
				Severity: sev, Source: n.Source})
		}
		return 0
	}))

	// graph:nodes() → array table of node tables
	L.SetField(graphTbl, "nodes", L.NewFunction(func(L *lua.LState) int {
		nodes := h.g.Nodes()
		arr := L.NewTable()
		for i, n := range nodes {
			tbl := nodeToLua(L, n)
			arr.RawSetInt(i+1, tbl)
		}
		L.Push(arr)
		return 1
	}))

	// graph:findings() → array table of finding node tables
	L.SetField(graphTbl, "findings", L.NewFunction(func(L *lua.LState) int {
		findings := h.g.Findings()
		arr := L.NewTable()
		for i, n := range findings {
			tbl := nodeToLua(L, n)
			arr.RawSetInt(i+1, tbl)
		}
		L.Push(arr)
		return 1
	}))

	// graph:children(parent_id) → array table
	L.SetField(graphTbl, "children", L.NewFunction(func(L *lua.LState) int {
		s := shift(L)
		parentID := L.CheckString(1 + s)
		children := h.g.Children(parentID)
		arr := L.NewTable()
		for i, n := range children {
			tbl := nodeToLua(L, n)
			arr.RawSetInt(i+1, tbl)
		}
		L.Push(arr)
		return 1
	}))

	L.SetGlobal("graph", graphTbl)

	httpTbl := L.NewTable()

	L.SetField(httpTbl, "get", L.NewFunction(func(L *lua.LState) int {
		urlStr := L.CheckString(1)
		var headersArg *lua.LTable
		if L.GetTop() >= 2 {
			headersArg = L.CheckTable(2)
		}
		req, err := http.NewRequest("GET", urlStr, nil)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		if headersArg != nil {
			headersArg.ForEach(func(k, v lua.LValue) {
				req.Header.Set(k.String(), v.String())
			})
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		tbl := L.NewTable()
		L.SetField(tbl, "status", lua.LNumber(resp.StatusCode))
		L.SetField(tbl, "body", lua.LString(body))
		hdrs := L.NewTable()
		for k, v := range resp.Header {
			if len(v) > 0 {
				L.SetField(hdrs, strings.ToLower(k), lua.LString(v[0]))
			}
		}
		L.SetField(tbl, "headers", hdrs)
		L.Push(tbl)
		return 1
	}))

	L.SetField(httpTbl, "post", L.NewFunction(func(L *lua.LState) int {
		urlStr := L.CheckString(1)
		bodyStr := L.OptString(2, "")
		var headersArg *lua.LTable
		if L.GetTop() >= 3 {
			headersArg = L.CheckTable(3)
		}
		req, err := http.NewRequest("POST", urlStr, strings.NewReader(bodyStr))
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		if headersArg != nil {
			headersArg.ForEach(func(k, v lua.LValue) {
				req.Header.Set(k.String(), v.String())
			})
		}
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		tbl := L.NewTable()
		L.SetField(tbl, "status", lua.LNumber(resp.StatusCode))
		L.SetField(tbl, "body", lua.LString(body))
		L.Push(tbl)
		return 1
	}))

	L.SetGlobal("http", httpTbl)

	fsTbl := L.NewTable()
	L.SetField(fsTbl, "write", L.NewFunction(func(L *lua.LState) int {
		rawPath := L.CheckString(1)
		content := L.CheckString(2)

		// reject traversal
		if strings.Contains(rawPath, "..") {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("path traversal rejected"))
			return 2
		}
		// expand ~ to home dir
		home, _ := os.UserHomeDir()
		absPath := rawPath
		if strings.HasPrefix(rawPath, "~/") {
			absPath = filepath.Join(home, rawPath[2:])
		} else if !filepath.IsAbs(rawPath) {
			// relative paths not allowed
			L.Push(lua.LBool(false))
			L.Push(lua.LString("use absolute path or ~/..."))
			return 2
		}
		// must be under home
		if !strings.HasPrefix(absPath, home) {
			L.Push(lua.LBool(false))
			L.Push(lua.LString("write outside home dir rejected"))
			return 2
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		if err := os.WriteFile(absPath, []byte(content), 0o644); err != nil {
			L.Push(lua.LBool(false))
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LBool(true))
		return 1
	}))

	// fs.read(path) → content string or nil, err
	L.SetField(fsTbl, "read", L.NewFunction(func(L *lua.LState) int {
		rawPath := L.CheckString(1)
		if strings.Contains(rawPath, "..") {
			L.Push(lua.LNil)
			L.Push(lua.LString("path traversal rejected"))
			return 2
		}
		home, _ := os.UserHomeDir()
		absPath := rawPath
		if strings.HasPrefix(rawPath, "~/") {
			absPath = filepath.Join(home, rawPath[2:])
		}
		if !strings.HasPrefix(absPath, home) {
			L.Push(lua.LNil)
			L.Push(lua.LString("read outside home dir rejected"))
			return 2
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			L.Push(lua.LNil)
			L.Push(lua.LString(err.Error()))
			return 2
		}
		L.Push(lua.LString(data))
		return 1
	}))

	L.SetGlobal("fs", fsTbl)
}

func nodeToLua(L *lua.LState, n *graph.Node) *lua.LTable {
	tbl := L.NewTable()
	L.SetField(tbl, "id", lua.LString(n.ID))
	L.SetField(tbl, "kind", lua.LString(n.Kind))
	L.SetField(tbl, "label", lua.LString(n.Label))
	L.SetField(tbl, "parent", lua.LString(n.Parent))
	L.SetField(tbl, "severity", lua.LString(n.Severity.String()))
	L.SetField(tbl, "source", lua.LString(n.Source))
	L.SetField(tbl, "confidence", lua.LNumber(n.Confidence))
	detail := L.NewTable()
	for k, v := range n.Detail {
		L.SetField(detail, k, lua.LString(v))
	}
	L.SetField(tbl, "detail", detail)
	tags := L.NewTable()
	for i, t := range n.Tags {
		tags.RawSetInt(i+1, lua.LString(t))
	}
	L.SetField(tbl, "tags", tags)
	return tbl
}

func toLuaTable(L *lua.LState, m map[string]any) *lua.LTable {
	t := L.NewTable()
	for k, v := range m {
		switch x := v.(type) {
		case string:
			L.SetField(t, k, lua.LString(x))
		case int:
			L.SetField(t, k, lua.LNumber(x))
		case float64:
			L.SetField(t, k, lua.LNumber(x))
		case bool:
			L.SetField(t, k, lua.LBool(x))
		case map[string]any:
			L.SetField(t, k, toLuaTable(L, x))
		}
	}
	return t
}

// UserPluginDir returns $XDG_CONFIG_HOME/prowlrview/plugins (or ~/.config/...).
func UserPluginDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "prowlrview", "plugins")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "prowlrview", "plugins")
}
