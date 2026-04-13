package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/ProwlrBot/prowlrview/internal/plugin"
	"github.com/ProwlrBot/prowlrview/internal/ui"
)

const version = "0.1.0-dev"

const banner = `
  ██████  ██████   ██████  ██     ██ ██      ██████  ██    ██ ██ ███████ ██     ██
  ██   ██ ██   ██ ██    ██ ██     ██ ██      ██   ██ ██    ██ ██ ██      ██     ██
  ██████  ██████  ██    ██ ██  █  ██ ██      ██████  ██    ██ ██ █████   ██  █  ██
  ██      ██   ██ ██    ██ ██ ███ ██ ██      ██   ██  ██  ██  ██ ██      ██ ███ ██
  ██      ██   ██  ██████   ███ ███  ███████ ██   ██   ████   ██ ███████  ███ ███
`

func usage() {
	fmt.Print(banner)
	fmt.Printf("  prowlrview %s — k9s for bug bounty\n\n", version)
	fmt.Println("  USAGE:")
	fmt.Println("    prowlrview init                  create config dirs + enable all plugins/themes")
	fmt.Println("    prowlrview pipe                  stdin JSONL/SARIF → live graph")
	fmt.Println("    prowlrview watch DIR             tail results dir")
	fmt.Println("    prowlrview replay SNAP.jsonl     replay a saved graph snapshot")
	fmt.Println("    prowlrview plugin <cmd> ...      list|enable|disable|enable-all|disable-all|sync")
	fmt.Println("    prowlrview theme <cmd> ...       list|enable|enable-all")
	fmt.Println("    prowlrview proxy [:port]         MITM proxy (planned)")
	fmt.Println("    prowlrview version")
	fmt.Println()
	fmt.Println("  KEYS (in TUI):")
	fmt.Println("    /    fuzzy filter     j/k  navigate     enter  expand node")
	fmt.Println("    f    follow live      e    export       t      cycle theme")
	fmt.Println("    s    sort by severity q    quit         ?      help")
	fmt.Println()
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(0)
	}

	switch os.Args[1] {
	case "pipe":
		fatal(ui.RunPipe(os.Stdin))
	case "watch":
		need(3, "watch: missing DIR")
		fatal(ui.RunWatch(os.Args[2]))
	case "replay":
		need(3, "replay: missing SNAPSHOT.jsonl")
		fatal(ui.RunReplay(os.Args[2]))
	case "init":
		runInit()
	case "plugin":
		runPlugin("plugin", os.Args[2:])
	case "theme":
		runPlugin("theme", os.Args[2:])
	case "proxy":
		fmt.Fprintln(os.Stderr, "proxy mode: planned for v0.2 (goproxy-based MITM)")
		os.Exit(2)
	case "version", "-v", "--version":
		fmt.Println("prowlrview", version)
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func runInit() {
	if err := os.MkdirAll(plugin.UserPluginDir(), 0o755); err != nil {
		die(err)
	}
	if err := os.MkdirAll(plugin.ThemeDir(), 0o755); err != nil {
		die(err)
	}
	entries, err := plugin.Scan()
	if err != nil {
		die(err)
	}
	n := 0
	for _, e := range entries {
		if err := plugin.Install(e); err != nil {
			fmt.Fprintln(os.Stderr, "skip", e.Name, "—", err)
			continue
		}
		n++
	}
	fmt.Printf("✓ enabled %d items (plugins: %s, themes: %s)\n", n, plugin.UserPluginDir(), plugin.ThemeDir())
	fmt.Println("  run: prowlrview plugin list")
}

func runPlugin(kind string, args []string) {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list", "ls":
		entries := scanOrDie(kind)
		plugin.PrintList(os.Stdout, entries)
		fmt.Println()
	case "enable", "install":
		need(3, kind+" enable: missing NAME (e.g. idor-hunter)")
		toggle(kind, args[1], true)
	case "disable", "uninstall", "remove":
		need(3, kind+" disable: missing NAME")
		toggle(kind, args[1], false)
	case "enable-all", "install-all":
		toggleAll(kind, true)
	case "disable-all":
		toggleAll(kind, false)
	case "sync":
		// re-clone/pull the plugins repo then enable anything already enabled
		repo, err := plugin.RepoPath()
		if err != nil {
			die(err)
		}
		fmt.Println("repo:", repo)
		fmt.Println("(run `git -C " + repo + " pull` to update)")
	default:
		fmt.Fprintln(os.Stderr, "unknown", kind, "command:", args[0])
		os.Exit(2)
	}
}

func scanOrDie(kind string) []plugin.Entry {
	entries, err := plugin.Scan()
	if err != nil {
		die(err)
	}
	filtered := entries[:0]
	for _, e := range entries {
		if (kind == "plugin" && e.Kind == "plugin") || (kind == "theme" && e.Kind == "theme") {
			filtered = append(filtered, e)
		}
	}
	return filtered
}

func toggle(kind, name string, enable bool) {
	entries := scanOrDie(kind)
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".lua"), ".toml")
	for _, e := range entries {
		if e.Name == name {
			var err error
			if enable {
				err = plugin.Install(e)
			} else {
				err = plugin.Uninstall(e)
			}
			if err != nil {
				die(err)
			}
			verb := "enabled"
			if !enable {
				verb = "disabled"
			}
			fmt.Printf("✓ %s %s (%s)\n", verb, e.Name, e.Target)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "not found:", name)
	os.Exit(1)
}

func toggleAll(kind string, enable bool) {
	entries := scanOrDie(kind)
	n := 0
	for _, e := range entries {
		var err error
		if enable {
			err = plugin.Install(e)
		} else {
			err = plugin.Uninstall(e)
		}
		if err == nil {
			n++
		}
	}
	verb := "enabled"
	if !enable {
		verb = "disabled"
	}
	fmt.Printf("✓ %s %d %ss\n", verb, n, kind)
}

func need(n int, msg string) {
	if len(os.Args) < n {
		fmt.Fprintln(os.Stderr, msg)
		os.Exit(2)
	}
}

func fatal(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
