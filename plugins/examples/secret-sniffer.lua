-- secret-sniffer.lua — passive regex pass over response bodies for leaked secrets.

plugin.name  = "secret-sniffer"
plugin.style = { badge = "SECRET", color = "#ff2d55" }

local patterns = {
  { name = "aws-access-key",   re = "AKIA[0-9A-Z]{16}",                 sev = "critical" },
  { name = "github-pat",       re = "ghp_[A-Za-z0-9]{36}",              sev = "critical" },
  { name = "slack-token",      re = "xox[abpr]-[A-Za-z0-9-]+",          sev = "high"     },
  { name = "google-api",       re = "AIza[0-9A-Za-z_-]{35}",            sev = "high"     },
  { name = "jwt",              re = "eyJ[A-Za-z0-9_-]+%.eyJ[A-Za-z0-9_-]+%.[A-Za-z0-9_-]+", sev = "medium" },
  { name = "private-key",      re = "-----BEGIN (RSA|OPENSSH|EC) PRIVATE KEY-----",         sev = "critical" },
}

on_response(function(resp)
  for _, p in ipairs(patterns) do
    if resp:matches(p.re) then
      local id = "finding:secret:" .. p.name .. ":" .. resp.url
      graph:upsert(id, "finding", "leaked " .. p.name, resp.url, p.sev)
      graph:tag(resp.url, p.name, plugin.style.color)
      notify("🔴 " .. p.name .. " in " .. resp.url)
    end
  end
end)
