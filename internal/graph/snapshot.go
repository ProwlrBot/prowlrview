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
