# WASM Plugin ABI

Export `prowlrview_handle(event_ptr i32, event_len i32) -> result_ptr i32`.

The host writes JSON-encoded `Event` at `event_ptr`. Your plugin reads it, acts, optionally writes a JSON-encoded `Result` somewhere in memory and returns its pointer (or 0 for no result).

## TinyGo example

```go
//go:build tinygo

package main

import (
    "encoding/json"
    "unsafe"
)

//export prowlrview_handle
func handle(ptr, length uint32) uint32 {
    data := unsafe.Slice((*byte)(unsafe.Pointer(uintptr(ptr))), length)
    var evt map[string]any
    json.Unmarshal(data, &evt)
    // example: log every finding
    if evt["kind"] == "finding" {
        result := `{"log":"wasm: finding seen"}` + "\x00"
        buf := []byte(result)
        return uint32(uintptr(unsafe.Pointer(&buf[0])))
    }
    return 0
}

func main() {}
```

Build: `tinygo build -o myplugin.wasm -target wasi ./myplugin.go`
Drop `myplugin.wasm` in `~/.config/prowlrview/plugins/`.
