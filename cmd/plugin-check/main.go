package main

import (
	"fmt"
	"os"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/plugin"
)

func main() {
	g := graph.New()
	h := plugin.NewHost(g,
		func(s string) { fmt.Fprintln(os.Stdout, "LOG:", s) },
		func(s string) { fmt.Fprintln(os.Stdout, "NOTIFY:", s) },
	)
	defer h.Close()
	dir := plugin.UserPluginDir()
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	if err := h.LoadDir(dir); err != nil {
		fmt.Fprintln(os.Stderr, "ERR:", err)
		os.Exit(1)
	}
	fmt.Println("ok — all plugins loaded from", dir)
}
