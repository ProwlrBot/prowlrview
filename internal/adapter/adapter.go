// Package adapter normalizes tool output into graph.Node events.
// Auto-detects: SARIF, nuclei JSONL, httpx JSONL, subfinder, katana, flaw, generic.
package adapter

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/ProwlrBot/prowlrview/internal/graph"
)

// Parse one JSON line and upsert into the graph. Unknown shapes are tolerated.
func ParseLine(line []byte, g *graph.Graph) {
	line = bytes_trim(line)
	if len(line) == 0 || line[0] != '{' {
		return
	}
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return
	}
	switch detect(m) {
	case "nuclei":
		fromNuclei(m, g)
	case "httpx":
		fromHttpx(m, g)
	case "subfinder":
		fromSubfinder(m, g)
	case "katana":
		fromKatana(m, g)
	case "flaw":
		fromFlaw(m, g)
	case "sarif":
		fromSARIFResult(m, g)
	default:
		fromGeneric(m, g)
	}
}

func detect(m map[string]any) string {
	if _, ok := m["template-id"]; ok {
		return "nuclei"
	}
	if _, ok := m["template"]; ok {
		if _, ok2 := m["matched-at"]; ok2 {
			return "nuclei"
		}
	}
	if _, ok := m["status_code"]; ok {
		if _, ok2 := m["input"]; ok2 {
			return "httpx"
		}
	}
	if _, ok := m["host"]; ok {
		if _, ok2 := m["source"]; ok2 {
			return "subfinder"
		}
	}
	if _, ok := m["endpoint"]; ok {
		return "katana"
	}
	if _, ok := m["rule"]; ok {
		if _, ok2 := m["file"]; ok2 {
			return "flaw"
		}
	}
	if _, ok := m["ruleId"]; ok {
		return "sarif"
	}
	return "generic"
}

func fromNuclei(m map[string]any, g *graph.Graph) {
	host := hostOf(str(m, "host"), str(m, "matched-at"))
	sev := graph.ParseSeverity(strNested(m, "info", "severity"))
	name := strNested(m, "info", "name")
	matched := str(m, "matched-at")
	rule := str(m, "template-id")
	if host != "" {
		g.Upsert(graph.Node{ID: host, Kind: "host", Label: host, Source: "nuclei"})
	}
	if matched != "" {
		g.Upsert(graph.Node{ID: matched, Kind: "endpoint", Label: matched, Parent: host, Source: "nuclei"})
	}
	id := "finding:" + rule + ":" + matched
	g.Upsert(graph.Node{
		ID: id, Kind: "finding", Label: name + " [" + rule + "]",
		Parent: firstNonEmpty(matched, host), Severity: sev, Source: "nuclei",
		Detail: map[string]string{"rule": rule, "matched": matched, "severity": sev.String()},
	})
}

func fromHttpx(m map[string]any, g *graph.Graph) {
	u := firstNonEmpty(str(m, "url"), str(m, "input"))
	host := hostOf(u)
	title := str(m, "title")
	tech, _ := m["tech"].([]any)
	if host != "" {
		g.Upsert(graph.Node{ID: host, Kind: "host", Label: host, Source: "httpx"})
	}
	if u != "" {
		detail := map[string]string{"title": title}
		if len(tech) > 0 {
			var techs []string
			for _, t := range tech {
				if s, ok := t.(string); ok {
					techs = append(techs, s)
				}
			}
			detail["tech"] = strings.Join(techs, ", ")
		}
		g.Upsert(graph.Node{ID: u, Kind: "endpoint", Label: u, Parent: host, Source: "httpx", Detail: detail})
	}
}

func fromSubfinder(m map[string]any, g *graph.Graph) {
	host := str(m, "host")
	src := str(m, "source")
	if host == "" {
		return
	}
	parts := strings.SplitN(host, ".", 2)
	var parent string
	if len(parts) == 2 {
		parent = parts[1]
		g.Upsert(graph.Node{ID: parent, Kind: "host", Label: parent, Source: "subfinder"})
	}
	g.Upsert(graph.Node{ID: host, Kind: "host", Label: host, Parent: parent, Source: "subfinder",
		Detail: map[string]string{"discovered_by": src}})
}

func fromKatana(m map[string]any, g *graph.Graph) {
	u := str(m, "endpoint")
	host := hostOf(u)
	if host != "" {
		g.Upsert(graph.Node{ID: host, Kind: "host", Label: host, Source: "katana"})
	}
	if u != "" {
		g.Upsert(graph.Node{ID: u, Kind: "endpoint", Label: u, Parent: host, Source: "katana"})
	}
}

func fromFlaw(m map[string]any, g *graph.Graph) {
	rule := str(m, "rule")
	file := str(m, "file")
	sev := graph.ParseSeverity(str(m, "severity"))
	msg := str(m, "message")
	g.Upsert(graph.Node{ID: file, Kind: "asset", Label: file, Source: "flaw"})
	id := "finding:" + rule + ":" + file
	g.Upsert(graph.Node{
		ID: id, Kind: "finding", Label: msg + " [" + rule + "]",
		Parent: file, Severity: sev, Source: "flaw",
		Detail: map[string]string{"rule": rule, "severity": sev.String()},
	})
}

func fromSARIFResult(m map[string]any, g *graph.Graph) {
	rule := str(m, "ruleId")
	sev := graph.ParseSeverity(str(m, "level"))
	msg := strNested(m, "message", "text")
	id := "finding:" + rule
	g.Upsert(graph.Node{ID: id, Kind: "finding", Label: msg, Severity: sev, Source: "sarif",
		Detail: map[string]string{"rule": rule}})
}

func fromGeneric(m map[string]any, g *graph.Graph) {
	if id, ok := m["id"].(string); ok && id != "" {
		label := firstNonEmpty(str(m, "label"), str(m, "name"), id)
		g.Upsert(graph.Node{ID: id, Kind: "asset", Label: label, Source: "generic"})
	}
}

func hostOf(candidates ...string) string {
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if !strings.Contains(c, "://") {
			c = "http://" + c
		}
		u, err := url.Parse(c)
		if err == nil && u.Host != "" {
			return u.Hostname()
		}
	}
	return ""
}

func str(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}

func strNested(m map[string]any, keys ...string) string {
	cur := any(m)
	for _, k := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[k]
	}
	if s, ok := cur.(string); ok {
		return s
	}
	return ""
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func bytes_trim(b []byte) []byte {
	for len(b) > 0 && (b[0] == ' ' || b[0] == '\t' || b[0] == '\n' || b[0] == '\r') {
		b = b[1:]
	}
	for len(b) > 0 {
		c := b[len(b)-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			b = b[:len(b)-1]
			continue
		}
		break
	}
	return b
}
