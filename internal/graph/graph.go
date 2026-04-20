// Package graph holds the in-memory attack-surface DAG.
// Nodes = hosts/paths/findings. Edges = discovery relationships.
package graph

import (
	"sort"
	"strings"
	"sync"
	"time"
)

type Severity int

const (
	SevInfo Severity = iota
	SevLow
	SevMedium
	SevHigh
	SevCritical
)

func (s Severity) Icon() string {
	switch s {
	case SevCritical:
		return "🔴"
	case SevHigh:
		return "🟠"
	case SevMedium:
		return "🟡"
	case SevLow:
		return "🟢"
	default:
		return "·"
	}
}

func (s Severity) String() string {
	return [...]string{"info", "low", "medium", "high", "critical"}[s]
}

func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "critical":
		return SevCritical
	case "high", "error":
		return SevHigh
	case "medium", "warning":
		return SevMedium
	case "low":
		return SevLow
	default:
		return SevInfo
	}
}

// Node is one entity in the attack surface graph.
type Node struct {
	ID         string            // canonical key (host, host+path, etc.)
	Kind       string            // host | path | endpoint | finding | asset
	Label      string            // display string
	Parent     string            // parent node ID ("" for roots)
	Severity   Severity          // worst severity observed on this node
	Confidence float64           // 0 means unset (treated as 1.0); range 0.0-1.0
	Tags       []string          // plugin-applied tags
	Source     string            // which adapter emitted it (nuclei, httpx, flaw, ...)
	Detail     map[string]string // free-form metadata
	SeenAt     time.Time
	Hits       int
}

// Score returns a sortable priority: (severity+1) * confidence.
// Confidence of 0 is treated as 1.0 (fully confident).
func (n *Node) Score() float64 {
	c := n.Confidence
	if c <= 0 {
		c = 1.0
	}
	return float64(n.Severity+1) * c
}

// Graph is a thread-safe node store.
type Graph struct {
	mu       sync.RWMutex
	nodes    map[string]*Node
	OnUpsert func(*Node) // called after each Upsert, outside the lock
}

func New() *Graph {
	return &Graph{nodes: make(map[string]*Node)}
}

// Upsert adds or merges a node. Severity is promoted (never demoted).
func (g *Graph) Upsert(n Node) *Node {
	g.mu.Lock()
	if n.ID == "" {
		g.mu.Unlock()
		return nil
	}
	if n.SeenAt.IsZero() {
		n.SeenAt = time.Now()
	}
	var result *Node
	if existing, ok := g.nodes[n.ID]; ok {
		existing.Hits++
		existing.SeenAt = n.SeenAt
		if n.Severity > existing.Severity {
			existing.Severity = n.Severity
		}
		if n.Confidence > 0 {
			existing.Confidence = n.Confidence
		}
		for _, t := range n.Tags {
			if !contains(existing.Tags, t) {
				existing.Tags = append(existing.Tags, t)
			}
		}
		for k, v := range n.Detail {
			if existing.Detail == nil {
				existing.Detail = map[string]string{}
			}
			existing.Detail[k] = v
		}
		result = existing
	} else {
		n.Hits = 1
		cp := n
		g.nodes[n.ID] = &cp
		result = &cp
	}
	g.mu.Unlock()
	if g.OnUpsert != nil {
		g.OnUpsert(result)
	}
	return result
}

func (g *Graph) Get(id string) (*Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

// Roots returns nodes with no parent, sorted by severity desc then ID.
func (g *Graph) Roots() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Node
	for _, n := range g.nodes {
		if n.Parent == "" {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// Children returns direct children of parentID.
func (g *Graph) Children(parentID string) []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Node
	for _, n := range g.nodes {
		if n.Parent == parentID {
			out = append(out, n)
		}
	}
	sortNodes(out)
	return out
}

// Findings returns all nodes with Kind == "finding", newest first.
func (g *Graph) Findings() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []*Node
	for _, n := range g.nodes {
		if n.Kind == "finding" {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Severity != out[j].Severity {
			return out[i].Severity > out[j].Severity
		}
		return out[i].SeenAt.After(out[j].SeenAt)
	})
	return out
}

// Nodes returns a snapshot of every node, severity-sorted.
func (g *Graph) Nodes() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]*Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sortNodes(out)
	return out
}

// PruneKind caps nodes of the given kind to keep (most recently seen).
// Findings (severity >= medium) are preserved regardless. Returns removed count.
func (g *Graph) PruneKind(kind string, keep int) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	if keep < 0 {
		keep = 0
	}
	var victims []*Node
	for _, n := range g.nodes {
		if n.Kind == kind && n.Severity < SevMedium {
			victims = append(victims, n)
		}
	}
	if len(victims) <= keep {
		return 0
	}
	sort.Slice(victims, func(i, j int) bool { return victims[i].SeenAt.After(victims[j].SeenAt) })
	removed := 0
	for _, n := range victims[keep:] {
		delete(g.nodes, n.ID)
		removed++
	}
	return removed
}

func (g *Graph) Len() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.nodes)
}

func sortNodes(ns []*Node) {
	sort.Slice(ns, func(i, j int) bool {
		if ns[i].Severity != ns[j].Severity {
			return ns[i].Severity > ns[j].Severity
		}
		return ns[i].Label < ns[j].Label
	})
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
