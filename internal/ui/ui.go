// Package ui renders the prowlrview TUI.
package ui

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/adapter"
	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/plugin"
	"github.com/ProwlrBot/prowlrview/internal/proxy"
	"github.com/ProwlrBot/prowlrview/internal/session"
	"github.com/ProwlrBot/prowlrview/internal/theme"
	"github.com/fsnotify/fsnotify"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type SortMode int

const (
	SortBySeverity SortMode = iota
	SortByRecency
	SortByAlpha
)

func (s SortMode) String() string {
	return [...]string{"severity", "recent", "alpha"}[s]
}

type app struct {
	tv        *tview.Application
	g         *graph.Graph
	theme     *theme.Theme
	themes    map[string]*theme.Theme
	names     []string
	tree      *tview.TreeView
	findTbl   *tview.Table
	flowTbl   *tview.Table
	detail    *tview.TextView
	status    *tview.TextView
	logView   *tview.TextView
	filterI   *tview.InputField
	root      *tview.Flex
	pages     *tview.Pages
	graphPane *graphView
	showGraph bool

	plugin *plugin.Host
	store  *proxy.FlowStore

	mu     sync.Mutex
	filter string
	sort   SortMode
	follow bool
}

// RunPipe reads JSONL from r and renders live.
func RunPipe(r io.Reader) error {
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.plugin.Watch(ctx)
	go a.ingestReader(r)
	go a.refreshLoop()
	go a.tickLoop()
	defer a.plugin.Close()
	return a.tv.Run()
}

// RunWatch tails *.jsonl / *.json / *.sarif in dir.
func RunWatch(dir string) error {
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.plugin.Watch(ctx)
	go a.ingestDir(ctx, dir)
	go a.refreshLoop()
	go a.tickLoop()
	defer a.plugin.Close()
	return a.tv.Run()
}

// RunProxy starts the MITM proxy on addr and renders flows live in the TUI.
// If webAddr != "", also serves the HTML dashboard on that port.
func RunProxy(addr, webAddr string) error {
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.plugin.Watch(ctx)
	a.store = proxy.NewFlowStore(2000)
	a.attachFlowPane()
	// Direct write — app not yet running, QueueUpdateDraw would deadlock
	c := colorTag(a.theme.Accent)
	a.status.SetText(fmt.Sprintf(" %sprowlrview[-] · proxy on %s · point browser at 127.0.0.1%s · ? for keys", c, addr, addr))
	if webAddr != "" {
		go func() {
			if err := proxy.ServeWeb(webAddr, a.g, a.store, func(s string) { a.logf("%s", s) }); err != nil {
				a.logf("web: %v", err)
			}
		}()
		a.logf("web dashboard: http://127.0.0.1%s", webAddr)
	}
	go func() {
		err := proxy.Run(addr, a.g, a.plugin, a.store, func(s string) { a.logf("%s", s) })
		if err != nil {
			a.logf("proxy: %v", err)
		}
	}()
	go a.refreshLoop()
	go a.tickLoop()
	defer a.plugin.Close()
	return a.tv.Run()
}

// RunReplay loads a snapshot file and renders (no live updates).
func RunReplay(path string) error {
	a := newApp()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.plugin.Watch(ctx)
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := a.g.Load(f); err != nil {
		return err
	}
	go a.refreshLoop()
	defer a.plugin.Close()
	return a.tv.Run()
}

func newApp() *app {
	themes := theme.Builtin()
	_ = theme.LoadUserThemes(theme.UserThemeDir(), themes)

	names := []string{"prowlr", "cyberpunk", "dracula", "nightshade", "solarized"}
	for k := range themes {
		if !containsStr(names, k) {
			names = append(names, k)
		}
	}

	a := &app{
		tv:     tview.NewApplication(),
		g:      graph.New(),
		theme:  themes["prowlr"],
		themes: themes,
		names:  names,
		follow: true,
		sort:   SortBySeverity,
	}

	// load active session snapshot if present
	if name := session.Active(); name != "" {
		snapPath := session.SnapshotPath(name)
		if f, err := os.Open(snapPath); err == nil {
			_ = a.g.Load(f)
			f.Close()
		}
	}

	a.buildUI()

	a.plugin = plugin.NewHost(a.g, func(s string) { a.logf("%s", s) }, a.notify)
	if err := a.plugin.LoadDir(plugin.UserPluginDir()); err != nil {
		a.logf("plugin dir: %v", err)
	}
	a.g.OnUpsert = a.plugin.NodeObserver()
	return a
}

func (a *app) buildUI() {
	t := a.theme
	tview.Styles.PrimitiveBackgroundColor = t.Background
	tview.Styles.PrimaryTextColor = t.Foreground
	tview.Styles.BorderColor = t.Border
	tview.Styles.TitleColor = t.Title

	a.tree = tview.NewTreeView()
	a.tree.SetBorder(true).SetTitle(" ⌘ surface ").SetBorderColor(t.Border).SetTitleColor(t.Title)
	a.tree.SetRoot(tview.NewTreeNode("prowlrview").SetColor(t.Accent)).SetCurrentNode(nil)

	a.findTbl = tview.NewTable().SetBorders(false).SetSelectable(true, false)
	a.findTbl.SetBorder(true).SetTitle(" ⚠ findings ").SetBorderColor(t.Border).SetTitleColor(t.Title)

	a.detail = tview.NewTextView().SetDynamicColors(true).SetWrap(true)
	a.detail.SetBorder(true).SetTitle(" ℹ detail ").SetBorderColor(t.Border).SetTitleColor(t.Title)

	a.logView = tview.NewTextView().SetDynamicColors(true).SetMaxLines(500).SetScrollable(true)
	a.logView.SetBorder(true).SetTitle(" ▸ log ").SetBorderColor(t.Border).SetTitleColor(t.Title)

	a.filterI = tview.NewInputField().SetLabel(" / ").SetFieldWidth(0)
	a.filterI.SetFieldBackgroundColor(t.Background).SetLabelColor(t.Accent).SetFieldTextColor(t.Foreground)
	a.filterI.SetChangedFunc(func(s string) {
		a.mu.Lock()
		a.filter = strings.ToLower(s)
		a.mu.Unlock()
		a.refresh()
	})
	a.filterI.SetDoneFunc(func(tcell.Key) { a.tv.SetFocus(a.tree) })

	a.status = tview.NewTextView().SetDynamicColors(true)
	a.setStatus("ready · theme=" + t.Name + " · ? for keys")

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.tree, 0, 2, true).
		AddItem(a.findTbl, 0, 1, false)

	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.detail, 0, 2, false).
		AddItem(a.logView, 0, 1, false)

	main := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(left, 0, 1, true).
		AddItem(right, 0, 2, false)

	a.root = tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.banner(), 6, 0, false).
		AddItem(main, 0, 1, true).
		AddItem(a.filterI, 1, 0, false).
		AddItem(a.status, 1, 0, false)

	a.pages = tview.NewPages().AddPage("main", a.root, true, true)

	a.graphPane = newGraphView(a.g, t)
	graphLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.graphPane, 0, 1, true).
		AddItem(a.status, 1, 0, false)
	a.pages.AddPage("graph", graphLayout, true, false)

	a.tv.SetRoot(a.pages, true).EnableMouse(true)

	a.bindKeys()
	a.bindSelections()
}

func (a *app) banner() *tview.TextView {
	tv := tview.NewTextView().SetDynamicColors(true).SetTextAlign(tview.AlignCenter)
	c := colorTag(a.theme.Title)
	ac := colorTag(a.theme.Accent)
	fmt.Fprintln(tv, c+"  ██████  ██████   ██████  ██     ██ ██      ██████  ██    ██ ██ ███████ ██     ██[-]")
	fmt.Fprintln(tv, c+"  ██   ██ ██   ██ ██    ██ ██     ██ ██      ██   ██ ██    ██ ██ ██      ██     ██[-]")
	fmt.Fprintln(tv, c+"  ██████  ██████  ██    ██ ██  █  ██ ██      ██████  ██    ██ ██ █████   ██  █  ██[-]")
	fmt.Fprintln(tv, c+"  ██      ██   ██ ██    ██ ██ ███ ██ ██      ██   ██  ██  ██  ██ ██      ██ ███ ██[-]")
	fmt.Fprintln(tv, c+"  ██      ██   ██  ██████   ███ ███  ███████ ██   ██   ████   ██ ███████  ███ ███[-]")
	fmt.Fprintf(tv, "%s  k9s for bug bounty · live graph · proxy · plugins[-]\n", ac)
	return tv
}

func (a *app) bindKeys() {
	a.tv.SetInputCapture(func(e *tcell.EventKey) *tcell.EventKey {
		if a.tv.GetFocus() == a.filterI {
			return e
		}
		if e.Key() == tcell.KeyCtrlC {
			a.tv.Stop()
			return nil
		}
		switch e.Rune() {
		case 'q':
			a.tv.Stop()
			return nil
		case 't':
			a.cycleTheme()
			return nil
		case 'f':
			a.follow = !a.follow
			a.setStatus(fmt.Sprintf("follow=%v", a.follow))
			return nil
		case 's':
			a.sort = SortMode((int(a.sort) + 1) % 3)
			a.setStatus("sort: " + a.sort.String())
			a.refresh()
			return nil
		case '/':
			a.tv.SetFocus(a.filterI)
			return nil
		case 'e':
			a.exportMenu()
			return nil
		case 'r':
			a.plugin.Reload()
			a.refresh()
			return nil
		case '?':
			a.showHelp()
			return nil
		case 'g':
			a.showGraph = !a.showGraph
			if a.showGraph {
				a.setStatus("graph view · g to toggle back")
				a.pages.ShowPage("graph")
				a.pages.HidePage("main")
				a.tv.SetFocus(a.graphPane)
			} else {
				a.pages.ShowPage("main")
				a.pages.HidePage("graph")
				a.tv.SetFocus(a.tree)
			}
			return nil
		}
		return e
	})
}

func (a *app) bindSelections() {
	a.findTbl.SetSelectedFunc(func(row, _ int) {
		if ref, ok := a.findTbl.GetCell(row, 0).GetReference().(string); ok {
			if n, ok := a.g.Get(ref); ok {
				a.showDetail(n)
			}
		}
	})
	a.tree.SetChangedFunc(func(node *tview.TreeNode) {
		if id, ok := node.GetReference().(string); ok {
			if n, ok := a.g.Get(id); ok {
				a.showDetail(n)
			}
		}
	})
}

func (a *app) showDetail(n *graph.Node) {
	sev := colorTag(a.sevColor(n.Severity))
	ac := colorTag(a.theme.Accent)
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s %s[-]  %s%s[-]\n", sev, n.Severity.Icon(), n.Severity.String(), ac, graph.KindIcon(n.Kind))
	fmt.Fprintf(&b, "[::b]%s[-]\n\n", tview.Escape(n.Label))
	fmt.Fprintf(&b, "kind:    %s\n", n.Kind)
	fmt.Fprintf(&b, "source:  %s\n", n.Source)
	fmt.Fprintf(&b, "hits:    %d\n", n.Hits)
	fmt.Fprintf(&b, "seen:    %s\n", n.SeenAt.Format(time.RFC3339))
	if len(n.Tags) > 0 {
		fmt.Fprintf(&b, "tags:    %s\n", strings.Join(n.Tags, ", "))
	}
	if len(n.Detail) > 0 {
		b.WriteString("\n[::u]detail[-]\n")
		keys := make([]string, 0, len(n.Detail))
		for k := range n.Detail {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "  %-10s %s\n", k+":", tview.Escape(n.Detail[k]))
		}
	}
	a.detail.SetText(b.String())
}

func (a *app) showHelp() {
	a.detail.SetText(`[::b]keys[-]
  q           quit
  t           cycle theme
  f           toggle follow
  s           cycle sort (severity → recent → alpha)
  /           focus filter  (esc/enter returns)
  e           export menu (dot / mermaid / canvas / snapshot)
  g           toggle graph view (DAG visualization)
  r           refresh
  ?           this help

[::b]pipes[-]
  nuclei -jsonl -l hosts.txt | prowlrview pipe
  httpx -json -l hosts.txt   | prowlrview pipe
  flaw scan --json ./src     | prowlrview pipe

[::b]plugins[-] ~/.config/prowlrview/plugins/*.lua
[::b]themes[-]  ~/.config/prowlrview/themes/*.toml
`)
}

func (a *app) exportMenu() {
	ts := time.Now().Format("20060102-150405")
	modal := tview.NewModal().
		SetText("export graph as:").
		AddButtons([]string{"DOT", "Mermaid", "Obsidian Canvas", "Snapshot JSONL", "cancel"}).
		SetDoneFunc(func(_ int, label string) {
			a.pages.RemovePage("export")
			a.tv.SetFocus(a.tree)
			var path string
			var err error
			switch label {
			case "DOT":
				path = "prowlrview-" + ts + ".dot"
				err = writeFile(path, func(w io.Writer) error { return a.g.Dot(w) })
			case "Mermaid":
				path = "prowlrview-" + ts + ".mmd"
				err = writeFile(path, func(w io.Writer) error { return a.g.Mermaid(w) })
			case "Obsidian Canvas":
				path = "prowlrview-" + ts + ".canvas"
				err = writeFile(path, func(w io.Writer) error { return a.g.ObsidianCanvas(w) })
			case "Snapshot JSONL":
				path = "prowlrview-" + ts + ".snapshot.jsonl"
				err = a.g.Save(path)
			default:
				return
			}
			if err != nil {
				a.logf("export failed: %v", err)
				return
			}
			a.notify("exported → " + path)
		})
	a.pages.AddPage("export", modal, true, true)
}

func (a *app) cycleTheme() {
	cur := a.theme.Name
	next := a.names[0]
	for i, n := range a.names {
		if n == cur {
			next = a.names[(i+1)%len(a.names)]
			break
		}
	}
	a.theme = a.themes[next]
	a.buildUI()
	a.refresh()
	a.setStatus("theme: " + next)
}

func (a *app) refreshLoop() {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		a.tv.QueueUpdateDraw(a.refresh)
	}
}

func (a *app) tickLoop() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	for range tick.C {
		a.plugin.Fire("on_tick", map[string]any{
			"now":            time.Now().Unix(),
			"node_count":     a.g.Len(),
			"finding_count":  len(a.g.Findings()),
		})
	}
}

func (a *app) refresh() {
	t := a.theme
	a.mu.Lock()
	filter := a.filter
	mode := a.sort
	a.mu.Unlock()

	root := tview.NewTreeNode(fmt.Sprintf("prowlrview · %d nodes", a.g.Len())).SetColor(t.Accent)
	for _, n := range a.g.Roots() {
		if tn := a.buildTreeNode(n, 0, filter); tn != nil {
			root.AddChild(tn)
		}
	}
	a.tree.SetRoot(root)

	a.findTbl.Clear()
	headers := []string{"sev", "kind", "source", "finding"}
	for i, h := range headers {
		a.findTbl.SetCell(0, i, tview.NewTableCell("[::b]"+h).SetSelectable(false).SetTextColor(t.Accent))
	}
	findings := a.g.Findings()
	findings = applySort(findings, mode)
	row := 1
	for _, n := range findings {
		if filter != "" && !strings.Contains(strings.ToLower(n.Label), filter) {
			continue
		}
		a.findTbl.SetCell(row, 0, tview.NewTableCell(n.Severity.Icon()+" "+n.Severity.String()).
			SetTextColor(a.sevColor(n.Severity)).SetReference(n.ID))
		a.findTbl.SetCell(row, 1, tview.NewTableCell(graph.KindIcon(n.Kind)))
		a.findTbl.SetCell(row, 2, tview.NewTableCell(n.Source))
		a.findTbl.SetCell(row, 3, tview.NewTableCell(truncate(n.Label, 80)))
		row++
	}
	if a.follow && a.findTbl.GetRowCount() > 1 {
		a.findTbl.Select(1, 0)
	}

	if a.flowTbl != nil && a.store != nil {
		a.refreshFlows(filter)
	}
}

func (a *app) attachFlowPane() {
	t := a.theme
	a.flowTbl = tview.NewTable().SetBorders(false).SetSelectable(true, false).SetFixed(1, 0)
	a.flowTbl.SetBorder(true).SetTitle(" ⇌ flows ").SetBorderColor(t.Border).SetTitleColor(t.Title)

	// swap right column: detail(2) · flows(2) · log(1)
	right := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.detail, 0, 2, false).
		AddItem(a.flowTbl, 0, 2, false).
		AddItem(a.logView, 0, 1, false)

	left := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(a.tree, 0, 2, true).
		AddItem(a.findTbl, 0, 1, false)

	main := tview.NewFlex().SetDirection(tview.FlexColumn).
		AddItem(left, 0, 1, true).
		AddItem(right, 0, 2, false)

	a.root.Clear().
		AddItem(a.banner(), 6, 0, false).
		AddItem(main, 0, 1, true).
		AddItem(a.filterI, 1, 0, false).
		AddItem(a.status, 1, 0, false)
}

func (a *app) refreshFlows(filter string) {
	flows := a.store.Snapshot()
	a.flowTbl.Clear()
	headers := []string{"time", "method", "status", "dur", "host", "path"}
	for i, h := range headers {
		a.flowTbl.SetCell(0, i, tview.NewTableCell("[::b]"+h).SetSelectable(false).SetTextColor(a.theme.Accent))
	}
	// newest first
	row := 1
	for i := len(flows) - 1; i >= 0; i-- {
		f := flows[i]
		if filter != "" && !strings.Contains(strings.ToLower(f.Host+f.Path+f.Method), filter) {
			continue
		}
		a.flowTbl.SetCell(row, 0, tview.NewTableCell(f.Time.Format("15:04:05")))
		a.flowTbl.SetCell(row, 1, tview.NewTableCell(f.Method).SetTextColor(methodColor(f.Method)))
		statusStr := fmt.Sprintf("%d", f.Status)
		if f.Blocked {
			statusStr = "BLK"
		}
		a.flowTbl.SetCell(row, 2, tview.NewTableCell(statusStr).SetTextColor(statusColor(f.Status, f.Blocked)))
		a.flowTbl.SetCell(row, 3, tview.NewTableCell(fmt.Sprintf("%dms", f.DurMs)))
		a.flowTbl.SetCell(row, 4, tview.NewTableCell(truncate(f.Host, 30)))
		a.flowTbl.SetCell(row, 5, tview.NewTableCell(truncate(f.Path, 60)))
		row++
		if row > 400 {
			break
		}
	}
	if a.follow && row > 1 {
		a.flowTbl.Select(1, 0)
	}
}

func methodColor(m string) tcell.Color {
	switch m {
	case "GET":
		return tcell.ColorLightGreen
	case "POST":
		return tcell.ColorGold
	case "PUT", "PATCH":
		return tcell.ColorOrange
	case "DELETE":
		return tcell.ColorRed
	case "HEAD", "OPTIONS":
		return tcell.ColorGray
	default:
		return tcell.ColorWhite
	}
}

func statusColor(s int, blocked bool) tcell.Color {
	if blocked {
		return tcell.ColorRed
	}
	switch {
	case s >= 500:
		return tcell.ColorRed
	case s >= 400:
		return tcell.ColorGold
	case s >= 300:
		return tcell.ColorAqua
	case s >= 200:
		return tcell.ColorLightGreen
	default:
		return tcell.ColorGray
	}
}

func applySort(ns []*graph.Node, m SortMode) []*graph.Node {
	out := make([]*graph.Node, len(ns))
	copy(out, ns)
	switch m {
	case SortByRecency:
		sort.Slice(out, func(i, j int) bool { return out[i].SeenAt.After(out[j].SeenAt) })
	case SortByAlpha:
		sort.Slice(out, func(i, j int) bool { return out[i].Label < out[j].Label })
	default:
		sort.Slice(out, func(i, j int) bool {
			if out[i].Severity != out[j].Severity {
				return out[i].Severity > out[j].Severity
			}
			return out[i].SeenAt.After(out[j].SeenAt)
		})
	}
	return out
}

func (a *app) buildTreeNode(n *graph.Node, depth int, filter string) *tview.TreeNode {
	if depth > 6 {
		return tview.NewTreeNode("…")
	}
	label := fmt.Sprintf("%s %s %s", graph.KindIcon(n.Kind), n.Severity.Icon(), n.Label)
	if n.Hits > 1 {
		label += fmt.Sprintf(" (%d)", n.Hits)
	}
	tn := tview.NewTreeNode(label).SetReference(n.ID).SetColor(a.sevColor(n.Severity))
	anyChild := false
	for _, c := range a.g.Children(n.ID) {
		if sub := a.buildTreeNode(c, depth+1, filter); sub != nil {
			tn.AddChild(sub)
			anyChild = true
		}
	}
	if filter != "" && !strings.Contains(strings.ToLower(n.Label), filter) && !anyChild {
		return nil
	}
	return tn
}

func (a *app) sevColor(s graph.Severity) tcell.Color {
	switch s {
	case graph.SevCritical:
		return a.theme.SevCritical
	case graph.SevHigh:
		return a.theme.SevHigh
	case graph.SevMedium:
		return a.theme.SevMedium
	case graph.SevLow:
		return a.theme.SevLow
	default:
		return a.theme.SevInfo
	}
}

func (a *app) setStatus(s string) {
	c := colorTag(a.theme.Accent)
	text := fmt.Sprintf(" %sprowlrview[-] · %s", c, s)
	a.tv.QueueUpdateDraw(func() {
		a.status.SetText(text)
	})
}

func (a *app) notify(s string) {
	a.setStatus(s)
	a.logf("notify: %s", s)
}

func (a *app) logf(format string, args ...any) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("[gray]%s[-] %s\n", ts, fmt.Sprintf(format, args...))
	a.tv.QueueUpdateDraw(func() {
		fmt.Fprint(a.logView, line)
	})
}

func (a *app) ingestReader(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		adapter.ParseLine(sc.Bytes(), a.g)
	}
	if err := sc.Err(); err != nil {
		a.logf("ingest: %v", err)
	}
	a.logf("ingest: eof")
}

func (a *app) ingestDir(ctx context.Context, dir string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		a.logf("watch: fsnotify: %v", err)
		return
	}
	defer watcher.Close()
	if err := watcher.Add(dir); err != nil {
		a.logf("watch: add %s: %v", dir, err)
		return
	}
	a.logf("watch: inotify on %s", dir)
	offsets := map[string]int64{}

	ingestFile := func(path string) {
		info, err := os.Stat(path)
		if err != nil || info.Size() <= offsets[path] {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		if _, err := f.Seek(offsets[path], io.SeekStart); err != nil {
			return
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
		for sc.Scan() {
			adapter.ParseLine(sc.Bytes(), a.g)
		}
		offsets[path] = info.Size()
	}

	// do an initial sweep of existing files
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".jsonl") || strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".sarif") {
			ingestFile(filepath.Join(dir, n))
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			n := event.Name
			if !(strings.HasSuffix(n, ".jsonl") || strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".sarif")) {
				continue
			}
			if event.Op&fsnotify.Write != 0 || event.Op&fsnotify.Create != 0 {
				ingestFile(n)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			a.logf("watch: %v", err)
		}
	}
}

func colorTag(c tcell.Color) string {
	r, g, b := c.RGB()
	return fmt.Sprintf("[#%02x%02x%02x]", r, g, b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func writeFile(path string, fn func(io.Writer) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return fn(f)
}

func containsStr(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
