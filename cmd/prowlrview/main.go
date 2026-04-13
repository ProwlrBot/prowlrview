package main

import (
	"fmt"
	"os"

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
	fmt.Println("    prowlrview pipe              stdin JSONL/SARIF → live graph")
	fmt.Println("    prowlrview watch DIR         tail results dir")
	fmt.Println("    prowlrview replay SNAP.jsonl replay a saved graph snapshot")
	fmt.Println("    prowlrview proxy [:port]     MITM proxy (planned)")
	fmt.Println("    prowlrview run FILE.yml      orchestrate recon (planned)")
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
		if err := ui.RunPipe(os.Stdin); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "watch":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "watch: missing DIR")
			os.Exit(2)
		}
		if err := ui.RunWatch(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "replay":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "replay: missing SNAPSHOT.jsonl")
			os.Exit(2)
		}
		if err := ui.RunReplay(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	case "proxy":
		fmt.Fprintln(os.Stderr, "proxy mode: planned for v0.2 (goproxy-based MITM)")
		os.Exit(2)
	case "run":
		fmt.Fprintln(os.Stderr, "run mode: planned for v0.5 (recon orchestrator)")
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
