package setupweb

import (
	"html/template"
	"net/http"
)

// HTML templates are inline (and intentionally small) so the setup wizard
// is self-contained — no embed.FS, no JS framework, no external assets.

var formTmpl = template.Must(template.New("form").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>WantasticCore — Setup</title>
  <style>
    :root { color-scheme: light dark; }
    body {
      font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      max-width: 780px; margin: 0 auto; padding: 2rem 1.25rem 4rem; line-height: 1.5;
      background: #0f1218; color: #e5e7eb;
    }
    h1 { margin: 0 0 0.25rem; font-size: 1.6rem; }
    .lede { color: #9ca3af; margin: 0 0 2rem; }
    fieldset {
      border: 1px solid #2b3140; border-radius: 8px; padding: 1rem 1.25rem;
      margin: 0 0 1.25rem; background: rgba(255,255,255,0.02);
    }
    legend { padding: 0 0.5rem; font-weight: 600; color: #d1d5db; }
    label { display: block; margin: 0.75rem 0 0.25rem; font-size: 0.9rem; color: #9ca3af; }
    input[type="text"], input[type="email"], input[type="password"], input[type="number"] {
      width: 100%; box-sizing: border-box; padding: 0.5rem 0.65rem; font-size: 0.95rem;
      background: rgba(255,255,255,0.04); border: 1px solid #2b3140; color: inherit;
      border-radius: 4px;
    }
    .row { display: grid; grid-template-columns: 1fr 1fr; gap: 0.75rem 1rem; }
    .hint { color: #6b7280; font-size: 0.8rem; margin-top: 0.25rem; }
    .toggle { display: flex; align-items: center; gap: 0.5rem; margin: 0.5rem 0 0; }
    button {
      background: #2563eb; color: #fff; border: none; padding: 0.65rem 1.5rem;
      border-radius: 4px; font-size: 0.95rem; cursor: pointer; font-weight: 500;
    }
    button:hover { background: #1d4ed8; }
    .error {
      background: rgba(248, 113, 113, 0.1); border: 1px solid rgba(248, 113, 113, 0.3);
      color: #fca5a5; padding: 0.65rem 0.85rem; border-radius: 4px; margin: 0 0 1rem;
    }
  </style>
</head>
<body>
  <h1>WantasticCore — Setup</h1>
  <p class="lede">First-run configuration. The values you enter here are
    written to <code>/etc/wantastic/config.yaml</code>, then the service
    restarts in normal mode. Required fields are marked with *.</p>

  {{ if .Error }}<div class="error">{{ .Error }}</div>{{ end }}

  <form method="post" action="/submit" autocomplete="off">
    <fieldset>
      <legend>Network</legend>
      <label>Domain *<input type="text" name="domain" value="{{ .Domain }}" placeholder="example.com" required></label>
      <div class="hint">The base domain. DNS records for the portal,
        Winbox, and WireGuard are derived from this.</div>

      <label>Console hostname *<input type="text" name="console_host" value="{{ .ConsoleHost }}" placeholder="console.example.com" required></label>
      <div class="hint">Where the web portal will live. Common choices:
        <code>console.&lt;domain&gt;</code> or just <code>&lt;domain&gt;</code> itself.</div>

      <div class="row">
        <div>
          <label>Winbox hostname<input type="text" name="winbox_host" value="{{ .WinboxHost }}" placeholder="winbox.example.com"></label>
          <div class="hint">Defaults to <code>winbox.&lt;domain&gt;</code>.</div>
        </div>
        <div>
          <label>WireGuard hostname<input type="text" name="wireguard_host" value="{{ .WireguardHost }}" placeholder="wg.example.com"></label>
          <div class="hint">Defaults to <code>wg.&lt;domain&gt;</code>.</div>
        </div>
      </div>
      <label>WireGuard UDP port<input type="number" name="wireguard_port" value="{{ .WireguardPort }}" min="1" max="65535"></label>
    </fieldset>

    <fieldset>
      <legend>TLS &amp; firewall</legend>
      <label class="toggle"><input type="checkbox" name="le_enabled" {{ if .LetsEncryptEnabled }}checked{{ end }}> Enable Let's Encrypt (auto-issue + 12h renew)</label>
      <div class="hint">Leave OFF for local-dev or VPN-only setups — nginx
        keeps the self-signed bootstrap cert and your domain is still
        propagated to the nginx <code>server_name</code>. Turn it ON only
        when this box's public IP is reachable on port 80 and your domain
        resolves to it; otherwise ACME will fail.</div>
      <label>Let's Encrypt email<input type="email" name="le_email" value="{{ .LetsEncryptEmail }}" placeholder="ops@example.com"></label>
      <div class="hint">Required when LE is enabled. Used for ACME
        registration and expiry notices.</div>
      <label class="toggle"><input type="checkbox" name="firewall_enabled" {{ if .FirewallEnabled }}checked{{ end }}> Enable in-container firewall (default-deny iptables)</label>
      <div class="hint">Allows only 80, 443, 8291/tcp and 51820/udp.
        Requires <code>--cap-add NET_ADMIN</code>. Toggle off if your host
        runs its own firewall and you don't want a second layer.</div>
    </fieldset>

    <fieldset>
      <legend>Super-admin account</legend>
      <label>Email *<input type="email" name="admin_email" value="{{ .AdminEmail }}" required></label>
      <label>Full name<input type="text" name="admin_name" value="{{ .AdminName }}"></label>
      <label>Password * (min 8 chars)<input type="password" name="admin_password" required></label>
      <label>Max peers for this account<input type="number" name="admin_max_peers" value="{{ .AdminMaxPeers }}" min="1"></label>
    </fieldset>

    <!-- Database & Redis are pre-provisioned inside the all-in-one image.
         Submitted as hidden fields so the existing handler keeps working. -->
    <input type="hidden" name="db_host"        value="{{ .DBHost }}">
    <input type="hidden" name="db_port"        value="{{ .DBPort }}">
    <input type="hidden" name="db_user"        value="{{ .DBUser }}">
    <input type="hidden" name="db_name"        value="{{ .DBName }}">
    <input type="hidden" name="db_password"    value="{{ .DBPassword }}">
    <input type="hidden" name="redis_addr"     value="{{ .RedisAddr }}">
    <input type="hidden" name="redis_password" value="{{ .RedisPassword }}">

    <fieldset>
      <legend>SMTP (optional)</legend>
      <label class="toggle"><input type="checkbox" name="smtp_enabled" {{ if .SMTPEnabled }}checked{{ end }}> Enable SMTP for outgoing email</label>
      <div class="hint">If disabled, mail falls back to the local
        <code>sendmail</code> binary, or to a disk spool under
        <code>/tmp/mail/</code>. You can enable this later by editing
        <code>config.yaml</code>.</div>
      <div class="row">
        <label>Host<input type="text" name="smtp_host" value="{{ .SMTPHost }}"></label>
        <label>Port<input type="number" name="smtp_port" value="{{ .SMTPPort }}"></label>
        <label>User<input type="text" name="smtp_user" value="{{ .SMTPUser }}"></label>
        <label>Password<input type="password" name="smtp_password" value="{{ .SMTPPassword }}"></label>
      </div>
      <label>From address<input type="email" name="smtp_from" value="{{ .SMTPFrom }}" placeholder="noreply@example.com"></label>
    </fieldset>

    <fieldset>
      <legend>Copilot / AdminBot (optional)</legend>
      <label class="toggle"><input type="checkbox" name="claude_enabled" {{ if .ClaudeEnabled }}checked{{ end }}> Enable AI assistant (Claude-backed)</label>
      <div class="hint">Powers the in-portal Copilot and (optionally) the
        WhatsApp adminbot. Requires an Anthropic API key.</div>
      <label>Anthropic API key<input type="password" name="claude_api_key" value="{{ .ClaudeAPIKey }}" placeholder="sk-ant-..."></label>
    </fieldset>

    <button type="submit">Save and continue →</button>
  </form>
</body>
</html>
`))

var doneTmpl = template.Must(template.New("done").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width,initial-scale=1" />
  <title>Wantastic — Setup complete</title>
  <style>
    :root { color-scheme: dark; }
    body {
      font-family: ui-sans-serif, system-ui, -apple-system, "Segoe UI", Roboto, sans-serif;
      max-width: 720px; margin: 0 auto; padding: 2rem 1.25rem; line-height: 1.5;
      background: #0f1218; color: #e5e7eb;
    }
    h1 { margin: 0 0 0.5rem; font-size: 1.6rem; color: #34d399; display:flex; align-items:center; gap:.5rem; }
    .lede { color: #9ca3af; margin: 0 0 1.5rem; }
    .card {
      background: rgba(255,255,255,0.03); border: 1px solid #2b3140;
      border-radius: 8px; padding: 1rem 1.25rem; margin: 0 0 1.25rem;
    }
    table { width: 100%; border-collapse: collapse; font-size: 0.95rem; }
    th, td { padding: 0.5rem 0.65rem; text-align: left; border-bottom: 1px solid #2b3140; }
    th { color: #9ca3af; font-weight: 500; font-size: 0.85rem; }
    td code { background: rgba(255,255,255,0.06); padding: 0.1rem 0.4rem; border-radius: 3px; }
    .ip { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; color: #fbbf24; }

    /* ── Progress UI ───────────────────────────────────────────── */
    .progress-card { border-color: rgba(96,165,250,0.4); }
    .progress-row { display:flex; align-items:center; gap:.75rem; margin:.6rem 0; }
    .progress-row .dot {
      width:.8rem; height:.8rem; border-radius:50%;
      background:#374151; flex-shrink:0;
      transition: background .2s, box-shadow .2s;
    }
    .progress-row.active .dot {
      background:#60a5fa;
      box-shadow:0 0 0 3px rgba(96,165,250,.18);
      animation:pulse 1.4s ease-in-out infinite;
    }
    .progress-row.done .dot { background:#34d399; }
    .progress-row .label { color:#d1d5db; font-size:.95rem; }
    .progress-row.done .label { color:#9ca3af; }
    @keyframes pulse {
      0%,100% { box-shadow:0 0 0 3px rgba(96,165,250,.18); }
      50%     { box-shadow:0 0 0 7px rgba(96,165,250,.04); }
    }
    .bar {
      height:4px; background:#1f2937; border-radius:2px; overflow:hidden;
      margin-top:1rem;
    }
    .bar-fill {
      height:100%;
      background:linear-gradient(90deg, #60a5fa, #34d399);
      width:0%;
      transition: width .4s ease;
    }
    .hint { color:#6b7280; font-size:.85rem; margin-top:.75rem; }
    .redirect-row { margin-top:1rem; font-size:.95rem; }
    .redirect-row a { color:#60a5fa; }
  </style>
</head>
<body>
  <h1>✓ Setup complete</h1>
  <p class="lede">Configuration written to <code>/etc/wantastic/config.yaml</code>.
    The portal is coming up — this page will redirect automatically.</p>

  <div class="card progress-card">
    <div class="progress-row done" id="step-config">
      <span class="dot"></span><span class="label">Configuration saved</span>
    </div>
    <div class="progress-row active" id="step-restart">
      <span class="dot"></span><span class="label">Restarting service in normal mode…</span>
    </div>
    <div class="progress-row" id="step-portal">
      <span class="dot"></span><span class="label">Waiting for portal to come up…</span>
    </div>
    <div class="progress-row" id="step-redirect">
      <span class="dot"></span><span class="label">Redirecting to the console…</span>
    </div>
    <div class="bar"><div class="bar-fill" id="bar"></div></div>
    <p class="hint" id="hint">This usually takes 5–15 seconds while postgres + redis + nginx settle.</p>
    <p class="redirect-row">
      Not redirecting? <a href="/" id="manual-link">Open the console</a>.
    </p>
  </div>

  <div class="card">
    <h2 style="margin-top:0;font-size:1.1rem;">DNS records to add</h2>
    <p>Point these hostnames at this server's public IP:
      <span class="ip">{{ .IP }}</span></p>
    <table>
      <thead><tr><th>Type</th><th>Name</th><th>Value</th></tr></thead>
      <tbody>
        <tr><td>A / AAAA</td><td><code>{{ .Console }}</code></td><td><span class="ip">{{ .IP }}</span></td></tr>
        <tr><td>A / AAAA</td><td><code>{{ .Winbox }}</code></td><td><span class="ip">{{ .IP }}</span></td></tr>
        <tr><td>A / AAAA</td><td><code>{{ .Wireguard }}</code></td><td><span class="ip">{{ .IP }}</span></td></tr>
      </tbody>
    </table>
  </div>

  <script>
    (function () {
      // Poll a portal-only asset. The wizard never serves /img/icon/Account.svg
      // (that file lives in the embedded Svelte bundle), so a 200 here is a
      // reliable "portal is up + nginx is routing to it" signal.
      var probeUrl = "/img/icon/Account.svg?probe=" + Date.now();
      var attempts = 0;
      var maxAttempts = 90;          // ~3 minutes worst case
      var intervalMs = 2000;
      var stepRestart = document.getElementById("step-restart");
      var stepPortal  = document.getElementById("step-portal");
      var stepRedir   = document.getElementById("step-redirect");
      var bar         = document.getElementById("bar");
      var hint        = document.getElementById("hint");
      var moved       = false;

      function setActive(el) {
        ["step-config","step-restart","step-portal","step-redirect"].forEach(function (id) {
          var e = document.getElementById(id);
          e.classList.remove("active");
        });
        if (el) el.classList.add("active");
      }

      function markDone(el) {
        el.classList.remove("active");
        el.classList.add("done");
      }

      function poll() {
        attempts++;
        var pct = Math.min(95, Math.round((attempts / 8) * 100));
        bar.style.width = pct + "%";
        if (attempts === 3) {
          markDone(stepRestart);
          setActive(stepPortal);
        }
        if (attempts > 30) {
          hint.textContent = "Still warming up. If this persists, check 'docker logs wantastic'.";
        }
        if (attempts > maxAttempts) {
          hint.textContent = "Portal didn't come up after 3 minutes. Check the container logs.";
          return;
        }
        fetch(probeUrl + attempts, { cache: "no-store", credentials: "omit" })
          .then(function (r) {
            if (r.ok) {
              markDone(stepPortal);
              setActive(stepRedir);
              bar.style.width = "100%";
              hint.textContent = "Portal is up — redirecting now.";
              if (!moved) {
                moved = true;
                setTimeout(function () { window.location.replace("/"); }, 600);
              }
              return;
            }
            setTimeout(poll, intervalMs);
          })
          .catch(function () { setTimeout(poll, intervalMs); });
      }

      // Start polling after a brief delay so the s6 supervisor has a chance
      // to actually restart wantastic-core.
      setTimeout(poll, 1500);
    })();
  </script>
</body>
</html>
`))

func renderForm(w http.ResponseWriter, data formData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = formTmpl.Execute(w, data)
}

func renderDone(w http.ResponseWriter, res *Result, ip string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = doneTmpl.Execute(w, struct {
		Console   string
		Winbox    string
		Wireguard string
		IP        string
	}{
		Console:   res.ConsoleHost,
		Winbox:    res.WinboxHost,
		Wireguard: res.WireguardHost,
		IP:        ip,
	})
}
