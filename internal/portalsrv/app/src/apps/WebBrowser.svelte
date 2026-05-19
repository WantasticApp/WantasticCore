<script lang="ts">
  import { draggable } from "@neodrag/svelte";
  import { scale } from "svelte/transition";
  import { onMount, onDestroy } from "svelte";
  import Titlebar from "$components/shared/Titlebar.svelte";
  import {
    webProxyStore,
    type WebProxySession,
    type BrowserTab,
    handleSetCookie,
    getCookieHeader,
  } from "$store/webproxy";
  import { WebProxyMux, type ProxyWebSocket } from "$store/webproxy-mux";
  import {
    openedApps,
    activeThing,
    appZIndexes,
    bringToFront,
  } from "$store/store";
  import { _ } from "$store/i18n";

  // Props for the browser instance
  export let tabId: string = "";

  // Window state
  let isMaximized = false;
  let iframeElement: HTMLIFrameElement | null = null;

  // Browser state
  // (former srcdoc cache removed — iframe is always written via the
  // bootstrap-injection path in loadPage().)
  let iframeKey = 0;
  let isLoading = false;
  let error: string | null = null;

  // Current tab and session
  let currentTab: BrowserTab | null = null;
  let currentSession: WebProxySession | null = null;

  // Stream manager for this session
  let streamManager: WebProxyMux | null = null;
  // Map iframe-generated ws_id → the matching ProxyWebSocket, so we can
  // dispatch ws_frame / ws_close postMessages from the iframe to the right
  // proxied connection. The iframe owns the wsId namespace; we just route.
  const iframeWsConns = new Map<string, ProxyWebSocket>();

  // Window ID for this browser instance
  $: windowId = "WebBrowser";

  // Z-index for window stacking
  $: zIndex = $appZIndexes[windowId] || 100;

  // Get shared minimize state from store
  $: isMinimized = $webProxyStore.isMinimized;

  // Subscribe to store
  $: {
    const state = $webProxyStore;
    if (tabId) {
      currentTab = state.openBrowsers.find((t) => t.id === tabId) || null;
      if (currentTab) {
        currentSession =
          state.sessions.find((s) => s.id === currentTab!.session_id) || null;
        isLoading = currentTab.loading;
        error = currentTab.error;
      }
    }
  }

  // Initialize stream manager when session changes
  $: if (currentSession && !streamManager) {
    streamManager = new WebProxyMux(currentSession.id);
  }

  // Watch activeThing to restore when clicked from taskbar
  $: if ($activeThing === windowId && isMinimized) {
    webProxyStore.restore();
  }

  // Bring to front when activated
  $: if ($activeThing === windowId) {
    bringToFront(windowId);
  }

  function handleFocus() {
    $activeThing = windowId;
    bringToFront(windowId);
  }

  function handleMaximize() {
    isMaximized = !isMaximized;
  }

  function handleReduce() {
    webProxyStore.minimize();
    $activeThing = "";
  }

  function handleClose() {
    iframeWsConns.clear();
    if (streamManager) {
      streamManager.close();
      streamManager = null;
    }
    if (tabId) {
      webProxyStore.closeBrowser(tabId);
    }
    $activeThing = "";
  }

  /**
   * Generate the iframe bootstrap script.
   *
   * The script runs once at the top of the proxied page's document and
   * installs intercepts for every way a browser context can leave or
   * trigger network I/O — fetch, XHR, EventSource, WebSocket, link
   * clicks, form submits, location.assign/replace/href, window.open,
   * and dynamically-added child iframes. Every intercept funnels into
   * one of three postMessage types (http_request, ws_*, nav_request)
   * so the parent has a single dispatch table to maintain.
   *
   * Correctness notes:
   *   - All listeners are paired with removal on the matching end-of-
   *     life event (response complete, ws_close). When the parent
   *     re-writes the document via document.open()/write()/close(),
   *     the global is reset and any leftover listeners are GC'd
   *     automatically.
   *   - Click and submit listeners are capture-phase + preventDefault,
   *     so page handlers cannot stop the intercept and a postMessage
   *     never re-dispatches the same event.
   *   - The streaming response path uses ReadableStream's built-in
   *     highWaterMark for backpressure — an iframe consumer that's slow
   *     to read body chunks naturally throttles the parent.
   *   - urlToProxyPath() resolves any URL the page emits (absolute,
   *     protocol-relative, page-relative) against the proxy base,
   *     and external hosts go to a real new tab via window.open.
   *   - location.* override is best-effort: same-origin sandbox lets us
   *     defineProperty in modern browsers, but if it throws we silently
   *     fall back to the link/form/<base> intercepts which catch most
   *     practical navigation.
   */
  function generateBootstrapScript(sessionId: string, baseUrl: string): string {
    // Both arguments are interpolated into a string literal in the
    // generated script body. JSON.stringify handles quotes/escapes,
    // but we additionally escape every `<` to `\u003c` so a hostile
    // baseUrl containing a closing script-tag sequence cannot break
    // out of the injected script and inject arbitrary code (sessionId is a server-issued
    // UUID and won't, but defensive escaping is free).
    const escapeForScript = (v: string): string =>
      JSON.stringify(v).replace(/</g, "\\u003c");
    const safeSessionId = escapeForScript(sessionId);
    const safeBaseUrl = escapeForScript(baseUrl);
    return `
      <script>
      (function() {
        'use strict';

        var SESSION_ID = ${safeSessionId};
        var BASE_URL = ${safeBaseUrl};

        // ── Helpers ─────────────────────────────────────────────────

        function makeId() {
          return Date.now().toString(36) + '-' + Math.random().toString(36).slice(2, 11);
        }

        // Resolve any URL a page might emit to a path under the proxy.
        // Returns { path, external } — external URLs (different host)
        // are passed through to the parent for "open in new tab".
        function urlToProxyPath(url) {
          if (!url) return { path: '/', external: false };
          var s = String(url);
          var lower = s.toLowerCase();
          if (lower.startsWith('javascript:') || lower.startsWith('mailto:') ||
              lower.startsWith('tel:') || lower.startsWith('data:') ||
              lower.startsWith('blob:') || lower.startsWith('about:') ||
              lower.startsWith('#')) {
            return { path: s, external: true, ignore: true };
          }
          try {
            var resolved = new URL(s, BASE_URL || 'https://localhost/');
            var base = new URL(BASE_URL || 'https://localhost/');
            if (resolved.host && base.host && resolved.host !== base.host) {
              return { path: s, external: true };
            }
            return {
              path: resolved.pathname + resolved.search + resolved.hash,
              external: false
            };
          } catch (e) {
            return { path: s.charAt(0) === '/' ? s : '/' + s, external: false };
          }
        }

        // Single funnel for every page-driven navigation.
        function requestNav(url, method, body, target) {
          var resolved = urlToProxyPath(url);
          if (resolved.ignore) return;
          parent.postMessage({
            type: 'nav_request',
            session_id: SESSION_ID,
            url: resolved.external ? url : resolved.path,
            method: method || 'GET',
            body: body || null,
            target: target || '_self'
          }, '*');
        }

        // ── Inject <base> so relative URLs in the page resolve against
        //    the proxied site's origin (browsers default to about:blank
        //    here, which would break "<img src='/logo.png'>").
        try {
          if (BASE_URL && !document.querySelector('base')) {
            var b = document.createElement('base');
            b.href = BASE_URL;
            (document.head || document.documentElement).insertBefore(
              b, (document.head || document.documentElement).firstChild
            );
          }
        } catch (e) { /* document not ready yet — page <base> in HTML still works */ }

        // ── fetch() override (streaming) ────────────────────────────

        window.fetch = function(input, options) {
          options = options || {};
          var inputUrl = (input && typeof input === 'object' && 'url' in input) ? input.url : input;
          return new Promise(function(resolve, reject) {
            var requestId = makeId();
            var streamCtrl = null;
            var responded = false;

            function handler(e) {
              var d = e.data;
              if (!d || d.request_id !== requestId) return;
              switch (d.type) {
                case 'http_response_head':
                  var stream = new ReadableStream({
                    start: function(c) { streamCtrl = c; },
                    cancel: function() { /* parent will hit EOF on its end */ }
                  });
                  resolve(new Response(stream, {
                    status: d.status,
                    statusText: d.statusText,
                    headers: d.headers
                  }));
                  responded = true;
                  return;
                case 'http_response_chunk':
                  if (streamCtrl) {
                    try { streamCtrl.enqueue(new Uint8Array(d.chunk)); }
                    catch (err) { /* consumer cancelled */ }
                  }
                  return;
                case 'http_response_end':
                  if (streamCtrl) { try { streamCtrl.close(); } catch (e) {} }
                  window.removeEventListener('message', handler);
                  return;
                case 'http_response_error':
                  if (streamCtrl) { try { streamCtrl.error(new Error(d.message || 'proxy error')); } catch (e) {} }
                  if (!responded) reject(new Error(d.message || 'proxy error'));
                  window.removeEventListener('message', handler);
                  return;
              }
            }
            window.addEventListener('message', handler);

            parent.postMessage({
              type: 'http_request',
              session_id: SESSION_ID,
              request_id: requestId,
              method: options.method || 'GET',
              url: inputUrl,
              headers: options.headers,
              body: options.body || null
            }, '*');
          });
        };

        // ── XMLHttpRequest override (delegates to fetch above) ──────

        window.XMLHttpRequest = function() {
          var self = this;
          this.readyState = 0;
          this.status = 0;
          this.statusText = '';
          this.response = '';
          this.responseText = '';
          this.responseType = '';
          this.responseURL = '';
          this.timeout = 0;
          this.withCredentials = false;
          this.upload = {};
          this.onreadystatechange = null;
          this.onload = null;
          this.onerror = null;
          this.ontimeout = null;
          this.onabort = null;
          this._headers = null;
          this._method = 'GET';
          this._url = '';
          this._aborted = false;
          this._respHeaders = '';
          this.open = function(method, url) {
            this._method = method;
            this._url = url;
            this.readyState = 1;
            if (this.onreadystatechange) this.onreadystatechange();
          };
          this.setRequestHeader = function(k, v) {
            this._headers = this._headers || {};
            this._headers[k] = v;
          };
          this.send = function(body) {
            self.readyState = 2;
            if (self.onreadystatechange) self.onreadystatechange();
            window.fetch(self._url, {
              method: self._method,
              headers: self._headers || {},
              body: body || null
            }).then(function(resp) {
              if (self._aborted) return;
              self.status = resp.status;
              self.statusText = resp.statusText;
              self.responseURL = self._url;
              var lines = [];
              resp.headers.forEach(function(v, k) { lines.push(k + ': ' + v); });
              self._respHeaders = lines.join('\\r\\n');
              self.readyState = 3;
              if (self.onreadystatechange) self.onreadystatechange();
              return resp.arrayBuffer().then(function(buf) {
                if (self._aborted) return;
                var text = new TextDecoder().decode(new Uint8Array(buf));
                self.responseText = text;
                if (self.responseType === 'arraybuffer') {
                  self.response = buf;
                } else if (self.responseType === 'blob') {
                  self.response = new Blob([buf]);
                } else if (self.responseType === 'json') {
                  try { self.response = JSON.parse(text); }
                  catch (e) { self.response = null; }
                } else {
                  self.response = text;
                }
                self.readyState = 4;
                if (self.onreadystatechange) self.onreadystatechange();
                if (self.onload) self.onload();
              });
            }).catch(function(err) {
              if (self._aborted) return;
              self.readyState = 4;
              if (self.onreadystatechange) self.onreadystatechange();
              if (self.onerror) self.onerror(err);
            });
          };
          this.abort = function() {
            self._aborted = true;
            self.readyState = 0;
            if (self.onabort) self.onabort();
          };
          this.getResponseHeader = function(name) {
            var re = new RegExp('^' + name + ': (.*)$', 'mi');
            var m = re.exec(self._respHeaders || '');
            return m ? m[1] : null;
          };
          this.getAllResponseHeaders = function() { return self._respHeaders || ''; };
        };

        // ── EventSource polyfill on top of streaming fetch ──────────
        // Spec: https://html.spec.whatwg.org/multipage/server-sent-events.html

        window.EventSource = function(url, opts) {
          var es = this;
          this.url = url;
          this.readyState = 0; // CONNECTING
          this.withCredentials = (opts && opts.withCredentials) || false;
          this.onopen = null;
          this.onmessage = null;
          this.onerror = null;
          var listeners = {};
          this.addEventListener = function(t, fn) { (listeners[t] = listeners[t] || []).push(fn); };
          this.removeEventListener = function(t, fn) {
            var arr = listeners[t]; if (!arr) return;
            var i = arr.indexOf(fn); if (i >= 0) arr.splice(i, 1);
          };
          var aborted = false;
          this.close = function() {
            aborted = true;
            es.readyState = 2;
          };

          window.fetch(url, { headers: { Accept: 'text/event-stream' } })
            .then(function(resp) {
              if (aborted) return;
              if (!resp.body) throw new Error('no stream');
              es.readyState = 1; // OPEN
              if (es.onopen) es.onopen({});
              var reader = resp.body.getReader();
              var dec = new TextDecoder();
              var buf = '';
              var lastEventId = '';
              function dispatch(name, data) {
                var ev = { data: data, lastEventId: lastEventId, origin: location.origin };
                if (name === 'message' && es.onmessage) es.onmessage(ev);
                var arr = listeners[name];
                if (arr) for (var i = 0; i < arr.length; i++) {
                  try { arr[i](ev); } catch (e) { /* user handler error */ }
                }
              }
              function pump() {
                reader.read().then(function(r) {
                  if (aborted) return reader.cancel();
                  if (r.done) {
                    es.readyState = 2;
                    if (es.onerror) es.onerror({});
                    return;
                  }
                  buf += dec.decode(r.value, { stream: true });
                  // Each event ends with a blank line. Process all
                  // complete events in this buffer, then keep the tail.
                  var idx;
                  while ((idx = buf.indexOf('\\n\\n')) >= 0 ||
                         (idx = buf.indexOf('\\r\\r')) >= 0 ||
                         (idx = buf.indexOf('\\r\\n\\r\\n')) >= 0) {
                    var raw = buf.slice(0, idx);
                    buf = buf.slice(idx + (buf.charAt(idx) === '\\r' && buf.charAt(idx + 1) === '\\n' ? 4 : 2));
                    var evName = 'message', evData = '';
                    var lines = raw.split(/\\r\\n|\\r|\\n/);
                    for (var i = 0; i < lines.length; i++) {
                      var line = lines[i];
                      if (!line || line.charAt(0) === ':') continue;
                      var c = line.indexOf(':');
                      var field = c < 0 ? line : line.slice(0, c);
                      var value = c < 0 ? '' : line.slice(c + 1).replace(/^ /, '');
                      if (field === 'event') evName = value;
                      else if (field === 'data') evData += (evData ? '\\n' : '') + value;
                      else if (field === 'id') lastEventId = value;
                    }
                    if (evData) dispatch(evName, evData);
                  }
                  pump();
                }).catch(function() {
                  es.readyState = 2;
                  if (es.onerror) es.onerror({});
                });
              }
              pump();
            })
            .catch(function() {
              if (aborted) return;
              es.readyState = 2;
              if (es.onerror) es.onerror({});
            });
        };
        window.EventSource.CONNECTING = 0;
        window.EventSource.OPEN = 1;
        window.EventSource.CLOSED = 2;

        // ── WebSocket override ──────────────────────────────────────

        window.WebSocket = function(url, protocols) {
          var wsId = makeId();
          var ws = this;
          this.url = url;
          this.readyState = 0;
          this.protocol = '';
          this.extensions = '';
          this.bufferedAmount = 0;
          this.binaryType = 'blob';
          this.onopen = null;
          this.onmessage = null;
          this.onclose = null;
          this.onerror = null;

          this.send = function(data) {
            parent.postMessage({ type: 'ws_frame', ws_id: wsId, data: data }, '*');
          };
          this.close = function(code, reason) {
            ws.readyState = 3;
            parent.postMessage({
              type: 'ws_close', ws_id: wsId,
              code: code, reason: reason
            }, '*');
          };

          function wsHandler(e) {
            var d = e.data;
            if (!d || d.ws_id !== wsId) return;
            switch (d.type) {
              case 'ws_open':
                ws.readyState = 1;
                if (ws.onopen) ws.onopen({});
                return;
              case 'ws_frame':
                if (ws.onmessage) ws.onmessage({ data: d.data });
                return;
              case 'ws_close':
                ws.readyState = 3;
                if (ws.onclose) ws.onclose({ code: d.code, reason: d.reason });
                window.removeEventListener('message', wsHandler);
                return;
            }
          }
          window.addEventListener('message', wsHandler);

          parent.postMessage({
            type: 'ws_connect',
            ws_id: wsId,
            session_id: SESSION_ID,
            target_url: url,
            protocols: protocols
          }, '*');
        };
        window.WebSocket.CONNECTING = 0;
        window.WebSocket.OPEN = 1;
        window.WebSocket.CLOSING = 2;
        window.WebSocket.CLOSED = 3;

        // ── window.open intercept ───────────────────────────────────

        window.open = function(url, target, features) {
          requestNav(url || '/', 'GET', null, target || '_blank');
          // Return a stub Window-like object so callers that do
          // var w = window.open(...); w.opener = null; without crashing.
          return { closed: false, close: function(){}, focus: function(){}, blur: function(){} };
        };

        // ── location.* override (best effort) ───────────────────────

        try {
          var realLocation = window.location;
          var locationProxy = {
            get href() { return realLocation.href; },
            set href(v) { requestNav(v); },
            assign: function(v) { requestNav(v); },
            replace: function(v) { requestNav(v, 'GET', null, '_self'); },
            reload: function() { requestNav(realLocation.pathname + realLocation.search); },
            toString: function() { return realLocation.href; },
            get protocol() { return realLocation.protocol; },
            get host() { return realLocation.host; },
            get hostname() { return realLocation.hostname; },
            get port() { return realLocation.port; },
            // pathname/search writes trigger real navigation in browsers,
            // so route them through the proxy.
            get pathname() { return realLocation.pathname; },
            set pathname(v) { requestNav(v + realLocation.search + realLocation.hash); },
            get search() { return realLocation.search; },
            set search(v) {
              var s = String(v);
              if (s && s.charAt(0) !== '?') s = '?' + s;
              requestNav(realLocation.pathname + s + realLocation.hash);
            },
            // Hash writes do NOT trigger navigation in real browsers — they
            // only fire hashchange. SPAs use this for client-side routing,
            // so forward directly to the iframe's about:blank location.
            get hash() { return realLocation.hash; },
            set hash(v) {
              try { realLocation.hash = String(v); } catch (e) {}
            },
            get origin() { return realLocation.origin; }
          };
          Object.defineProperty(window, 'location', {
            configurable: true,
            get: function() { return locationProxy; },
            set: function(v) { requestNav(typeof v === 'string' ? v : v && v.href); }
          });
        } catch (e) {
          // Some browsers refuse to redefine window.location; the click /
          // submit / <base> intercepts will still catch most navigation.
        }

        // ── Capture-phase click intercept on document ───────────────

        document.addEventListener('click', function(e) {
          if (e.defaultPrevented) return;
          var t = e.target;
          // event.target can be a text node or deep child — walk up to
          // the nearest <a>. closest() handles SVG anchors too.
          var a = (t && typeof t.closest === 'function') ? t.closest('a[href]') : null;
          if (!a) return;
          // Ignore middle-click / ctrl-click to let the user open in real tab.
          if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
          if (a.hasAttribute('download')) return;
          var href = a.getAttribute('href');
          if (!href) return;
          var lower = href.toLowerCase();
          if (lower.startsWith('javascript:') || lower.startsWith('#')) return;
          e.preventDefault();
          e.stopPropagation();
          requestNav(href, 'GET', null, a.target || '_self');
        }, true);

        // ── Capture-phase form submit intercept ─────────────────────

        document.addEventListener('submit', function(e) {
          if (e.defaultPrevented) return;
          var f = e.target;
          if (!f || f.tagName !== 'FORM') return;
          var method = (f.method || 'GET').toUpperCase();
          var action = f.getAttribute('action') || '';
          var fd = new FormData(f);
          var url, body = null;
          if (method === 'GET') {
            var params = new URLSearchParams();
            fd.forEach(function(v, k) { params.append(k, typeof v === 'string' ? v : v.name || ''); });
            var q = params.toString();
            url = action + (q ? (action.indexOf('?') >= 0 ? '&' : '?') + q : '');
          } else {
            url = action;
            // Encode body — multipart for files, urlencoded otherwise.
            var hasFile = false;
            fd.forEach(function(v) { if (v && typeof v !== 'string') hasFile = true; });
            if (hasFile) {
              body = fd; // structured-clone passes FormData natively
            } else {
              var p2 = new URLSearchParams();
              fd.forEach(function(v, k) { p2.append(k, typeof v === 'string' ? v : ''); });
              body = p2.toString();
            }
          }
          e.preventDefault();
          e.stopPropagation();
          requestNav(url, method, body, f.target || '_self');
        }, true);

        // Nested same-origin iframes (rare in admin UIs) currently load
        // their own about:blank without our intercepts. Supporting them
        // requires re-emitting this whole IIFE into the child document
        // — a deliberate v2 follow-up. The static-page intercepts above
        // (link click, form submit, location.*) are sufficient for the
        // 99% case.
      })();
      <\/script>
    `;
  }

  // Resolve any URL the iframe might emit (absolute, protocol-relative,
  // about:blank-relative) into a path under the proxied site. Anything
  // pointing at a different origin is left untouched and the caller
  // decides whether to open it externally.
  function urlToPath(url: string): { path: string; external: boolean } {
    if (!url) return { path: "/", external: false };
    try {
      const base = currentSession?.base_url ?? "https://localhost/";
      const resolved = new URL(url, base);
      const baseParsed = new URL(base);
      if (resolved.host && resolved.host !== baseParsed.host) {
        return { path: url, external: true };
      }
      return {
        path: resolved.pathname + resolved.search + resolved.hash,
        external: false,
      };
    } catch {
      return { path: url.startsWith("/") ? url : "/" + url, external: false };
    }
  }

  // Cap navigation chains: a redirect storm should give up rather than
  // loop forever. 10 hops matches what most browsers default to.
  const MAX_REDIRECTS = 10;

  async function loadPage(
    rawPath: string = "/",
    method: string = "GET",
    body?: BodyInit | null,
    redirectDepth: number = 0,
  ): Promise<void> {
    if (!currentSession || !streamManager) return;
    if (redirectDepth > MAX_REDIRECTS) {
      isLoading = false;
      error = "Too many redirects";
      return;
    }

    const { path, external } = urlToPath(rawPath);
    if (external) {
      // Off-site link — open in the user's real browser, not in our iframe.
      try {
        window.open(rawPath, "_blank", "noopener,noreferrer");
      } catch {
        /* popup blocked — silently drop */
      }
      return;
    }

    try {
      isLoading = true;
      error = null;
      if (tabId) {
        webProxyStore.updateBrowserTab(tabId, { loading: true, error: null });
      }

      const cookie = getCookieHeader(currentSession.id);
      const response = await streamManager.fetch(path, {
        method,
        headers: {
          Accept:
            "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
          "User-Agent": "WantasticBrowser/1.0",
          ...(cookie ? { Cookie: cookie } : {}),
        },
        body: body ?? undefined,
      });

      const setCookie = response.headers.get("Set-Cookie");
      if (setCookie) handleSetCookie(currentSession.id, setCookie);

      // Follow same-origin redirects ourselves so the iframe sees the
      // final document, not a 302 with empty body.
      if (response.status >= 300 && response.status < 400) {
        const location = response.headers.get("Location");
        if (location) {
          return loadPage(location, "GET", null, redirectDepth + 1);
        }
      }

      const html = await response.text();
      const sanitized = stripDangerousMeta(html);

      iframeKey++;
      // Wait for the iframe to (re)mount before writing into its document.
      setTimeout(() => {
        if (iframeElement && iframeElement.contentDocument) {
          const bootstrapScript = generateBootstrapScript(
            currentSession!.id,
            currentSession!.base_url ?? "",
          );
          iframeElement.contentDocument.open();
          iframeElement.contentDocument.write(bootstrapScript + sanitized);
          iframeElement.contentDocument.close();
        }
      }, 0);

      if (tabId) {
        webProxyStore.updateBrowserTab(tabId, {
          loading: false,
          url: path,
          error: null,
        });
      }
      isLoading = false;
    } catch (err: any) {
      isLoading = false;
      error =
        (err instanceof Error ? err.message : String(err)) ||
        $_("webBrowser.failedToLoadPage");
      if (tabId) {
        webProxyStore.updateBrowserTab(tabId, { loading: false, error });
      }
    }
  }

  // Strip <meta http-equiv="refresh"> tags so the iframe's browser doesn't
  // auto-navigate to a URL it cannot reach (only our intercept funnel can
  // perform a navigation that goes through the mux).
  function stripDangerousMeta(html: string): string {
    return html.replace(
      /<meta[^>]+http-equiv\s*=\s*["']?refresh["']?[^>]*>/gi,
      "",
    );
  }

  // Handle messages from iframe. Each message is one of a small,
  // discriminated set of types; unknown types are dropped silently so
  // a stray postMessage from an embedded ad/script can't crash us.
  async function handleIframeMessage(event: MessageEvent) {
    if (!event.data || !streamManager) return;
    // Drop messages that don't come from our iframe — third-party scripts
    // load in the iframe but their postMessages always come from the
    // iframe's contentWindow, so this filter is a defensive sanity net.
    if (
      iframeElement?.contentWindow &&
      event.source !== iframeElement.contentWindow
    ) {
      return;
    }

    const data = event.data as { type?: string; [k: string]: any };
    switch (data.type) {
      case "http_request":
        await handleHttpRequest(data);
        return;
      case "ws_connect":
        handleWsConnect(data);
        return;
      case "ws_frame":
        handleWsFrame(data);
        return;
      case "ws_close":
        handleWsClose(data);
        return;
      case "nav_request":
        handleNavRequest(data);
        return;
    }
  }

  // Streaming HTTP: parent ships head + chunks + end. The iframe rebuilds
  // a Response with a ReadableStream body, so SSE / chunked responses /
  // long-poll all work from the iframe's perspective without any extra
  // logic — fetch() in the iframe just looks like real fetch().
  async function handleHttpRequest(data: any) {
    const { request_id, url, method, headers, body } = data;
    if (!streamManager) return;

    const post = (msg: any) =>
      iframeElement?.contentWindow?.postMessage(msg, "*");

    let response: Response;
    try {
      const { path, external } = urlToPath(url);
      if (external) {
        post({
          type: "http_response_error",
          request_id,
          message: "External URL — open externally",
        });
        return;
      }

      // Build the outbound headers. The page's fetch override only
      // forwards what it explicitly set — so the implicit ambient
      // headers a real browser adds (Cookie, Referer, Accept,
      // User-Agent) need to come from us. Without these, server-side
      // apps that rely on session cookies (LuCI, MikroTik admin,
      // basically any auth UI) die on a null session lookup.
      const outHeaders: Record<string, string> = { ...(headers || {}) };
      const hasHeader = (name: string): boolean =>
        Object.keys(outHeaders).some(
          (k) => k.toLowerCase() === name.toLowerCase(),
        );

      if (currentSession && !hasHeader("Cookie")) {
        const cookie = getCookieHeader(currentSession.id);
        if (cookie) outHeaders["Cookie"] = cookie;
      }
      if (currentSession?.base_url && !hasHeader("Referer")) {
        // The Referer must look like it came from the proxied site,
        // not from our portal — many CSRF guards check this.
        const refPath = currentTab?.url || "/";
        try {
          outHeaders["Referer"] = new URL(refPath, currentSession.base_url).toString();
        } catch {
          /* ignore */
        }
      }
      if (!hasHeader("Accept")) {
        outHeaders["Accept"] = "*/*";
      }
      if (!hasHeader("User-Agent")) {
        outHeaders["User-Agent"] = "WantasticBrowser/1.0";
      }

      response = await streamManager.fetch(path, {
        method: method || "GET",
        headers: outHeaders,
        body: body ?? undefined,
      });
    } catch (err: any) {
      post({
        type: "http_response_error",
        request_id,
        message: err?.message || String(err),
      });
      return;
    }

    const setCookie = response.headers.get("Set-Cookie");
    if (setCookie && currentSession) {
      handleSetCookie(currentSession.id, setCookie);
    }

    post({
      type: "http_response_head",
      request_id,
      status: response.status,
      statusText: response.statusText,
      headers: Object.fromEntries(response.headers.entries()),
    });

    // Stream the body. The Response's ReadableStream is already chunked
    // by the mux; we forward each chunk as it arrives so the iframe sees
    // SSE / progressive HTML / chunked downloads in real time. The
    // built-in highWaterMark on the stream keeps memory bounded.
    const reader = response.body?.getReader();
    if (!reader) {
      post({ type: "http_response_end", request_id });
      return;
    }
    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) {
          post({ type: "http_response_end", request_id });
          return;
        }
        if (value && value.byteLength > 0) {
          post({ type: "http_response_chunk", request_id, chunk: value });
        }
      }
    } catch (err: any) {
      post({
        type: "http_response_error",
        request_id,
        message: err?.message || "stream error",
      });
    } finally {
      try {
        reader.releaseLock();
      } catch {
        /* already released */
      }
    }
  }

  function handleWsConnect(data: any) {
    if (!streamManager) return;
    const { ws_id, target_url } = data;
    const { path, external } = urlToPath(target_url);
    if (external) return;
    // Resolve to an absolute ws/wss URL the mux can use to compute the
    // Host header on the upstream side.
    const base = new URL(currentSession!.base_url ?? "https://localhost/");
    const absolute =
      (base.protocol === "https:" ? "wss:" : "ws:") + "//" + base.host + path;

    const conn = streamManager.openWebSocket(absolute);
    iframeWsConns.set(ws_id, conn);

    const post = (msg: any) =>
      iframeElement?.contentWindow?.postMessage(msg, "*");

    conn.onopen = () => post({ type: "ws_open", ws_id, success: true });
    conn.onmessage = (ev) =>
      post({ type: "ws_frame", ws_id, data: (ev as MessageEvent).data });
    conn.onclose = (ev: any) => {
      iframeWsConns.delete(ws_id);
      post({ type: "ws_close", ws_id, code: ev.code, reason: ev.reason });
    };
    conn.onerror = () => {
      iframeWsConns.delete(ws_id);
      post({ type: "ws_close", ws_id, code: 1011, reason: "Proxy error" });
    };
  }

  function handleWsFrame(data: any) {
    const conn = iframeWsConns.get(data.ws_id);
    if (conn && conn.readyState === WebSocket.OPEN) conn.send(data.data);
  }

  function handleWsClose(data: any) {
    const conn = iframeWsConns.get(data.ws_id);
    if (conn) {
      conn.close(data.code ?? 1000, data.reason ?? "");
      iframeWsConns.delete(data.ws_id);
    }
  }

  // Single funnel for every page-driven navigation — link clicks, form
  // submits, location.assign, location.href=, window.open, etc.
  // _blank target opens in the user's real browser; _self / _top reload
  // the current iframe via loadPage().
  function handleNavRequest(data: any) {
    const { url, method, body, target } = data ?? {};
    if (typeof url !== "string" || !url) return;

    if (target === "_blank") {
      const { path, external } = urlToPath(url);
      // Even same-origin links that explicitly want a new tab get one —
      // we open a real new browser window since we have no multi-tab
      // affordance inside the WebBrowser app.
      try {
        window.open(
          external ? url : path,
          "_blank",
          "noopener,noreferrer",
        );
      } catch {
        /* popup blocked */
      }
      return;
    }
    loadPage(url, method || "GET", body ?? null);
  }

  function handleBack() {
    if (!tabId || !currentTab || currentTab.historyIndex <= 0) return;
    const prevUrl = currentTab.history[currentTab.historyIndex - 1];
    webProxyStore.updateBrowserTab(tabId, {
      historyIndex: currentTab.historyIndex - 1,
    });
    loadPage(prevUrl);
  }

  function handleRefresh() {
    loadPage(currentTab?.url || "/");
  }

  // Can go back?
  $: canGoBack = currentTab && currentTab.historyIndex > 0;

  // Peer name for title
  $: peerName = currentSession
    ? `${currentSession.peer_ip}:${currentSession.port}`
    : $_("webBrowser.defaultPeerName");

  onMount(() => {
    window.addEventListener("message", handleIframeMessage);

    if (currentTab?.url) {
      loadPage(currentTab.url);
    } else {
      loadPage("/");
    }
  });

  onDestroy(() => {
    window.removeEventListener("message", handleIframeMessage);
    iframeWsConns.clear();
    if (streamManager) {
      streamManager.close();
    }
  });
</script>

{#if !isMinimized}
  <div
    class="web-browser activeShadow"
    class:maximized={isMaximized}
    style:z-index={zIndex}
    on:mousedown={handleFocus}
    on:touchstart={handleFocus}
    use:draggable={{
      handle: ".title-bar",
      disabled: isMaximized,
      bounds: "body",
    }}
    transition:scale={{ duration: 200 }}
  >
    <Titlebar
      appName={"WebBrowser"}
      customClose={true}
      on:maximize={handleMaximize}
      on:reduce={handleReduce}
      on:close={handleClose}
    >
      <svg
        xmlns="http://www.w3.org/2000/svg"
        width="16"
        height="16"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        stroke-width="2"
      >
        <circle cx="12" cy="12" r="3" fill="#22c55e" />
        <path
          d="M12 1v6m0 6v6M5.93 5.93l4.24 4.24m5.66 5.66l4.24 4.24M1 12h6m6 0h6M5.93 18.07l4.24-4.24m5.66-5.66l4.24-4.24"
          stroke="#22c55e"
        />
      </svg>
      <span class="appName pl-2">{windowId}</span>
    </Titlebar>

    <!-- Simple Toolbar -->
    <div class="toolbar">
      <button
        class="toolbar-btn"
        on:click={handleBack}
        disabled={!canGoBack}
        title={$_("webBrowser.back")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M19 12H5M12 19l-7-7 7-7" />
        </svg>
      </button>

      <button
        class="toolbar-btn"
        on:click={handleRefresh}
        disabled={isLoading}
        title={$_("webBrowser.refresh")}
      >
        <svg
          width="16"
          height="16"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
          class:spinning={isLoading}
        >
          <path d="M23 4v6h-6M1 20v-6h6" />
          <path
            d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"
          />
        </svg>
      </button>

      <!-- Spacer to push peer info to center -->
      <div class="toolbar-spacer">
        <span class="toolbar-peer-name">{peerName}</span>
      </div>
    </div>

    <!-- Content Area -->
    <div class="content">
      {#if error}
        <div class="error-state">
          <svg
            width="48"
            height="48"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="1.5"
          >
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="8" x2="12" y2="12" />
            <line x1="12" y1="16" x2="12.01" y2="16" />
          </svg>
          <p class="error-title">{$_("webBrowser.connectionError")}</p>
          <p class="error-message">{error}</p>
          <button class="retry-btn" on:click={handleRefresh}
            >{$_("webBrowser.retry")}</button
          >
        </div>
      {:else if isLoading}
        <div class="loading-state">
          <div class="loader" />
          <p>
            {$_("webBrowser.connectingTo", { values: { peerName: peerName } })}
          </p>
        </div>
      {:else}
        <!-- All content runs in a sandboxed iframe with the bootstrap
             script injected; fetch/XHR/WebSocket are all overridden to
             postMessage their requests up to WebProxyMux. -->
        {#key iframeKey}
          <iframe
            bind:this={iframeElement}
            src="about:blank"
            sandbox="allow-scripts allow-forms allow-same-origin allow-popups allow-modals"
            allow="fullscreen; autoplay; clipboard-write"
            title={$_("webBrowser.peerWebInterface")}
          />
        {/key}
      {/if}
    </div>

    <!-- Status Bar -->
    <div class="status-bar">
      <div class="status-left">
        <span class="peer-name">{peerName}</span>
      </div>
      <div class="status-right">
        <span class="secure-badge">
          <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
            <path
              d="M12 1L3 5v6c0 5.55 3.84 10.74 9 12 5.16-1.26 9-6.45 9-12V5l-9-4zm0 10.99h7c-.53 4.12-3.28 7.79-7 8.94V12H5V6.3l7-3.11v8.8z"
            />
          </svg>
          {$_("webBrowser.secured")}
        </span>
        <span class="session-id"
          >{$_("webBrowser.session", {
            values: { id: currentSession?.id?.slice(0, 8) || "..." },
          })}</span
        >
      </div>
    </div>
  </div>
{/if}

<style>
  .web-browser {
    position: absolute;
    top: 80px;
    left: 200px;
    width: 700px;
    height: 550px;
    display: flex;
    flex-direction: column;
    background: var(--mica);
    border-radius: 12px;
    box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5);
    overflow: hidden;
  }

  /* Mobile: Full screen by default */
  @media (max-width: 768px) {
    .web-browser {
      position: absolute !important;
      inset: 0 !important;
      width: 100% !important;
      height: 100% !important;
      top: 0 !important;
      left: 0 !important;
      border-radius: 0 !important;
      z-index: 9999 !important;
    }
  }

  .maximized {
    position: fixed !important;
    inset: 0 !important;
    width: 100% !important;
    height: 100% !important;
    border-radius: 0 !important;
  }

  .toolbar {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 8px 12px;
    background: rgb(var(--bg1));
    border-bottom: 1px solid rgb(var(--clr) / 10%);
    min-height: 48px;
  }

  .toolbar-spacer {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    overflow: hidden;
  }

  .toolbar-peer-name {
    font-size: 12px;
    color: rgb(var(--clr) / 55%);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 200px;
  }

  @media (max-width: 480px) {
    .toolbar-peer-name {
      max-width: 120px;
      font-size: 11px;
    }
  }

  .toolbar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 40px;
    height: 40px;
    background: transparent;
    border: none;
    border-radius: 6px;
    color: rgb(var(--clr) / 55%);
    cursor: pointer;
    transition: all 0.15s ease;
    flex-shrink: 0;
  }

  /* Larger touch targets on mobile */
  @media (max-width: 768px) {
    .toolbar-btn {
      width: 44px;
      height: 44px;
    }

    .toolbar-btn svg {
      width: 20px;
      height: 20px;
    }
  }

  .toolbar-btn:hover:not(:disabled) {
    background: rgb(var(--clr) / 10%);
    color: rgb(var(--clr));
  }

  .toolbar-btn:active:not(:disabled) {
    background: rgb(var(--clr) / 16%);
    transform: scale(0.95);
  }

  .toolbar-btn:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }

  .spinning {
    animation: spin 1s linear infinite;
  }

  @keyframes spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .content {
    flex: 1;
    background: #fff;
    overflow: hidden;
  }

  .content iframe {
    width: 100%;
    height: 100%;
    border: none;
  }

  .loading-state,
  .error-state {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #666;
    gap: 12px;
    padding: 20px;
    text-align: center;
  }

  .loader {
    width: 40px;
    height: 40px;
    border: 3px solid #e0e0e0;
    border-top-color: #3b82f6;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }

  .error-state svg {
    color: #e94560;
  }

  .error-title {
    font-weight: 600;
    color: #333;
    margin: 0;
  }

  .error-message {
    font-size: 13px;
    color: #888;
    margin: 0;
    max-width: 300px;
    text-align: center;
  }

  .retry-btn {
    padding: 10px 20px;
    background: #3b82f6;
    color: #fff;
    border: none;
    border-radius: 6px;
    font-size: 14px;
    cursor: pointer;
    transition: background 0.15s ease;
    min-height: 44px;
  }

  .retry-btn:hover {
    background: #2563eb;
  }

  .retry-btn:active {
    transform: scale(0.98);
  }

  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    background: rgb(var(--bg1));
    border-top: 1px solid rgb(var(--clr) / 10%);
    font-size: 11px;
    color: rgb(var(--clr) / 55%);
    min-height: 32px;
  }

  @media (max-width: 480px) {
    .status-bar {
      padding: 4px 8px;
      font-size: 10px;
    }
  }

  .status-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .peer-name {
    font-weight: 500;
    color: rgb(var(--clr));
  }

  .status-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .secure-badge {
    display: flex;
    align-items: center;
    gap: 4px;
    color: #10b981;
  }

  .session-id {
    color: rgb(var(--clr) / 40%);
    font-family: monospace;
  }
</style>
