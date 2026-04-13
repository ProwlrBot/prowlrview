package adapter

import (
	"testing"

	"github.com/ProwlrBot/prowlrview/internal/graph"
)

func TestNuclei(t *testing.T) {
	g := graph.New()
	line := []byte(`{"template-id":"cve-2024-1","info":{"name":"RCE","severity":"critical"},"host":"api.target.com","matched-at":"https://api.target.com/up"}`)
	ParseLine(line, g)
	if g.Len() < 3 {
		t.Fatalf("expected host+endpoint+finding, got %d nodes", g.Len())
	}
	host, ok := g.Get("api.target.com")
	if !ok || host.Kind != "host" {
		t.Fatal("missing host node")
	}
	fs := g.Findings()
	if len(fs) != 1 || fs[0].Severity != graph.SevCritical {
		t.Fatalf("want 1 critical finding, got %+v", fs)
	}
}

func TestHttpx(t *testing.T) {
	g := graph.New()
	ParseLine([]byte(`{"url":"https://api.target.com/admin","input":"api.target.com","status_code":200,"title":"Admin","tech":["Nginx","WordPress"]}`), g)
	ep, ok := g.Get("https://api.target.com/admin")
	if !ok || ep.Kind != "endpoint" {
		t.Fatal("missing endpoint node")
	}
	if ep.Detail["tech"] == "" {
		t.Fatal("tech not recorded")
	}
}

func TestSubfinder(t *testing.T) {
	g := graph.New()
	ParseLine([]byte(`{"host":"beta.target.com","source":"crtsh"}`), g)
	if _, ok := g.Get("beta.target.com"); !ok {
		t.Fatal("missing subdomain")
	}
	if _, ok := g.Get("target.com"); !ok {
		t.Fatal("parent apex not created")
	}
}

func TestFlaw(t *testing.T) {
	g := graph.New()
	ParseLine([]byte(`{"rule":"crystal-sql-injection","file":"src/app.cr","severity":"high","message":"SQLi in query"}`), g)
	fs := g.Findings()
	if len(fs) != 1 || fs[0].Severity != graph.SevHigh {
		t.Fatalf("want 1 high finding, got %+v", fs)
	}
}

func TestGarbageTolerated(t *testing.T) {
	g := graph.New()
	ParseLine([]byte(`not json`), g)
	ParseLine([]byte(``), g)
	ParseLine([]byte(`{"broken":`), g)
	if g.Len() != 0 {
		t.Fatalf("garbage should not create nodes, got %d", g.Len())
	}
}

func TestSeverityPromotion(t *testing.T) {
	g := graph.New()
	ParseLine([]byte(`{"template-id":"x","info":{"name":"A","severity":"low"},"host":"h","matched-at":"https://h/a"}`), g)
	ParseLine([]byte(`{"template-id":"x","info":{"name":"A","severity":"critical"},"host":"h","matched-at":"https://h/a"}`), g)
	fs := g.Findings()
	if len(fs) != 1 || fs[0].Severity != graph.SevCritical {
		t.Fatalf("severity should promote to critical, got %+v", fs)
	}
}
