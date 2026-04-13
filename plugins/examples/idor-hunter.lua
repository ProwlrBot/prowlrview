-- idor-hunter.lua — flags numeric-ID API paths as IDOR candidates.
-- drop in ~/.config/prowlrview/plugins/

plugin.name    = "idor-hunter"
plugin.author  = "kdairatchi"
plugin.style   = { badge = "IDOR", color = "#ff6c11" }

on_request(function(req)
  if req.path:match("/api/.*/%d+") or req.path:match("/users/%d+") then
    graph:tag(req.host, "idor-candidate", plugin.style.color)
    notify("IDOR candidate: " .. req.method .. " " .. req.path)
  end
end)

on_response(function(resp)
  -- PII leak heuristic on JSON bodies
  if resp.headers["content-type"] and resp.headers["content-type"]:match("json") then
    if resp:matches('"email"%s*:%s*"[^"]+@') then
      graph:upsert("finding:pii:" .. resp.url, "finding",
                   "email in response body", resp.url, "medium")
    end
  end
end)
