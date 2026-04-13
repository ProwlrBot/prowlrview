-- graph-decorator.lua — purely visual: paints nodes by tech stack / status.

plugin.name  = "graph-decorator"
plugin.style = { accent = "#00eaff" }

on_node(function(n)
  local tech = n.detail and n.detail.tech or ""
  if tech:match("WordPress") then graph:tag(n.id, "wp", "#21759b") end
  if tech:match("Laravel")   then graph:tag(n.id, "laravel", "#ff2d20") end
  if tech:match("Nginx")     then graph:tag(n.id, "nginx", "#009639") end
  if tech:match("Cloudflare") then graph:tag(n.id, "cf", "#f48120") end

  if n.kind == "endpoint" and n.label:match("/admin") then
    graph:tag(n.id, "admin", "#ff6c11")
    graph:raise(n.id, "low")
  end
end)
