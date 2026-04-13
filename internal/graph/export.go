package graph

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Dot writes Graphviz DOT.
func (g *Graph) Dot(w io.Writer) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	fmt.Fprintln(w, "digraph prowlrview {")
	fmt.Fprintln(w, `  rankdir=LR; node [shape=box style=filled fontname="monospace"];`)
	for id, n := range g.nodes {
		fmt.Fprintf(w, "  %q [label=%q fillcolor=%q];\n",
			id, KindIcon(n.Kind)+" "+n.Label, sevFillColor(n.Severity))
	}
	for id, n := range g.nodes {
		if n.Parent != "" {
			fmt.Fprintf(w, "  %q -> %q;\n", n.Parent, id)
		}
	}
	fmt.Fprintln(w, "}")
	return nil
}

// Mermaid writes a Mermaid flowchart — good for dropping into Obsidian notes.
func (g *Graph) Mermaid(w io.Writer) error {
	g.mu.RLock()
	defer g.mu.RUnlock()
	fmt.Fprintln(w, "flowchart LR")
	ids := map[string]string{}
	i := 0
	safe := func(id string) string {
		if s, ok := ids[id]; ok {
			return s
		}
		i++
		s := fmt.Sprintf("N%d", i)
		ids[id] = s
		return s
	}
	for id, n := range g.nodes {
		label := strings.ReplaceAll(n.Label, `"`, `'`)
		fmt.Fprintf(w, "  %s[\"%s %s\"]\n", safe(id), KindIcon(n.Kind), label)
	}
	for id, n := range g.nodes {
		if n.Parent != "" {
			fmt.Fprintf(w, "  %s --> %s\n", safe(n.Parent), safe(id))
		}
	}
	return nil
}

// ObsidianCanvas writes Obsidian's .canvas JSON format so a hunt graph drops
// straight into the vault.
func (g *Graph) ObsidianCanvas(w io.Writer) error {
	g.mu.RLock()
	defer g.mu.RUnlock()

	type cnode struct {
		ID     string `json:"id"`
		Type   string `json:"type"`
		Text   string `json:"text"`
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
		Color  string `json:"color,omitempty"`
	}
	type cedge struct {
		ID      string `json:"id"`
		FromNd  string `json:"fromNode"`
		FromSide string `json:"fromSide"`
		ToNd    string `json:"toNode"`
		ToSide  string `json:"toSide"`
	}
	type canvas struct {
		Nodes []cnode `json:"nodes"`
		Edges []cedge `json:"edges"`
	}

	c := canvas{}
	x, y := 0, 0
	i := 0
	for id, n := range g.nodes {
		c.Nodes = append(c.Nodes, cnode{
			ID: id, Type: "text",
			Text:  fmt.Sprintf("%s **%s**\n`%s` · %s", KindIcon(n.Kind), n.Label, n.Kind, n.Severity),
			X:     x, Y: y, Width: 280, Height: 80,
			Color: canvasColor(n.Severity),
		})
		i++
		x += 320
		if i%5 == 0 {
			x = 0
			y += 120
		}
	}
	ei := 0
	for id, n := range g.nodes {
		if n.Parent != "" {
			ei++
			c.Edges = append(c.Edges, cedge{
				ID: fmt.Sprintf("e%d", ei), FromNd: n.Parent, FromSide: "right",
				ToNd: id, ToSide: "left",
			})
		}
	}
	return json.NewEncoder(w).Encode(c)
}

func sevFillColor(s Severity) string {
	return [...]string{"#e0e0e0", "#b6e3a1", "#fff59d", "#ffb74d", "#ef5350"}[s]
}

func canvasColor(s Severity) string {
	// Obsidian canvas supports "1"-"6" presets.
	return [...]string{"", "4", "3", "2", "1"}[s]
}
