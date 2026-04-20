package graph

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
)

// Save writes one JSON object per node (JSONL) — replay-friendly + streamable.
func (g *Graph) Save(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	g.mu.RLock()
	defer g.mu.RUnlock()
	bw := bufio.NewWriter(f)
	enc := json.NewEncoder(bw)
	for _, n := range g.nodes {
		if err := enc.Encode(n); err != nil {
			return err
		}
	}
	return bw.Flush()
}

// Load replays a snapshot into g.
func (g *Graph) Load(r io.Reader) error {
	dec := json.NewDecoder(r)
	for dec.More() {
		var n Node
		if err := dec.Decode(&n); err != nil {
			return err
		}
		g.Upsert(n)
	}
	return nil
}

// DiffResult describes what changed between two graphs.
type DiffResult struct {
	Added   []*Node
	Removed []*Node
	Changed []*Node // severity promoted or label changed
}

// Diff compares two graphs and returns added/removed/changed nodes.
func Diff(before, after *Graph) DiffResult {
	before.mu.RLock()
	after.mu.RLock()
	defer before.mu.RUnlock()
	defer after.mu.RUnlock()

	var result DiffResult
	// nodes in after but not in before, or changed
	for id, an := range after.nodes {
		bn, exists := before.nodes[id]
		if !exists {
			cp := *an
			result.Added = append(result.Added, &cp)
		} else if an.Severity != bn.Severity || an.Label != bn.Label {
			cp := *an
			result.Changed = append(result.Changed, &cp)
		}
	}
	// nodes in before but not in after
	for id, bn := range before.nodes {
		if _, exists := after.nodes[id]; !exists {
			cp := *bn
			result.Removed = append(result.Removed, &cp)
		}
	}
	sortNodes(result.Added)
	sortNodes(result.Removed)
	sortNodes(result.Changed)
	return result
}

// LoadFromPath opens a snapshot file and replays it into a new Graph.
func LoadFromPath(path string) (*Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g := New()
	return g, g.Load(f)
}
