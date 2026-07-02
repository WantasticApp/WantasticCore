/**
 * WebProxyMux — frontend client for the webproxy multiplexer.
 *
 * One mux per webproxy session. Each logical HTTP request or proxied
 * WebSocket is a virtual stream identified by request_id. All traffic
 * rides binary WS frames (see wsStore.sendWebProxyFrame); body bytes
 * are raw — no base64 in either direction.
 *
 * Wire format (mirrors cmd/web/portal/pkg/wsgrpc/webproxy_bridge.go):
 *
 *   [version=1][frameType][sessionIdLen:2 BE][sessionId]
 *     [headerJsonLen:4 BE][headerJson][bodyBytes]
 *
 * The headerJson is one of these shapes, keyed on `kind`:
 *
 *   request   — { kind, request_id, method, path, query, headers }
 *   response  — { kind, request_id, status_code, status_text, headers,
 *                 content_type, content_length, is_final, chunk_index }
 *   ws_frame  — { kind, request_id, ws_type }
 *   error     — { kind, request_id, code, message, retryable }
 *   ping      — { kind, request_id, timestamp }
 *
 * Response bodies stream chunk-by-chunk into a ReadableStream — we never
 * accumulate the whole body in JS, so 1 GB downloads don't OOM the tab.
 */

import { wsStore } from "./websocket";

// ── Wire types ────────────────────────────────────────────────────────────

type ResponseHeader = {
  kind: "response";
  request_id: string;
  status_code?: number;
  status_text?: string;
  headers?: Record<string, string>;
  content_type?: string;
  content_length?: number;
  is_final?: boolean;
  chunk_index?: number;
};

type WSFrameHeader = {
  kind: "ws_frame";
  request_id: string;
  ws_type?: number;
};

type ErrorHeader = {
  kind: "error";
  request_id: string;
  code?: string;
  message?: string;
  retryable?: boolean;
};

type PingHeader = {
  kind: "ping";
  request_id: string;
  timestamp?: number;
};

type IncomingHeader = ResponseHeader | WSFrameHeader | ErrorHeader | PingHeader;

// ── Internal request state ────────────────────────────────────────────────

interface PendingRequest {
  controller: ReadableStreamDefaultController<Uint8Array>;
  responseInit: ResponseInit | null;
  resolveResponse: (resp: Response) => void;
  rejectResponse: (err: Error) => void;
  responseDelivered: boolean;
  timeout: ReturnType<typeof setTimeout> | null;
  closed: boolean;
}

interface WSConnState {
  readyState: number;
  onopen?: () => void;
  onmessage?: (data: ArrayBuffer | string) => void;
  onclose?: (ev: { code: number; reason: string }) => void;
  onerror?: (err: Error) => void;
}

// ── Manager ───────────────────────────────────────────────────────────────

const EMPTY_BODY = new Uint8Array(0);
const utf8 = new TextEncoder();
const utf8dec = new TextDecoder();

export class WebProxyMux {
  private readonly sessionId: string;
  private readonly pending = new Map<string, PendingRequest>();
  private readonly wsConns = new Map<string, WSConnState>();
  private readonly boundListener: (frame: {
    sessionId: string;
    headerJson: string;
    body: Uint8Array;
  }) => void;

  constructor(sessionId: string) {
    this.sessionId = sessionId;
    this.boundListener = (f) => {
      if (f.sessionId !== this.sessionId) return;
      this.handleIncoming(f.headerJson, f.body);
    };
    wsStore.registerWebProxyFrameListener(this.boundListener);
  }

  // ── Public API ─────────────────────────────────────────────────────────

  /**
   * Make an HTTP request through the mux. Returns a fetch-style Response
   * whose body is a ReadableStream — chunks arrive as the backend streams
   * them, so the caller can pipe straight to a Blob, an iframe, or
   * TransformStream without buffering the whole body.
   *
   * timeoutMs only applies to the **headers**: once the first response
   * frame arrives, the timer is cleared and the body can stream as long
   * as the upstream needs.
   */
  async fetch(
    path: string,
    options?: RequestInit & { timeout?: number }
  ): Promise<Response> {
    const requestId = WebProxyMux.genId();
    const timeoutMs = options?.timeout ?? 30_000;

    const bodyBytes = await WebProxyMux.bodyBytes(options?.body);
    const headers = WebProxyMux.normalizeHeaders(options?.headers);
    const { pathOnly, query } = WebProxyMux.splitQuery(path);

    return new Promise<Response>((resolve, reject) => {
      let controller!: ReadableStreamDefaultController<Uint8Array>;
      const stream = new ReadableStream<Uint8Array>({
        start(c) {
          controller = c;
        },
      });

      const pending: PendingRequest = {
        controller,
        responseInit: null,
        resolveResponse: resolve,
        rejectResponse: reject,
        responseDelivered: false,
        timeout: null,
        closed: false,
      };

      pending.timeout = setTimeout(() => {
        if (pending.responseDelivered) return;
        this.failRequest(
          requestId,
          new Error(`WebProxy: response timeout after ${timeoutMs}ms`)
        );
      }, timeoutMs);

      this.pending.set(requestId, pending);

      const header = JSON.stringify({
        kind: "request",
        request_id: requestId,
        method: options?.method ?? "GET",
        path: pathOnly,
        query,
        headers,
      });

      // Stash the stream on the request so handleResponse can deliver
      // a Response on the first chunk and then keep pumping.
      (pending as any).stream = stream;

      if (!wsStore.sendWebProxyFrame(this.sessionId, header, bodyBytes)) {
        this.failRequest(requestId, new Error("WebProxy: socket not open"));
      }
    });
  }

  /**
   * Open a proxied WebSocket. Returns a thin wrapper that mirrors the
   * native WebSocket surface (send/close/onopen/onmessage/onclose).
   *
   * The backend receives an HTTP request with the WebSocket upgrade
   * headers; on success it sends a 101 response (which we treat as
   * "connection open") and frames flow over the same request_id.
   */
  openWebSocket(targetUrl: string): ProxyWebSocket {
    const requestId = WebProxyMux.genId();
    const conn: WSConnState = { readyState: WebSocket.CONNECTING };
    this.wsConns.set(requestId, conn);

    const url = new URL(targetUrl);
    const headers: Record<string, string> = {
      Upgrade: "websocket",
      Connection: "Upgrade",
      "Sec-WebSocket-Version": "13",
      Host: url.host,
    };
    const header = JSON.stringify({
      kind: "request",
      request_id: requestId,
      method: "GET",
      path: url.pathname,
      query: url.search.replace(/^\?/, ""),
      headers,
    });
    wsStore.sendWebProxyFrame(this.sessionId, header, EMPTY_BODY);

    return new ProxyWebSocket(requestId, targetUrl, this);
  }

  /** Tear down the mux and release all in-flight requests. */
  close() {
    wsStore.unregisterWebProxyFrameListener(this.boundListener);
    for (const [id, p] of this.pending) {
      if (p.timeout) clearTimeout(p.timeout);
      this.failRequest(id, new Error("WebProxyMux closed"));
    }
    this.pending.clear();
    for (const [id, conn] of this.wsConns) {
      conn.readyState = WebSocket.CLOSED;
      conn.onclose?.({ code: 1001, reason: "Mux closed" });
      this.wsConns.delete(id);
    }
  }

  // ── Outbound (used by ProxyWebSocket) ──────────────────────────────────

  /** @internal */
  sendWSFrame(requestId: string, opcode: number, data: Uint8Array): void {
    const header = JSON.stringify({
      kind: "ws_frame",
      request_id: requestId,
      ws_type: opcode,
    });
    wsStore.sendWebProxyFrame(this.sessionId, header, data);
  }

  /** @internal */
  closeWS(requestId: string, code = 1000, reason = ""): void {
    // 0x08 = WebSocket Close opcode.
    const data = utf8.encode(reason);
    this.sendWSFrame(requestId, 0x08, data);
    const conn = this.wsConns.get(requestId);
    if (conn) {
      conn.readyState = WebSocket.CLOSED;
      conn.onclose?.({ code, reason });
      this.wsConns.delete(requestId);
    }
  }

  /** @internal */
  attachWS(
    requestId: string,
    onopen: () => void,
    onmessage: (data: ArrayBuffer | string) => void,
    onclose: (ev: { code: number; reason: string }) => void,
    onerror: (err: Error) => void
  ) {
    const conn = this.wsConns.get(requestId);
    if (!conn) return;
    conn.onopen = onopen;
    conn.onmessage = onmessage;
    conn.onclose = onclose;
    conn.onerror = onerror;
  }

  // ── Inbound dispatch ───────────────────────────────────────────────────

  private handleIncoming(headerJson: string, body: Uint8Array) {
    let h: IncomingHeader;
    try {
      h = JSON.parse(headerJson) as IncomingHeader;
    } catch {
      return;
    }
    switch (h.kind) {
      case "response":
        this.handleResponse(h, body);
        break;
      case "ws_frame":
        this.handleWSFrame(h, body);
        break;
      case "error":
        this.handleError(h);
        break;
      case "ping":
        // Ping is connection-level; bridge already pongs, ignore.
        break;
    }
  }

  private handleResponse(h: ResponseHeader, body: Uint8Array) {
    const pending = this.pending.get(h.request_id);
    if (!pending) return;

    // First chunk: build Response with the streaming body and resolve.
    if (!pending.responseDelivered) {
      // 101 Switching Protocols → WebSocket open
      if (h.status_code === 101) {
        const conn = this.wsConns.get(h.request_id);
        if (conn) {
          conn.readyState = WebSocket.OPEN;
          conn.onopen?.();
        }
        // No body; this request_id now belongs to the WS pump.
        pending.responseDelivered = true;
        if (pending.timeout) clearTimeout(pending.timeout);
        // Resolve with a dummy 101 response so the caller's await
        // resolves; but ProxyWebSocket users normally don't await.
        pending.resolveResponse(
          new Response(null, {
            status: 101,
            statusText: h.status_text ?? "Switching Protocols",
            headers: WebProxyMux.flatten(h.headers),
          })
        );
        this.pending.delete(h.request_id);
        return;
      }

      pending.responseInit = {
        status: h.status_code ?? 200,
        statusText: h.status_text ?? "",
        headers: WebProxyMux.flatten(h.headers),
      };
      const stream = (pending as any).stream as ReadableStream<Uint8Array>;
      pending.resolveResponse(new Response(stream, pending.responseInit));
      pending.responseDelivered = true;
      if (pending.timeout) {
        clearTimeout(pending.timeout);
        pending.timeout = null;
      }
    }

    if (body.length > 0 && !pending.closed) {
      try {
        pending.controller.enqueue(body);
      } catch {
        /* stream already closed by consumer */
      }
    }
    if (h.is_final) {
      pending.closed = true;
      try {
        pending.controller.close();
      } catch {
        /* already closed */
      }
      this.pending.delete(h.request_id);
    }
  }

  private handleWSFrame(h: WSFrameHeader, body: Uint8Array) {
    const conn = this.wsConns.get(h.request_id);
    if (!conn) return;
    const opcode = h.ws_type ?? 1;
    switch (opcode) {
      case 1: // text
        conn.onmessage?.(utf8dec.decode(body));
        return;
      case 2: { // binary
        const payload = new Uint8Array(body.byteLength);
        payload.set(body);
        conn.onmessage?.(payload.buffer);
        return;
      }
      case 8: // close
        conn.readyState = WebSocket.CLOSED;
        conn.onclose?.({ code: 1000, reason: utf8dec.decode(body) });
        this.wsConns.delete(h.request_id);
        return;
      // ping/pong are handled at the gorilla layer; ignore here.
    }
  }

  private handleError(h: ErrorHeader) {
    const err = new Error(`${h.code ?? "ERROR"}: ${h.message ?? ""}`);
    this.failRequest(h.request_id, err);
    const conn = this.wsConns.get(h.request_id);
    if (conn) {
      conn.readyState = WebSocket.CLOSED;
      conn.onerror?.(err);
      conn.onclose?.({ code: 1011, reason: err.message });
      this.wsConns.delete(h.request_id);
    }
  }

  private failRequest(id: string, err: Error) {
    const pending = this.pending.get(id);
    if (!pending) return;
    if (pending.timeout) clearTimeout(pending.timeout);
    if (pending.responseDelivered) {
      try {
        pending.controller.error(err);
      } catch {
        /* already errored */
      }
    } else {
      pending.rejectResponse(err);
    }
    this.pending.delete(id);
  }

  // ── Helpers ────────────────────────────────────────────────────────────

  private static genId(): string {
    return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 11)}`;
  }

  private static splitQuery(path: string): { pathOnly: string; query: string } {
    const i = path.indexOf("?");
    if (i < 0) return { pathOnly: path, query: "" };
    return { pathOnly: path.slice(0, i), query: path.slice(i + 1) };
  }

  private static normalizeHeaders(
    h?: HeadersInit
  ): Record<string, string> | undefined {
    if (!h) return undefined;
    const out: Record<string, string> = {};
    if (h instanceof Headers) {
      h.forEach((v, k) => {
        out[k] = v;
      });
    } else if (Array.isArray(h)) {
      for (const [k, v] of h) out[k] = v;
    } else {
      for (const [k, v] of Object.entries(h)) out[k] = String(v);
    }
    return out;
  }

  private static async bodyBytes(body: BodyInit | null | undefined): Promise<Uint8Array> {
    if (body == null) return EMPTY_BODY;
    if (typeof body === "string") return utf8.encode(body);
    if (body instanceof Uint8Array) return body;
    if (body instanceof ArrayBuffer) return new Uint8Array(body);
    if (ArrayBuffer.isView(body)) {
      return new Uint8Array(body.buffer, body.byteOffset, body.byteLength);
    }
    if (body instanceof Blob) {
      return new Uint8Array(await body.arrayBuffer());
    }
    if (body instanceof FormData || body instanceof URLSearchParams) {
      return utf8.encode(body.toString());
    }
    // ReadableStream and other oddities — drain into a Blob.
    const blob = await new Response(body as any).blob();
    return new Uint8Array(await blob.arrayBuffer());
  }

  private static flatten(h?: Record<string, string>): Headers {
    const out = new Headers();
    if (!h) return out;
    for (const [k, v] of Object.entries(h)) {
      // Set-Cookie in the proto map is "\n"-joined for multi-values
      // (see flattenResponseHeaders in the Go side). Restore them as
      // multiple Headers entries so document.cookie parses correctly.
      if (k.toLowerCase() === "set-cookie" && v.includes("\n")) {
        for (const part of v.split("\n")) out.append(k, part);
      } else {
        out.set(k, v);
      }
    }
    return out;
  }
}

// ── ProxyWebSocket — looks like a native WebSocket, talks via the mux ─

export class ProxyWebSocket implements WebSocket {
  readonly CONNECTING = WebSocket.CONNECTING;
  readonly OPEN = WebSocket.OPEN;
  readonly CLOSING = WebSocket.CLOSING;
  readonly CLOSED = WebSocket.CLOSED;
  readonly url: string;
  readonly protocol = "";
  readonly extensions = "";
  readonly bufferedAmount = 0;
  binaryType: BinaryType = "blob";

  onopen: ((this: WebSocket, ev: Event) => any) | null = null;
  onmessage: ((this: WebSocket, ev: MessageEvent) => any) | null = null;
  onclose: ((this: WebSocket, ev: CloseEvent) => any) | null = null;
  onerror: ((this: WebSocket, ev: Event) => any) | null = null;

  readyState: number = WebSocket.CONNECTING;

  private readonly mux: WebProxyMux;
  private readonly requestId: string;

  constructor(requestId: string, url: string, mux: WebProxyMux) {
    this.requestId = requestId;
    this.url = url;
    this.mux = mux;

    mux.attachWS(
      requestId,
      () => {
        this.readyState = WebSocket.OPEN;
        this.onopen?.call(this as any, new Event("open"));
      },
      (data) => {
        this.onmessage?.call(this as any, new MessageEvent("message", { data }));
      },
      (ev) => {
        this.readyState = WebSocket.CLOSED;
        this.onclose?.call(
          this as any,
          new CloseEvent("close", { code: ev.code, reason: ev.reason })
        );
      },
      (err) => {
        this.onerror?.call(this as any, new ErrorEvent("error", { error: err }));
      }
    );
  }

  send(data: string | ArrayBufferLike | Blob | ArrayBufferView): void {
    if (this.readyState !== WebSocket.OPEN) {
      throw new Error("WebSocket is not open");
    }
    let opcode = 2;
    let bytes: Uint8Array;
    if (typeof data === "string") {
      opcode = 1;
      bytes = utf8.encode(data);
    } else if (data instanceof ArrayBuffer) {
      bytes = new Uint8Array(data);
    } else if (ArrayBuffer.isView(data)) {
      bytes = new Uint8Array(data.buffer, data.byteOffset, data.byteLength);
    } else {
      throw new TypeError("Unsupported WebSocket payload type");
    }
    this.mux.sendWSFrame(this.requestId, opcode, bytes);
  }

  close(code = 1000, reason = ""): void {
    this.readyState = WebSocket.CLOSING;
    this.mux.closeWS(this.requestId, code, reason);
  }

  addEventListener(): void {
    /* not implemented — listeners go through onopen/onmessage/etc. */
  }
  removeEventListener(): void {
    /* not implemented */
  }
  dispatchEvent(): boolean {
    return true;
  }
}
