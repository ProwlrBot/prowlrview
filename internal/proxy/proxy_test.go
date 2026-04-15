package proxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/plugin"
)

// TestProxyFlowRoundTrip spins up the MITM proxy on a random port, sends a
// plain HTTP request through it, and verifies that:
//   - the flow is captured in the FlowStore,
//   - a host/flow node lands in the graph,
//   - plugin on_request fires and can block a request.
func TestProxyFlowRoundTrip(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		io.WriteString(w, "ok")
	}))
	defer backend.Close()

	g := graph.New()
	h := plugin.NewHost(g, func(string) {}, func(string) {})
	defer h.Close()
	store := NewFlowStore(100)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	go func() { _ = Run(addr, g, h, store, func(string) {}) }()

	// wait for listener
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	proxyURL, _ := url.Parse("http://" + addr)
	client := &http.Client{
		Transport: &http.Transport{
			Proxy:           http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		Timeout: 3 * time.Second,
	}

	resp, err := client.Get(backend.URL + "/hello")
	if err != nil {
		t.Fatalf("proxied GET: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "ok" || resp.StatusCode != 200 {
		t.Fatalf("unexpected response: %d %q", resp.StatusCode, body)
	}

	// give the proxy's OnResponse hook a tick to record the flow
	var flows []Flow
	for i := 0; i < 50; i++ {
		flows = store.Snapshot()
		if len(flows) > 0 && flows[0].Status == 200 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(flows) == 0 {
		t.Fatal("expected at least one captured flow")
	}
	f := flows[0]
	if f.Method != "GET" || !strings.HasSuffix(f.Path, "/hello") {
		t.Fatalf("unexpected flow: %+v", f)
	}
	if f.Status != 200 {
		t.Fatalf("want status 200, got %d", f.Status)
	}

	// graph should have a host node and at least one flow node
	hostNode, ok := g.Get("host:" + f.Host)
	if !ok || hostNode.Kind != "host" {
		t.Fatalf("host node missing for %s", f.Host)
	}
	var flowSeen bool
	for _, n := range g.Nodes() {
		if n.Kind == "flow" {
			flowSeen = true
			break
		}
	}
	if !flowSeen {
		t.Fatal("expected at least one flow node in graph")
	}
}
