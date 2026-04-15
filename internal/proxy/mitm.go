// Package proxy runs a MITM HTTP/HTTPS proxy that fires every request and
// response through the plugin host, and adds traffic nodes to the graph so
// the TUI can show live flow.
package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/graph"
	"github.com/ProwlrBot/prowlrview/internal/plugin"
	"github.com/elazarl/goproxy"
)

// maxFlowNodes bounds transient "flow" nodes in the graph (findings are kept).
const maxFlowNodes = 500

// Run starts the proxy on addr (e.g. ":8888") until the process exits.
// If store is non-nil, every flow is captured into it for the web UI.
func Run(addr string, g *graph.Graph, h *plugin.Host, store *FlowStore, logFn func(string)) error {
	if logFn == nil {
		logFn = func(string) {}
	}
	caCert, caKey, err := loadOrCreateCA()
	if err != nil {
		return fmt.Errorf("ca: %w", err)
	}
	if err := setGoproxyCA(caCert, caKey); err != nil {
		return fmt.Errorf("install ca: %w", err)
	}

	p := goproxy.NewProxyHttpServer()
	p.Verbose = false
	p.OnRequest().HandleConnect(goproxy.AlwaysMitm)

	p.OnRequest().DoFunc(func(r *http.Request, ctx *goproxy.ProxyCtx) (*http.Request, *http.Response) {
		start := time.Now()
		req := h.FireRequest(r)
		host := r.URL.Host
		g.Upsert(graph.Node{ID: "host:" + host, Kind: "host", Label: host, Source: "proxy"})
		flowID := fmt.Sprintf("flow:%s%s:%d", host, r.URL.Path, start.UnixNano())
		g.Upsert(graph.Node{
			ID:     flowID,
			Kind:   "flow",
			Label:  r.Method + " " + r.URL.Path,
			Parent: "host:" + host,
			Source: "proxy",
		})
		ctx.UserData = map[string]any{"flowID": flowID, "start": start}
		if req.Blocked {
			logFn("blocked " + r.URL.String() + ": " + req.Reason)
			if store != nil {
				store.Add(Flow{ID: flowID, Time: start, Method: r.Method, Host: host, Path: r.URL.Path,
					Status: 403, Blocked: true, Reason: req.Reason})
			}
			return r, goproxy.NewResponse(r, "text/plain", http.StatusForbidden,
				"prowlrview blocked: "+req.Reason)
		}
		return req.R, nil
	})

	p.OnResponse().DoFunc(func(resp *http.Response, ctx *goproxy.ProxyCtx) *http.Response {
		if resp == nil {
			return resp
		}
		h.FireResponse(resp)
		ud, _ := ctx.UserData.(map[string]any)
		flowID, _ := ud["flowID"].(string)
		start, _ := ud["start"].(time.Time)
		if flowID != "" {
			label := fmt.Sprintf("%d %s %s", resp.StatusCode, resp.Request.Method, resp.Request.URL.Path)
			sev := graph.SevInfo
			if resp.StatusCode >= 500 {
				sev = graph.SevMedium
			} else if resp.StatusCode >= 400 {
				sev = graph.SevLow
			}
			g.Upsert(graph.Node{
				ID:       flowID,
				Kind:     "flow",
				Label:    label,
				Parent:   "host:" + resp.Request.URL.Host,
				Severity: sev,
				Source:   "proxy",
			})
			if store != nil {
				rb := 0
				if resp.ContentLength > 0 {
					rb = int(resp.ContentLength)
				}
				store.Add(Flow{
					ID: flowID, Time: start, Method: resp.Request.Method,
					Host: resp.Request.URL.Host, Path: resp.Request.URL.Path,
					Status: resp.StatusCode, DurMs: time.Since(start).Milliseconds(),
					RespBytes: rb,
				})
			}
			// cap transient flow nodes so long sessions don't balloon the graph
			g.PruneKind("flow", maxFlowNodes)
		}
		return resp
	})

	logFn("proxy listening on " + addr + " (CA: " + caPath() + ")")
	return http.ListenAndServe(addr, p)
}

func caDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "prowlrview")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".config", "prowlrview")
}
func caPath() string  { return filepath.Join(caDir(), "ca.crt") }
func keyPath() string { return filepath.Join(caDir(), "ca.key") }

func loadOrCreateCA() (*x509.Certificate, *rsa.PrivateKey, error) {
	if cb, err := os.ReadFile(caPath()); err == nil {
		if kb, err := os.ReadFile(keyPath()); err == nil {
			cBlock, _ := pem.Decode(cb)
			kBlock, _ := pem.Decode(kb)
			if cBlock != nil && kBlock != nil {
				cert, e1 := x509.ParseCertificate(cBlock.Bytes)
				key, e2 := x509.ParsePKCS1PrivateKey(kBlock.Bytes)
				if e1 == nil && e2 == nil {
					return cert, key, nil
				}
			}
		}
	}
	if err := os.MkdirAll(caDir(), 0o700); err != nil {
		return nil, nil, err
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "prowlrview MITM CA", Organization: []string{"ProwlrBot"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(caPath(), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(keyPath(), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		return nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return cert, key, err
}

func setGoproxyCA(cert *x509.Certificate, key *rsa.PrivateKey) error {
	ca := tls.Certificate{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  key,
		Leaf:        cert,
	}
	goproxy.GoproxyCa = ca
	tlsConfig := goproxy.TLSConfigFromCA(&ca)
	goproxy.OkConnect = &goproxy.ConnectAction{Action: goproxy.ConnectAccept, TLSConfig: tlsConfig}
	goproxy.MitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectMitm, TLSConfig: tlsConfig}
	goproxy.HTTPMitmConnect = &goproxy.ConnectAction{Action: goproxy.ConnectHTTPMitm, TLSConfig: tlsConfig}
	goproxy.RejectConnect = &goproxy.ConnectAction{Action: goproxy.ConnectReject, TLSConfig: tlsConfig}
	return nil
}
