// Package plugin hosts Lua scripts that respond to graph/request/response events.
// Each plugin runs in its own lua.LState with an instruction-count cap so a
// runaway loop can't freeze the TUI.
package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	lua "github.com/yuin/gopher-lua"
)

const maxInstructions = 1_000_000 // per event callback

type Host struct {
	mu      sync.Mutex
	plugins []*plugin
	g       *graph.Graph
	log     func(string)
	notify  func(string)
}

type plugin struct {
	name string
	path string
	L    *lua.LState
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
	h.injectAPI(L)
	if err := L.DoFile(path); err != nil {
		L.Close()
		return err
	}
	h.mu.Lock()
	h.plugins = append(h.plugins, &plugin{name: filepath.Base(path), path: path, L: L})
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
}

// Fire calls the given event callback on every plugin that registered it.
// payload is copied into a Lua table.
func (h *Host) Fire(event string, payload map[string]any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, p := range h.plugins {
		cb := p.L.GetGlobal("__pv_" + event)
		if cb.Type() != lua.LTFunction {
			continue
		}
		p.L.SetMx(maxInstructions / 1000)
		tbl := toLuaTable(p.L, payload)
		if err := p.L.CallByParam(lua.P{Fn: cb, NRet: 0, Protect: true}, tbl); err != nil {
			h.log(fmt.Sprintf("%s:%s error: %v", p.name, event, err))
		}
	}
}

func (h *Host) injectAPI(L *lua.LState) {
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
	L.SetField(graphTbl, "tag", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		tag := L.CheckString(2)
		if n, ok := h.g.Get(id); ok {
			h.g.Upsert(graph.Node{ID: n.ID, Kind: n.Kind, Label: n.Label, Parent: n.Parent,
				Severity: n.Severity, Source: n.Source, Tags: []string{tag}})
		}
		return 0
	}))
	L.SetField(graphTbl, "upsert", L.NewFunction(func(L *lua.LState) int {
		h.g.Upsert(graph.Node{
			ID:       L.CheckString(1),
			Kind:     L.CheckString(2),
			Label:    L.CheckString(3),
			Parent:   L.OptString(4, ""),
			Severity: graph.ParseSeverity(L.OptString(5, "info")),
			Source:   "plugin",
		})
		return 0
	}))
	L.SetField(graphTbl, "raise", L.NewFunction(func(L *lua.LState) int {
		id := L.CheckString(1)
		sev := graph.ParseSeverity(L.CheckString(2))
		if n, ok := h.g.Get(id); ok {
			h.g.Upsert(graph.Node{ID: n.ID, Kind: n.Kind, Label: n.Label, Parent: n.Parent,
				Severity: sev, Source: n.Source})
		}
		return 0
	}))
	L.SetGlobal("graph", graphTbl)
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
