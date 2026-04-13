package graph

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpsertAndChildren(t *testing.T) {
	g := New()
	g.Upsert(Node{ID: "a.com", Kind: "host", Label: "a.com"})
	g.Upsert(Node{ID: "https://a.com/x", Kind: "endpoint", Label: "/x", Parent: "a.com"})
	if len(g.Children("a.com")) != 1 {
		t.Fatal("child not linked")
	}
}

func TestSeverityNeverDemoted(t *testing.T) {
	g := New()
	g.Upsert(Node{ID: "n", Kind: "host", Label: "n", Severity: SevCritical})
	g.Upsert(Node{ID: "n", Kind: "host", Label: "n", Severity: SevLow})
	n, _ := g.Get("n")
	if n.Severity != SevCritical {
		t.Fatalf("expected critical kept, got %v", n.Severity)
	}
}

func TestSnapshotRoundtrip(t *testing.T) {
	g := New()
	g.Upsert(Node{ID: "host1", Kind: "host", Label: "host1", Severity: SevHigh})
	g.Upsert(Node{ID: "f1", Kind: "finding", Label: "RCE", Parent: "host1", Severity: SevCritical})

	var buf bytes.Buffer
	tmp := t.TempDir() + "/snap.jsonl"
	if err := g.Save(tmp); err != nil {
		t.Fatal(err)
	}
	// round-trip
	g2 := New()
	f, _ := bytesReader(tmp)
	defer f.Close()
	if err := g2.Load(f); err != nil {
		t.Fatal(err)
	}
	if g2.Len() != 2 {
		t.Fatalf("expected 2 nodes restored, got %d", g2.Len())
	}
	_ = buf
}

func TestExportDot(t *testing.T) {
	g := New()
	g.Upsert(Node{ID: "a", Kind: "host", Label: "a"})
	var buf bytes.Buffer
	if err := g.Dot(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "digraph prowlrview") {
		t.Fatal("missing dot header")
	}
}

func TestExportMermaid(t *testing.T) {
	g := New()
	g.Upsert(Node{ID: "a", Kind: "host", Label: "a"})
	g.Upsert(Node{ID: "b", Kind: "endpoint", Label: "b", Parent: "a"})
	var buf bytes.Buffer
	if err := g.Mermaid(&buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "flowchart LR") {
		t.Fatal("not a mermaid flowchart")
	}
}

// helper to avoid importing os at the top (keep tests minimal)
func bytesReader(path string) (interface {
	Read(p []byte) (int, error)
	Close() error
}, error) {
	return openFile(path)
}
