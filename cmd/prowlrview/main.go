package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/plugin"
	"github.com/ProwlrBot/prowlrview/internal/proxy"
	"github.com/ProwlrBot/prowlrview/internal/runner"
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
	fmt.Println("    prowlrview plugin <cmd> ...      list|enable|disable|enable-all|disable-all|sync|search|update")
	fmt.Println("    prowlrview plugin search <tag>           search plugin registry by tag")
	fmt.Println("    prowlrview plugin update                 pull latest plugins from repo")
	fmt.Println("    prowlrview theme <cmd> ...       list|enable|enable-all")
	fmt.Println("    prowlrview proxy [:port]         MITM proxy → fires plugin on_request/on_response")
	fmt.Println("    prowlrview ca [show|install|export DEST]   show / install / export the MITM CA cert")
	fmt.Println("    prowlrview chrome [URL]          launch isolated Chrome through the proxy")
	fmt.Println("    prowlrview web [:webPort] [:proxyPort]   proxy + beautiful live dashboard")
	fmt.Println("    prowlrview findings [--file SNAP] [--json]  dump scored findings")
	fmt.Println("    prowlrview sync [--file SNAP] [--endpoint URL]  push findings to prowlrbot")
	fmt.Println("    prowlrview snapshot save <name>          save current graph snapshot")
	fmt.Println("    prowlrview snapshot diff <a.jsonl> <b.jsonl>  diff two snapshots")
	fmt.Println("    prowlrview caido-import <export.json>     import Caido session into graph")
	fmt.Println("    prowlrview caido-push <finding-id>        push finding back to Caido")
	fmt.Println("    prowlrview session new <name> [target]   create named session")
	fmt.Println("    prowlrview session list                  list sessions")
	fmt.Println("    prowlrview session switch <name>         activate session")
	fmt.Println("    prowlrview run PIPELINE.yml          execute recon pipeline (feeds graph)")
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
		addr := ":8888"
		if len(os.Args) > 2 {
			addr = os.Args[2]
		}
		if _, err := proxy.EnsureCA(); err != nil {
			die(err)
		}
		if err := ui.RunProxy(addr, ""); err != nil {
			die(err)
		}
	case "web":
		paddr, waddr := ":8888", ":8889"
		if len(os.Args) > 2 {
			waddr = os.Args[2]
		}
		if len(os.Args) > 3 {
			paddr = os.Args[3]
		}
		if _, err := proxy.EnsureCA(); err != nil {
			die(err)
		}
		if err := ui.RunProxy(paddr, waddr); err != nil {
			die(err)
		}
	case "ca":
		runCA(os.Args[2:])
	case "chrome":
		// usage: prowlrview chrome [proxyAddr] [url]
		// accepts args in any order: ":9888", "https://...", or both.
		proxyAddr, url := ":8888", ""
		for _, a := range os.Args[2:] {
			if strings.HasPrefix(a, ":") || strings.Contains(a, "127.0.0.1:") || strings.Contains(a, "localhost:") {
				proxyAddr = a
			} else {
				url = a
			}
		}
		if _, err := proxy.EnsureCA(); err != nil {
			die(err)
		}
		if err := proxy.LaunchChrome(proxyAddr, url); err != nil {
			die(err)
		}
	case "snapshot":
		runSnapshot(os.Args[2:])
	case "findings":
		runFindings(os.Args[2:])
	case "sync":
		runSync(os.Args[2:])
	case "caido-import":
		need(3, "caido-import: missing export.json")
		runCaidoImport(os.Args[2:])
	case "caido-push":
		need(3, "caido-push: missing finding-id")
		runCaidoPush(os.Args[2:])
	case "session":
		runSession(os.Args[2:])
	case "run":
		need(3, "run: missing PIPELINE.yml")
		runPipeline(os.Args[2:])
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

func snapshotDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".local", "share", "prowlrview", "snapshots")
}

func runSnapshot(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "snapshot: missing subcommand (save|diff)")
		os.Exit(2)
	}
	switch args[0] {
	case "save":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "snapshot save: missing NAME")
			os.Exit(2)
		}
		name := args[1]
		matches, _ := filepath.Glob("*.snapshot.jsonl")
		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "no *.snapshot.jsonl in current directory; run prowlrview pipe/watch first and press 'e' to export")
			os.Exit(1)
		}
		src := newestFile(matches)
		dir := snapshotDir()
		if err := os.MkdirAll(dir, 0o755); err != nil {
			die(err)
		}
		dest := filepath.Join(dir, name+".snapshot.jsonl")
		data, err := os.ReadFile(src)
		if err != nil {
			die(err)
		}
		if err := os.WriteFile(dest, data, 0o644); err != nil {
			die(err)
		}
		fmt.Printf("✓ saved %s → %s\n", src, dest)

	case "diff":
		if len(args) < 3 {
			fmt.Fprintln(os.Stderr, "snapshot diff: need <a.jsonl> <b.jsonl>")
			os.Exit(2)
		}
		aPath, bPath := args[1], args[2]
		if !strings.HasSuffix(aPath, ".jsonl") {
			aPath = filepath.Join(snapshotDir(), aPath+".snapshot.jsonl")
		}
		if !strings.HasSuffix(bPath, ".jsonl") {
			bPath = filepath.Join(snapshotDir(), bPath+".snapshot.jsonl")
		}
		ga, err := graph.LoadFromPath(aPath)
		if err != nil {
			die(err)
		}
		gb, err := graph.LoadFromPath(bPath)
		if err != nil {
			die(err)
		}
		diff := graph.Diff(ga, gb)
		fmt.Printf("+ added:   %d\n", len(diff.Added))
		fmt.Printf("~ changed: %d\n", len(diff.Changed))
		fmt.Printf("- removed: %d\n", len(diff.Removed))
		fmt.Println()
		for _, n := range diff.Added {
			fmt.Printf("  [+] %s  %s  (%s)\n", n.Severity, n.Label, n.ID)
		}
		for _, n := range diff.Changed {
			fmt.Printf("  [~] %s  %s  (%s)\n", n.Severity, n.Label, n.ID)
		}
		for _, n := range diff.Removed {
			fmt.Printf("  [-] %s  %s  (%s)\n", n.Severity, n.Label, n.ID)
		}

	default:
		fmt.Fprintln(os.Stderr, "snapshot: unknown subcommand:", args[0])
		os.Exit(2)
	}
}

func newestFile(paths []string) string {
	var newest string
	var newestTime time.Time
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err == nil && fi.ModTime().After(newestTime) {
			newestTime = fi.ModTime()
			newest = p
		}
	}
	return newest
}

func runFindings(args []string) {
	file, jsonOut := "", false
	for i, a := range args {
		if a == "--json" {
			jsonOut = true
		}
		if a == "--file" && i+1 < len(args) {
			file = args[i+1]
		}
	}
	if file == "" {
		matches, _ := filepath.Glob("*.snapshot.jsonl")
		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "no snapshot file found; use --file PATH")
			os.Exit(1)
		}
		var newest string
		var newestTime time.Time
		for _, m := range matches {
			fi, err := os.Stat(m)
			if err == nil && fi.ModTime().After(newestTime) {
				newestTime = fi.ModTime()
				newest = m
			}
		}
		file = newest
	}
	f, err := os.Open(file)
	if err != nil {
		die(err)
	}
	defer f.Close()
	g := graph.New()
	if err := g.Load(f); err != nil {
		die(err)
	}
	findings := g.Findings()
	sort.Slice(findings, func(i, j int) bool {
		return findings[i].Score() > findings[j].Score()
	})
	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(findings)
		return
	}
	fmt.Println("| Score | Severity | Label | Source | URL |")
	fmt.Println("|-------|----------|-------|--------|-----|")
	for _, n := range findings {
		conf := n.Confidence
		if conf <= 0 {
			conf = 1.0
		}
		fmt.Printf("| %.2f | %s | %s | %s | %s |\n",
			n.Score(), n.Severity, n.Label, n.Source, n.ID)
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

	case "search":
		q := ""
		if len(args) > 1 {
			q = args[1]
		}
		results, err := plugin.SearchManifest(q)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(results) == 0 {
			fmt.Println("  no plugins matched:", q)
			return
		}
		for _, e := range results {
			fmt.Printf("  %-24s [%s]  %s\n", e.Name, e.Category, e.Description)
			fmt.Printf("    tags: %s\n", strings.Join(e.Tags, ", "))
		}

	case "update", "upgrade":
		fmt.Println("pulling plugins repo...")
		if err := plugin.UpdateRepo(); err != nil {
			fmt.Fprintln(os.Stderr, "update failed:", err)
			os.Exit(1)
		}
		fmt.Println("✓ plugins repo updated")
		// re-scan and report
		entries, _ := plugin.Scan()
		var enabled int
		for _, e := range entries {
			if e.Enabled {
				enabled++
			}
		}
		fmt.Printf("  %d plugins available, %d enabled\n", len(entries), enabled)

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
	fmt.Fprintf(os.Stderr, "  tip: run `prowlrview plugin search %s` to find it\n", name)
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

func runCA(args []string) {
	cmd := "show"
	if len(args) > 0 {
		cmd = args[0]
	}
	switch cmd {
	case "show", "path":
		p, err := proxy.EnsureCA()
		if err != nil {
			die(err)
		}
		fmt.Println(p)
	case "install", "info":
		p, err := proxy.EnsureCA()
		if err != nil {
			die(err)
		}
		fmt.Print(proxy.Instructions(p))
	case "export":
		dest := ""
		if len(args) > 1 {
			dest = args[1]
		} else {
			dest = "win:Downloads"
		}
		out, err := proxy.ExportTo(dest)
		if err != nil {
			die(err)
		}
		fmt.Println("✓ exported CA to", out)
		fmt.Println("  Windows: double-click → Install Certificate → Local Machine → Trusted Root")
	default:
		fmt.Fprintln(os.Stderr, "ca: unknown subcommand:", cmd)
		fmt.Fprintln(os.Stderr, "usage: prowlrview ca [show|install|export DEST]")
		os.Exit(2)
	}
}

func runPipeline(args []string) {
	file := args[0]
	p, err := runner.Load(file)
	if err != nil {
		die(err)
	}
	fmt.Printf("▶ pipeline: %s  target: %s  stages: %d\n", p.Name, p.Target, len(p.Stages))
	g := graph.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := runner.Run(ctx, p, g, func(s string) { fmt.Println(s) }); err != nil {
		fmt.Fprintln(os.Stderr, "run:", err)
	}
	// print summary
	fmt.Printf("\n✓ graph: %d nodes, %d findings\n", g.Len(), len(g.Findings()))
}

func runSync(args []string) {
	var file, endpoint string
	for i, a := range args {
		switch a {
		case "--file":
			if i+1 < len(args) {
				file = args[i+1]
			}
		case "--endpoint":
			if i+1 < len(args) {
				endpoint = args[i+1]
			}
		}
	}
	if endpoint == "" {
		endpoint = os.Getenv("PROWLRBOT_ENDPOINT")
	}
	if endpoint == "" {
		endpoint = "http://127.0.0.1:7171/api/findings"
	}
	if file == "" {
		matches, _ := filepath.Glob("*.snapshot.jsonl")
		if len(matches) == 0 {
			fmt.Fprintln(os.Stderr, "sync: no snapshot found; use --file PATH")
			os.Exit(1)
		}
		file = newestFile(matches)
	}
	g, err := graph.LoadFromPath(file)
	if err != nil {
		die(err)
	}

	findings := g.Findings()
	if len(findings) == 0 {
		fmt.Println("sync: no findings to push")
		return
	}

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, f := range findings {
		_ = enc.Encode(f)
	}

	req, err := http.NewRequest("POST", endpoint, &buf)
	if err != nil {
		die(err)
	}
	req.Header.Set("Content-Type", "application/x-ndjson")
	req.Header.Set("X-Prowlrbot-Attribution", "h1-anom5x")
	req.Header.Set("X-Prowlrbot-Source", "prowlrview")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "sync: POST failed: %v\n", err)
		fmt.Printf("  (attempted: %s)\n", endpoint)
		fmt.Printf("  findings serialized: %d\n", len(findings))
		fmt.Println("  tip: set PROWLRBOT_ENDPOINT or pass --endpoint")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("✓ synced %d findings → %s (%d)\n", len(findings), endpoint, resp.StatusCode)
	} else {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "sync: server returned %d: %s\n", resp.StatusCode, string(body))
		os.Exit(1)
	}
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
