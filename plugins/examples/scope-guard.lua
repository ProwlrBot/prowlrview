-- scope-guard.lua — blocks out-of-scope requests in proxy mode,
-- colors in-scope green and OOS red in the graph.

plugin.name  = "scope-guard"
plugin.style = { in_scope = "#30d158", out_of_scope = "#ff2d55" }

local scope = {
  "*.prowlrbot.com",
  "*.hackerone.com",
  "target.example.com",
}

local function matches(host, pat)
  if pat:sub(1, 2) == "*." then
    return host:sub(-#pat + 1) == pat:sub(2)
  end
  return host == pat
end

local function in_scope(host)
  for _, p in ipairs(scope) do
    if matches(host, p) then return true end
  end
  return false
end

on_request(function(req)
  if in_scope(req.host) then
    graph:tag(req.host, "in-scope", plugin.style.in_scope)
  else
    graph:tag(req.host, "OOS", plugin.style.out_of_scope)
    req:block("out-of-scope per scope-guard.lua")
  end
end)
