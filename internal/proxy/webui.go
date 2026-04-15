package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/ProwlrBot/prowlrview/internal/graph"
)

// Flow is a single captured request/response pair shown in the web UI.
type Flow struct {
	ID        string    `json:"id"`
	Time      time.Time `json:"time"`
	Method    string    `json:"method"`
	Host      string    `json:"host"`
	Path      string    `json:"path"`
	Status    int       `json:"status"`
	Blocked   bool      `json:"blocked"`
	Reason    string    `json:"reason,omitempty"`
	DurMs     int64     `json:"dur_ms"`
	ReqBytes  int       `json:"req_bytes"`
	RespBytes int       `json:"resp_bytes"`
}

type FlowStore struct {
	mu    sync.Mutex
	flows []Flow
	max   int
}

func NewFlowStore(max int) *FlowStore { return &FlowStore{max: max} }

func (s *FlowStore) Add(f Flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flows = append(s.flows, f)
	if len(s.flows) > s.max {
		s.flows = s.flows[len(s.flows)-s.max:]
	}
}

func (s *FlowStore) Snapshot() []Flow {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Flow, len(s.flows))
	copy(out, s.flows)
	return out
}

// ServeWeb starts an HTTP server on addr exposing the dashboard + JSON feed.
func ServeWeb(addr string, g *graph.Graph, store *FlowStore, logFn func(string)) error {
	if logFn == nil {
		logFn = func(string) {}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(dashboardHTML))
	})
	mux.HandleFunc("/api/flows", func(w http.ResponseWriter, r *http.Request) {
		flows := store.Snapshot()
		sort.Slice(flows, func(i, j int) bool { return flows[i].Time.After(flows[j].Time) })
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(flows)
	})
	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		nodes := g.Nodes()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(nodes)
	})
	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		flows := store.Snapshot()
		stats := map[string]any{
			"flows":     len(flows),
			"nodes":     len(g.Nodes()),
			"hosts":     uniqueHosts(flows),
			"blocked":   countBlocked(flows),
			"errors":    countStatusGE(flows, 400),
			"timestamp": time.Now().Unix(),
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(stats)
	})
	mux.HandleFunc("/ca.crt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="prowlrview-ca.crt"`)
		w.Header().Set("Content-Type", "application/x-x509-ca-cert")
		http.ServeFile(w, r, caPath())
	})
	logFn(fmt.Sprintf("web ui: http://%s  ·  CA: http://%s/ca.crt", addr, addr))
	return http.ListenAndServe(addr, mux)
}

func uniqueHosts(flows []Flow) int {
	m := map[string]bool{}
	for _, f := range flows {
		m[f.Host] = true
	}
	return len(m)
}
func countBlocked(flows []Flow) int {
	n := 0
	for _, f := range flows {
		if f.Blocked {
			n++
		}
	}
	return n
}
func countStatusGE(flows []Flow, s int) int {
	n := 0
	for _, f := range flows {
		if f.Status >= s {
			n++
		}
	}
	return n
}

const dashboardHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>prowlrview · live</title>
<meta name="viewport" content="width=device-width,initial-scale=1">
<style>
  :root{
    --bg:#0b0f14; --panel:#11161f; --panel2:#0e1320; --line:#1d2535;
    --fg:#e6edf3; --dim:#7d8aa0; --accent:#7ee787; --accent2:#79c0ff;
    --warn:#f0b400; --err:#ff7b72; --crit:#ff4d6d; --info:#79c0ff;
    --mono:'JetBrains Mono','Fira Code',ui-monospace,Menlo,monospace;
  }
  *{box-sizing:border-box}
  html,body{margin:0;height:100%;background:var(--bg);color:var(--fg);font-family:var(--mono);font-size:13px}
  header{
    display:flex;align-items:center;gap:16px;padding:10px 18px;
    background:linear-gradient(90deg,#0d1320,#0b0f14);border-bottom:1px solid var(--line);
  }
  header .logo{font-weight:700;letter-spacing:.5px;color:var(--accent)}
  header .tag{color:var(--dim);font-size:11px}
  header .pill{padding:3px 10px;border:1px solid var(--line);border-radius:999px;color:var(--dim)}
  header .pill b{color:var(--accent2)}
  header .grow{flex:1}
  header a{color:var(--accent2);text-decoration:none;border-bottom:1px dotted var(--accent2)}
  main{display:grid;grid-template-columns:1fr 380px;gap:0;height:calc(100% - 49px)}
  .pane{overflow:auto;border-right:1px solid var(--line)}
  .pane:last-child{border-right:0;background:var(--panel2)}
  table{width:100%;border-collapse:collapse}
  thead th{position:sticky;top:0;background:var(--panel);color:var(--dim);font-weight:500;
    text-align:left;padding:8px 12px;border-bottom:1px solid var(--line);font-size:11px;text-transform:uppercase;letter-spacing:.5px}
  tbody td{padding:7px 12px;border-bottom:1px solid #131a26;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;max-width:1px}
  tbody tr:hover{background:#141b28;cursor:pointer}
  tbody tr.blocked{background:#2a1418}
  .method{display:inline-block;padding:1px 7px;border-radius:4px;font-size:11px;font-weight:600}
  .m-GET{background:#0e3a25;color:var(--accent)}
  .m-POST{background:#1e3a5f;color:var(--accent2)}
  .m-PUT,.m-PATCH{background:#3d2f0a;color:var(--warn)}
  .m-DELETE{background:#3a1217;color:var(--err)}
  .status-2{color:var(--accent)} .status-3{color:var(--accent2)}
  .status-4{color:var(--warn)} .status-5{color:var(--err)} .status-0{color:var(--dim)}
  .stats{display:grid;grid-template-columns:repeat(2,1fr);gap:8px;padding:14px}
  .stat{background:var(--panel);border:1px solid var(--line);border-radius:8px;padding:12px}
  .stat .k{color:var(--dim);font-size:10px;text-transform:uppercase;letter-spacing:.6px}
  .stat .v{font-size:22px;font-weight:700;margin-top:2px}
  .stat.warn .v{color:var(--warn)} .stat.err .v{color:var(--err)} .stat.ok .v{color:var(--accent)}
  h3{margin:18px 14px 6px;color:var(--dim);font-size:11px;letter-spacing:.6px;text-transform:uppercase;font-weight:500}
  .hosts{padding:0 14px 14px}
  .host{display:flex;justify-content:space-between;padding:5px 8px;border-bottom:1px solid #131a26;font-size:12px}
  .host .n{color:var(--dim)}
  .empty{padding:40px;text-align:center;color:var(--dim)}
  .empty .ascii{white-space:pre;color:var(--accent);opacity:.6;font-size:11px;line-height:1.2;margin-bottom:14px}
  footer{padding:8px 14px;border-top:1px solid var(--line);color:var(--dim);font-size:11px;background:var(--panel)}
  .live{display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--accent);
    box-shadow:0 0 8px var(--accent);margin-right:6px;animation:pulse 1.4s ease-in-out infinite}
  @keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}
</style>
</head>
<body>
<header>
  <span class="logo">▲ prowlrview</span>
  <span class="tag">live MITM dashboard</span>
  <span class="pill"><span class="live"></span><b id="flows">0</b> flows</span>
  <span class="pill"><b id="hosts">0</b> hosts</span>
  <span class="pill"><b id="blocked" style="color:var(--err)">0</b> blocked</span>
  <span class="grow"></span>
  <a href="/ca.crt" download>↓ CA cert</a>
</header>
<main>
  <section class="pane" id="flowpane">
    <table>
      <thead><tr><th style="width:60px">⏱</th><th style="width:60px">method</th><th style="width:60px">status</th><th style="width:200px">host</th><th>path</th><th style="width:70px">size</th></tr></thead>
      <tbody id="rows"></tbody>
    </table>
    <div id="empty" class="empty">
      <div class="ascii">▲ ▲ ▲
waiting for traffic
configure your browser proxy → 127.0.0.1:8888</div>
      Run <code>prowlrview chrome</code> for an isolated browser already wired up.
    </div>
  </section>
  <section class="pane">
    <div class="stats">
      <div class="stat ok"><div class="k">Flows</div><div class="v" id="s-flows">0</div></div>
      <div class="stat"><div class="k">Hosts</div><div class="v" id="s-hosts">0</div></div>
      <div class="stat warn"><div class="k">4xx</div><div class="v" id="s-errors">0</div></div>
      <div class="stat err"><div class="k">Blocked</div><div class="v" id="s-blocked">0</div></div>
    </div>
    <h3>Top hosts</h3>
    <div class="hosts" id="hostlist"></div>
  </section>
</main>
<footer><span class="live"></span> auto-refresh 1s · proxy on :8888 · plugins fire on every flow</footer>
<script>
const fmt=t=>{const d=new Date(t);return d.toTimeString().slice(0,8)};
const sizeFmt=n=>n<1024?n+'b':(n/1024).toFixed(1)+'k';
async function tick(){
  const [flows, stats] = await Promise.all([
    fetch('/api/flows').then(r=>r.json()),
    fetch('/api/stats').then(r=>r.json()),
  ]);
  document.getElementById('flows').textContent=stats.flows;
  document.getElementById('hosts').textContent=stats.hosts;
  document.getElementById('blocked').textContent=stats.blocked;
  document.getElementById('s-flows').textContent=stats.flows;
  document.getElementById('s-hosts').textContent=stats.hosts;
  document.getElementById('s-errors').textContent=stats.errors;
  document.getElementById('s-blocked').textContent=stats.blocked;

  const tbody=document.getElementById('rows'), empty=document.getElementById('empty');
  if(!flows.length){empty.style.display='block';tbody.innerHTML='';return}
  empty.style.display='none';
  tbody.innerHTML=flows.slice(0,200).map(f=>{
    const sc=Math.floor((f.status||0)/100);
    return '<tr class="'+(f.blocked?'blocked':'')+'">'+
      '<td>'+fmt(f.time)+'</td>'+
      '<td><span class="method m-'+(f.method||'GET')+'">'+(f.method||'')+'</span></td>'+
      '<td class="status-'+sc+'">'+(f.status||'—')+'</td>'+
      '<td>'+f.host+'</td>'+
      '<td>'+f.path+'</td>'+
      '<td>'+sizeFmt(f.resp_bytes||0)+'</td></tr>';
  }).join('');

  const hosts={};flows.forEach(f=>hosts[f.host]=(hosts[f.host]||0)+1);
  const sorted=Object.entries(hosts).sort((a,b)=>b[1]-a[1]).slice(0,12);
  document.getElementById('hostlist').innerHTML=sorted.map(([h,n])=>
    '<div class="host"><span>'+h+'</span><span class="n">'+n+'</span></div>').join('') ||
    '<div class="host"><span style="color:var(--dim)">no hosts yet</span><span></span></div>';
}
tick();setInterval(tick,1000);
</script>
</body>
</html>`
