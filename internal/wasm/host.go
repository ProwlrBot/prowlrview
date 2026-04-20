package wasm

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// Event is the JSON payload passed to every WASM plugin call.
type Event struct {
	Kind    string         `json:"kind"`    // "request"|"response"|"finding"|"node"|"tick"
	Payload map[string]any `json:"payload"`
}

// Result is the optional JSON the plugin writes back.
type Result struct {
	Block  bool   `json:"block,omitempty"`
	Reason string `json:"reason,omitempty"`
	Notify string `json:"notify,omitempty"`
	Log    string `json:"log,omitempty"`
}

// Plugin wraps one loaded WASM module.
type Plugin struct {
	name   string
	rt     wazero.Runtime
	mod    api.Module
	handle api.Function
	alloc  api.Function
	free   api.Function
}

// Load compiles and instantiates a WASM plugin from path.
func Load(ctx context.Context, path string) (*Plugin, error) {
	code, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	rt := wazero.NewRuntime(ctx)
	cm, err := rt.CompileModule(ctx, code)
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("compile %s: %w", path, err)
	}
	mod, err := rt.InstantiateModule(ctx, cm, wazero.NewModuleConfig().WithName(""))
	if err != nil {
		rt.Close(ctx)
		return nil, fmt.Errorf("instantiate %s: %w", path, err)
	}
	handle := mod.ExportedFunction("prowlrview_handle")
	if handle == nil {
		mod.Close(ctx)
		rt.Close(ctx)
		return nil, fmt.Errorf("%s: missing export prowlrview_handle", path)
	}
	// alloc/free are optional — plugins that don't need memory management skip them
	alloc := mod.ExportedFunction("prowlrview_alloc")
	free := mod.ExportedFunction("prowlrview_free")
	return &Plugin{
		name:   path,
		rt:     rt,
		mod:    mod,
		handle: handle,
		alloc:  alloc,
		free:   free,
	}, nil
}

// Name returns the plugin's path/identifier.
func (p *Plugin) Name() string {
	return p.name
}

// Fire sends an event to the plugin and returns the Result (may be zero value).
func (p *Plugin) Fire(ctx context.Context, event Event) (Result, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return Result{}, err
	}
	mem := p.mod.Memory()
	if mem == nil {
		return Result{}, nil
	}
	// write event JSON into WASM memory
	var ptr uint32
	if p.alloc != nil {
		res, err := p.alloc.Call(ctx, uint64(len(data)))
		if err != nil || len(res) == 0 {
			return Result{}, fmt.Errorf("alloc: %w", err)
		}
		ptr = uint32(res[0])
	} else {
		// fall back: write at offset 64 (first 64 bytes are reserved)
		ptr = 64
	}
	if !mem.Write(ptr, data) {
		return Result{}, fmt.Errorf("write to wasm memory failed")
	}
	// call handler
	results, err := p.handle.Call(ctx, uint64(ptr), uint64(len(data)))
	if err != nil {
		return Result{}, fmt.Errorf("handle: %w", err)
	}
	if p.free != nil && p.alloc != nil {
		_, _ = p.free.Call(ctx, uint64(ptr))
	}
	if len(results) == 0 || results[0] == 0 {
		return Result{}, nil
	}
	// read result from returned pointer
	resultPtr := uint32(results[0])
	// read up to 4096 bytes for the result
	raw := make([]byte, 4096)
	n := uint32(0)
	for i := uint32(0); i < 4096; i++ {
		b, ok := mem.ReadByte(resultPtr + i)
		if !ok || b == 0 {
			n = i
			break
		}
		raw[i] = b
	}
	var r Result
	_ = json.Unmarshal(raw[:n], &r)
	return r, nil
}

// Close releases wazero resources.
func (p *Plugin) Close(ctx context.Context) {
	p.mod.Close(ctx)
	p.rt.Close(ctx)
}
