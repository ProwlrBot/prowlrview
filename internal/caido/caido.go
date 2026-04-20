package caido

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/graph"
)

// Export is the top-level shape of a Caido session export file.
type Export struct {
	Requests []CaidoRequest `json:"requests"`
	Findings []CaidoFinding `json:"findings"`
}

// CaidoRequest represents one HTTP request recorded by Caido.
type CaidoRequest struct {
	ID       string         `json:"id"`
	Host     string         `json:"host"`
	Port     int            `json:"port"`
	Path     string         `json:"path"`
	Method   string         `json:"method"`
	Response *CaidoResponse `json:"response"`
}

// CaidoResponse is the optional response attached to a request.
type CaidoResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers"`
	Body    string              `json:"body"`
}

// CaidoFinding is a finding entry from Caido.
type CaidoFinding struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Severity  string `json:"severity"`
	RequestID string `json:"request_id"`
}

// Import reads a Caido export file and upserts nodes into g.
// Returns counts of imported requests and findings.
// Missing or empty fields are handled gracefully.
func Import(path string, g *graph.Graph) (requests, findings int, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	var exp Export
	if err := json.Unmarshal(data, &exp); err != nil {
		return 0, 0, fmt.Errorf("parse: %w", err)
	}

	// index Caido request ID → resolved URL for finding parent lookups
	reqURLs := make(map[string]string)

	for _, r := range exp.Requests {
		if r.Host == "" {
			continue
		}
		scheme := "https"
		if r.Port == 80 {
			scheme = "http"
		}
		url := fmt.Sprintf("%s://%s%s", scheme, r.Host, r.Path)

		g.Upsert(graph.Node{ID: r.Host, Kind: "host", Label: r.Host, Source: "caido"})

		sev := graph.SevInfo
		if r.Response != nil && r.Response.Status >= 500 {
			sev = graph.SevMedium
		} else if r.Response != nil && r.Response.Status >= 400 {
			sev = graph.SevLow
		}

		detail := map[string]string{"method": r.Method}
		if r.Response != nil {
			detail["status"] = fmt.Sprintf("%d", r.Response.Status)
		}

		g.Upsert(graph.Node{
			ID:       url,
			Kind:     "endpoint",
			Label:    r.Method + " " + r.Path,
			Parent:   r.Host,
			Severity: sev,
			Source:   "caido",
			Detail:   detail,
		})
		reqURLs[r.ID] = url
		requests++
	}

	for _, f := range exp.Findings {
		parent := reqURLs[f.RequestID]
		id := "finding:caido:" + f.ID
		g.Upsert(graph.Node{
			ID:       id,
			Kind:     "finding",
			Label:    f.Title,
			Parent:   parent,
			Severity: graph.ParseSeverity(f.Severity),
			Source:   "caido",
			Detail:   map[string]string{"rule": f.Title, "severity": f.Severity},
		})
		findings++
	}
	return requests, findings, nil
}

// Push sends a finding node to Caido via its REST API.
// endpoint is the Caido instance base URL, e.g. "http://127.0.0.1:8080".
func Push(endpoint, findingID string, g *graph.Graph) error {
	n, ok := g.Get(findingID)
	if !ok {
		return fmt.Errorf("node not found: %s", findingID)
	}
	payload := map[string]any{
		"title":    n.Label,
		"severity": n.Severity.String(),
		"reporter": "prowlrview",
		"url":      n.Parent,
		"detail":   n.Detail,
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest("POST", endpoint+"/api/v1/findings", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Source", "prowlrview")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("POST: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("caido returned %d", resp.StatusCode)
	}
	return nil
}
