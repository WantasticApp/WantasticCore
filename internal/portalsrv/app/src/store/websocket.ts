import { writable, derived } from "svelte/store";
import {
  type EncryptionState,
  createEncryptionState,
  initializeEncryption,
  enableEncryption,
  encryptMessage,
  decryptMessage,
  isEncryptionEnabled,
} from "$lib/crypto";

/**
 * WebSocket event types for peer status updates
 */
export type PeerEventType =
  | "peer_connected"
  | "peer_disconnected"
  | "peer_updated"
  | "status_change"
  | "scan_started"
  | "scan_progress"
  | "scan_complete"
  | "scan_failed"
  // Live WUSP datamodel push from the agent — consumed by wuspStore.applyNotify
  // in $store/wusp.ts. Not technically a "peer event" semantically, but it
  // rides the same ring-buffer feed so it shares the type.
  | "wusp_notify";
export type ConnectionStatus =
  | "connecting"
  | "connected"
  | "disconnected"
  | "error";

// Session management - separate from auth to avoid circular dependencies
// Session token is passed via connect() or stored in sessionStorage
let sessionToken: string | null = null;
let sessionExpiresAt: number | null = null;

export interface PeerEvent {
  type: PeerEventType;
  peerId: string;
  timestamp: string;
  data?: Record<string, any>;
}

export interface PeerStatusUpdate {
  peerId: string;
  isOnline: boolean;
  lastSeen: string;
  latency?: number;
  transferRx?: number;
  transferTx?: number;
}

export interface WSMessage {
  type: string;
  payload: any;
  timestamp: string;
}

/**
 * gRPC request/response types
 */
export interface GRPCRequest {
  id: string;
  service: string;
  method: string;
  request: any;
}

export interface GRPCResponse {
  id: string;
  type: "response" | "error";
  response?: any;
  error?: string;
}

export interface CallGRPCOptions {
  /**
   * Override the default request timeout for operations that legitimately take
   * longer than the normal websocket RPC budget, such as WUSP data-model sync.
   */
  timeoutMs?: number;
  /**
   * Expected anonymous probes like authStore.checkSession() should fail as
   * "not signed in" without dispatching the global session-expired event.
   */
  suppressAuthExpiredEvent?: boolean;
}

export class ApiError extends Error {
  constructor(public code: string, public status: number, message: string) {
    super(message);
    this.name = "ApiError";
  }
}

/**
 * WebSocket connection manager for real-time peer updates
 */
function createWebSocketStore() {
  const { subscribe, set, update } = writable<{
    status: ConnectionStatus;
    message: string;
    events: PeerEvent[];
    peerStatuses: Map<string, PeerStatusUpdate>;
    deviceAuthRequests: any[];
    lastUpdate: string;
    /** True once the E2E key exchange has completed for the current connection. */
    encryptionReady: boolean;
    /**
     * Monotonic counter incremented on every successful (re)connection
     * (post key-exchange). Stores/components watch this to refetch state
     * that may have changed while the socket was down — without it, a long-
     * lived console keeps showing pre-disconnect data forever after recovery.
     */
    connectionGeneration: number;
  }>({
    status: "disconnected",
    message: "",
    events: [],
    peerStatuses: new Map(),
    deviceAuthRequests: [],
    connectionGeneration: 0,
    lastUpdate: new Date().toISOString(),
    encryptionReady: false,
  });

  let ws: WebSocket | null = null;
  let reconnectAttempts = 0;

  // WebProxy binary-frame listeners — registered by WebProxyMux instances
  // WebProxy binary-frame listeners. Each listener is a single callback
  // that receives the parsed envelope; webproxy-mux.ts is the only
  // expected subscriber, but the registry is a Set to keep the path
  // future-proof.
  type WebProxyFrame = {
    sessionId: string;
    headerJson: string; // raw JSON; the consumer parses with its own type
    body: Uint8Array;
  };
  const webProxyFrameListeners = new Set<(frame: WebProxyFrame) => void>();

  function registerWebProxyFrameListener(cb: (frame: WebProxyFrame) => void) {
    webProxyFrameListeners.add(cb);
  }
  function unregisterWebProxyFrameListener(
    cb: (frame: WebProxyFrame) => void
  ) {
    webProxyFrameListeners.delete(cb);
  }

  const MAX_RECONNECT_ATTEMPTS = 10;
  const BASE_RECONNECT_DELAY = 1000; // 1 second base
  const MAX_RECONNECT_DELAY = 30000; // 30 seconds max
  const CONNECTION_READY_TIMEOUT = 8000; // Give login/public RPCs time to catch the initial WS handshake.
  let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  let intentionalDisconnect = false; // Flag to prevent auto-reconnect after logout
  let lastWsUrl: string | null = null; // Track URL for reconnection
  let isOnline = typeof navigator !== "undefined" ? navigator.onLine : true;

  function isAuthExpiredError(message: string): boolean {
    const normalized = (message || "").toLowerCase();
    return (
      normalized.includes("unauthenticated") ||
      normalized.includes("authentication required") ||
      normalized.includes("invalid session") ||
      normalized.includes("session expired") ||
      normalized.includes("expired session") ||
      normalized.includes("missing session") ||
      normalized.includes("no valid session")
    );
  }

  function notifySessionExpired(message: string) {
    if (typeof window === "undefined") return;
    window.dispatchEvent(
      new CustomEvent("wantastic:session-expired", {
        detail: { message: message || "Session expired" },
      })
    );
  }

  // Listen for online/offline events to manage reconnection
  if (typeof window !== "undefined") {
    window.addEventListener("online", () => {
      isOnline = true;
      // When coming back online, attempt to reconnect if we were connected before
      if (
        lastWsUrl &&
        !intentionalDisconnect &&
        (!ws || ws.readyState !== WebSocket.OPEN)
      ) {
        reconnectAttempts = 0; // Reset attempts on network recovery
        connect(lastWsUrl);
      }
    });

    window.addEventListener("offline", () => {
      isOnline = false;
      // Clear any pending reconnect when offline
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
      }
      update((state) => ({
        ...state,
        status: "disconnected",
        message: "Offline - waiting for network...",
      }));
    });

    // Also reconnect when page becomes visible after being hidden
    document.addEventListener("visibilitychange", () => {
      if (
        document.visibilityState === "visible" &&
        lastWsUrl &&
        !intentionalDisconnect
      ) {
        // Check if we need to reconnect
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          reconnectAttempts = 0;
          connect(lastWsUrl);
        }
      }
    });
  }

  // Track pending gRPC requests for correlation
  const pendingRequests = new Map<
    string,
    {
      resolve: (value: any) => void;
      reject: (error: Error) => void;
      timeout: NodeJS.Timeout;
      suppressAuthExpiredEvent?: boolean;
    }
  >();

  // Track active gRPC streams
  const streamRequests = new Map<
    string,
    {
      onStart?: () => void;
      onData?: (data: any) => void;
      onError?: (error: string) => void;
      onEnd?: () => void;
    }
  >();

  // Subscription registry — survives disconnects so we can replay them after
  // the post-reconnect key exchange completes. Without this, a peer whose
  // status events were flowing before a server reload will silently stop
  // updating after recovery, since the proxy refcount went back to zero.
  const subscribedPeers = new Set<string>();
  const subscribedWusp = new Set<string>();

  // Module-scope counter so `set()` calls inside connect()/onopen don't reset
  // the generation back to 0 (which would look like "we went backwards" to
  // any subscriber comparing against a cached lastGen).
  let connectionGenerationCounter = 0;

  const REQUEST_TIMEOUT = 30000; // 30 seconds

  // SSH stream handlers for bidirectional streaming
  interface SSHStreamHandler {
    onReady?: () => void;
    onData: (data: string | Uint8Array) => void;
    onError: (error: string) => void;
    onClose: () => void;
    ready?: boolean;
  }
  const sshStreams = new Map<string, SSHStreamHandler>();

  interface RouterOSStreamHandler {
    onReady?: () => void;
    onState?: (state: any) => void;
    onResource?: (resource: any) => void;
    onNotice?: (notice: any) => void;
    onError: (error: string) => void;
    onClose: () => void;
    ready?: boolean;
  }
  const routerOSStreams = new Map<string, RouterOSStreamHandler>();
  const sshInputEncoder = new TextEncoder();
  const sshTextDecoder = new TextDecoder();
  // SSH_INPUT_BATCH_DELAY_MS removed — we always flush immediately.
  const SSH_INPUT_BATCH_MAX_BYTES = 2048;
  const SSH_BINARY_FRAME_VERSION = 1;
  const SSH_BINARY_FRAME_INPUT = 1;
  const SSH_BINARY_FRAME_OUTPUT = 2;
  // WebProxy binary frames share the SSH envelope version. frameType 0x10
  // carries browser→backend frames; 0x11 carries backend→browser frames.
  const WEBPROXY_BINARY_FRAME_CLIENT = 0x10;
  const WEBPROXY_BINARY_FRAME_SERVER = 0x11;
  type PendingSSHInput = {
    chunks: Uint8Array[];
    totalBytes: number;
    timer: ReturnType<typeof setTimeout> | null;
  };
  const pendingSSHInputs = new Map<string, PendingSSHInput>();

  function encodeSSHBinaryFrame(
    frameType: number,
    sessionId: string,
    payload: Uint8Array
  ): Uint8Array {
    const sessionBytes = sshInputEncoder.encode(sessionId);
    const frame = new Uint8Array(4 + sessionBytes.length + payload.length);
    frame[0] = SSH_BINARY_FRAME_VERSION;
    frame[1] = frameType;
    frame[2] = (sessionBytes.length >> 8) & 0xff;
    frame[3] = sessionBytes.length & 0xff;
    frame.set(sessionBytes, 4);
    frame.set(payload, 4 + sessionBytes.length);
    return frame;
  }

  function decodeSSHBinaryFrame(data: ArrayBuffer): {
    frameType: number;
    sessionId: string;
    payload: Uint8Array;
  } | null {
    const bytes = new Uint8Array(data);
    if (bytes.length < 4 || bytes[0] !== SSH_BINARY_FRAME_VERSION) {
      return null;
    }

    const sessionIdLength = (bytes[2] << 8) | bytes[3];
    const sessionStart = 4;
    const payloadStart = sessionStart + sessionIdLength;
    if (payloadStart > bytes.length) {
      return null;
    }

    return {
      frameType: bytes[1],
      sessionId: sshTextDecoder.decode(bytes.subarray(sessionStart, payloadStart)),
      payload: bytes.subarray(payloadStart),
    };
  }

  function flushSSHInput(sessionId: string): boolean {
    const pending = pendingSSHInputs.get(sessionId);
    if (!pending || pending.totalBytes === 0) {
      return true;
    }
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    if (!sshStreams.has(sessionId)) {
      pendingSSHInputs.delete(sessionId);
      return false;
    }

    if (pending.timer) {
      clearTimeout(pending.timer);
      pending.timer = null;
    }

    const merged = new Uint8Array(pending.totalBytes);
    let offset = 0;
    for (const chunk of pending.chunks) {
      merged.set(chunk, offset);
      offset += chunk.length;
    }
    pendingSSHInputs.delete(sessionId);

    // The DOM lib's BufferSource excludes SharedArrayBuffer-backed views,
    // so a Uint8Array<ArrayBufferLike> can't be passed directly. Copy into
    // a fresh ArrayBuffer so the type is concretely ArrayBuffer.
    const frame = encodeSSHBinaryFrame(SSH_BINARY_FRAME_INPUT, sessionId, merged);
    const sendBuf = new ArrayBuffer(frame.byteLength);
    new Uint8Array(sendBuf).set(frame);
    ws.send(sendBuf);

    return true;
  }

  function clearPendingSSHInput(sessionId: string): void {
    const pending = pendingSSHInputs.get(sessionId);
    if (pending?.timer) {
      clearTimeout(pending.timer);
    }
    pendingSSHInputs.delete(sessionId);
  }

  // ── WebProxy binary frames ──────────────────────────────────────────────
  // Wire format mirrors webproxy_bridge.go (Go side):
  //   [version=1:1][frameType:1][sessionIdLen:2 BE][sessionId]
  //     [headerJsonLen:4 BE][headerJson][bodyBytes]
  // headerJson is a small UTF-8 JSON document (request/response metadata);
  // bodyBytes is raw — request body, response chunk, or WS frame data.
  const webProxyEncoder = new TextEncoder();
  const webProxyDecoder = new TextDecoder();

  function encodeWebProxyBinaryFrame(
    frameType: number,
    sessionId: string,
    headerJson: string,
    body: Uint8Array
  ): Uint8Array {
    const sessionBytes = webProxyEncoder.encode(sessionId);
    const headerBytes = webProxyEncoder.encode(headerJson);
    const total = 4 + sessionBytes.length + 4 + headerBytes.length + body.length;
    const frame = new Uint8Array(total);
    frame[0] = SSH_BINARY_FRAME_VERSION;
    frame[1] = frameType;
    frame[2] = (sessionBytes.length >> 8) & 0xff;
    frame[3] = sessionBytes.length & 0xff;
    let off = 4;
    frame.set(sessionBytes, off);
    off += sessionBytes.length;
    frame[off++] = (headerBytes.length >>> 24) & 0xff;
    frame[off++] = (headerBytes.length >>> 16) & 0xff;
    frame[off++] = (headerBytes.length >>> 8) & 0xff;
    frame[off++] = headerBytes.length & 0xff;
    frame.set(headerBytes, off);
    off += headerBytes.length;
    frame.set(body, off);
    return frame;
  }

  function decodeWebProxyBinaryFrame(data: ArrayBuffer): WebProxyFrame | null {
    const bytes = new Uint8Array(data);
    if (
      bytes.length < 4 ||
      bytes[0] !== SSH_BINARY_FRAME_VERSION ||
      bytes[1] !== WEBPROXY_BINARY_FRAME_SERVER
    ) {
      return null;
    }
    const sessionIdLen = (bytes[2] << 8) | bytes[3];
    let off = 4;
    if (off + sessionIdLen + 4 > bytes.length) return null;
    const sessionId = webProxyDecoder.decode(
      bytes.subarray(off, off + sessionIdLen)
    );
    off += sessionIdLen;
    const headerLen =
      (bytes[off] << 24) |
      (bytes[off + 1] << 16) |
      (bytes[off + 2] << 8) |
      bytes[off + 3];
    off += 4;
    if (off + headerLen > bytes.length) return null;
    const headerJson = webProxyDecoder.decode(
      bytes.subarray(off, off + headerLen)
    );
    off += headerLen;
    const body = bytes.subarray(off);
    return { sessionId, headerJson, body };
  }

  /**
   * Send a webproxy frame over the WebSocket. headerJson is the small
   * metadata object (request/response/ws_frame/error/ping); body is the
   * raw payload bytes (empty Uint8Array for control frames).
   *
   * Returns false if the socket is not open. The caller is expected to
   * either retry after reconnect or surface the error to the UI.
   */
  function sendWebProxyFrame(
    sessionId: string,
    headerJson: string,
    body: Uint8Array
  ): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return false;
    }
    const frame = encodeWebProxyBinaryFrame(
      WEBPROXY_BINARY_FRAME_CLIENT,
      sessionId,
      headerJson,
      body
    );
    const buf = new ArrayBuffer(frame.byteLength);
    new Uint8Array(buf).set(frame);
    ws.send(buf);
    return true;
  }

  // Encryption state for end-to-end encrypted messages
  let encryptionState: EncryptionState = createEncryptionState();

  // Tracks whether the server-side key exchange has completed.
  // callGRPC / streamGRPC MUST await this before sending any message —
  // the server rejects all plaintext requests with "authentication required"
  // until the encrypted channel is established.
  let encryptionReadyResolve: (() => void) | null = null;
  let encryptionReadyReject: ((e: Error) => void) | null = null;
  let encryptionReadyPromise: Promise<void> = Promise.resolve(); // resolved by default (no-op until first connect)

  function markHandled<T>(promise: Promise<T>): Promise<T> {
    // Some callers intentionally fire-and-forget refresh/login RPCs. Keep the
    // promise semantics intact for awaiters, but prevent browser-level
    // "Uncaught (in promise)" noise when a socket closes underneath them.
    promise.catch(() => {});
    return promise;
  }

  function resetEncryptionReady() {
    encryptionReadyPromise = markHandled(
      new Promise<void>((resolve, reject) => {
        encryptionReadyResolve = resolve;
        encryptionReadyReject = reject;
      })
    );
  }

  function resolveEncryptionReady() {
    encryptionReadyResolve?.();
    encryptionReadyResolve = null;
    encryptionReadyReject = null;
    connectionGenerationCounter += 1;
    update((s) => ({
      ...s,
      encryptionReady: true,
      // Bump the generation last so subscribers see a fully-ready store
      // (encryptionReady=true AND status=connected) when they react to it.
      connectionGeneration: connectionGenerationCounter,
    }));
    // Replay subscriptions that survived the disconnect. The proxy refcounts
    // duplicates, so re-sending the same subscribe is safe even if (e.g.)
    // the server still had a stale entry for this session.
    if (subscribedPeers.size > 0 || subscribedWusp.size > 0) {
      console.log(
        `[wsStore] Replaying ${subscribedPeers.size} peer + ${subscribedWusp.size} WUSP subscriptions after reconnect`
      );
      for (const peerId of subscribedPeers) {
        send("subscribe_peer", { peer_id: peerId });
      }
      for (const peerId of subscribedWusp) {
        send("subscribe_wusp", { peer_id: peerId });
      }
    }
  }

  function rejectEncryptionReady(reason: string) {
    encryptionReadyReject?.(new Error(reason));
    encryptionReadyResolve = null;
    encryptionReadyReject = null;
    // Replace with an already-rejected promise so future awaits fail fast.
    encryptionReadyPromise = markHandled(Promise.reject(new Error(reason)));
  }

  function waitForSocketOpen(timeoutMs = CONNECTION_READY_TIMEOUT): Promise<void> {
    if (ws?.readyState === WebSocket.OPEN) {
      return Promise.resolve();
    }

    if ((!ws || ws.readyState === WebSocket.CLOSED || ws.readyState === WebSocket.CLOSING) && lastWsUrl) {
      reconnectAttempts = 0;
      intentionalDisconnect = false;
      connect(lastWsUrl);
    }

    if (ws?.readyState === WebSocket.OPEN) {
      return Promise.resolve();
    }
    if (!ws || ws.readyState !== WebSocket.CONNECTING) {
      return Promise.reject(new ApiError("WS_ERROR", 503, "WebSocket not connected"));
    }

    const socket = ws;
    return markHandled(
      new Promise<void>((resolve, reject) => {
        let timeout: ReturnType<typeof setTimeout> | null = null;

        const cleanup = () => {
          if (timeout) clearTimeout(timeout);
          socket.removeEventListener("open", onOpen);
          socket.removeEventListener("close", onClose);
          socket.removeEventListener("error", onError);
        };
        const onOpen = () => {
          cleanup();
          resolve();
        };
        const onClose = () => {
          cleanup();
          reject(new ApiError("WS_ERROR", 503, "WebSocket disconnected"));
        };
        const onError = () => {
          cleanup();
          reject(new ApiError("WS_ERROR", 503, "WebSocket connection failed"));
        };

        timeout = setTimeout(() => {
          cleanup();
          reject(new ApiError("WS_ERROR", 503, "WebSocket connection timed out"));
        }, timeoutMs);
        socket.addEventListener("open", onOpen, { once: true });
        socket.addEventListener("close", onClose, { once: true });
        socket.addEventListener("error", onError, { once: true });
      })
    );
  }

  /**
   * Set session token for authentication
   * Call this whenever auth state changes
   */
  function setSessionToken(token: string | null, expiresAt?: number) {
    sessionToken = token;
    sessionExpiresAt = expiresAt || null;

    // If token is expired or missing, disconnect
    if (!token || (expiresAt && Date.now() >= expiresAt)) {
      if (ws && ws.readyState === WebSocket.OPEN) {
        disconnect();
      }
    }
  }

  /**
   * Check if session is still valid
   */
  function isSessionValid(): boolean {
    return !!(
      sessionToken &&
      (!sessionExpiresAt || Date.now() < sessionExpiresAt)
    );
  }

  /**
   * Connect to WebSocket server
   */
  function connect(wsUrl: string, token?: string) {
    // Store URL for reconnection
    lastWsUrl = wsUrl;

    // Cancel any pending reconnect
    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }

    // Don't connect if offline
    if (!isOnline) {
      update((state) => ({
        ...state,
        status: "disconnected",
        message: "Offline - waiting for network...",
      }));
      return;
    }

    if (
      ws &&
      (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)
    ) {
      return; // Already connected or handshaking
    }

    // Reset intentional disconnect flag when connecting
    intentionalDisconnect = false;

    // Arm a new encryption-ready gate for this connection attempt.
    // All callGRPC / streamGRPC calls will queue behind this promise until
    // the server completes the key exchange.
    resetEncryptionReady();
    // Also reset encryption state so the new key exchange starts fresh.
    encryptionState = createEncryptionState();

    // Update token if provided
    if (token) {
      setSessionToken(token);
    }

    // Allow connection without session for login/registration flows
    // The backend will check the session cookie if it exists
    // For unauthenticated requests (login/register), the backend allows them

    set({
      status: "connecting",
      message: "Connecting to server...",
      events: [],
      peerStatuses: new Map(),
      deviceAuthRequests: [],
      lastUpdate: new Date().toISOString(),
      encryptionReady: false,
      connectionGeneration: connectionGenerationCounter,
    });

    try {
      const url = new URL(wsUrl);
      // Browser automatically sends auth_session cookie with WebSocket upgrade
      // (no need to manually add token as query param)

      ws = new WebSocket(url.toString());
      ws.binaryType = "arraybuffer";

      ws.onopen = () => {
        // console.log(' WebSocket connected');
        set({
          status: "connected",
          message: "Connected",
          events: [],
          peerStatuses: new Map(),
          deviceAuthRequests: [],
          lastUpdate: new Date().toISOString(),
          encryptionReady: false, // key exchange happens after onopen
          connectionGeneration: connectionGenerationCounter,
        });
        reconnectAttempts = 0;
        startHeartbeat();
      };

      ws.onmessage = async (event) => {
        try {
          if (event.data instanceof ArrayBuffer) {
            const bytes = new Uint8Array(event.data);
            if (bytes.length < 2 || bytes[0] !== SSH_BINARY_FRAME_VERSION) {
              return;
            }
            // Dispatch by frameType: SSH output and webproxy server frames
            // share the envelope version byte.
            switch (bytes[1]) {
              case SSH_BINARY_FRAME_OUTPUT: {
                const frame = decodeSSHBinaryFrame(event.data);
                if (!frame) return;
                const handler = sshStreams.get(frame.sessionId);
                if (!handler) return;
                if (!handler.ready) {
                  handler.ready = true;
                  handler.onReady?.();
                }
                handler.onData(frame.payload);
                return;
              }
              case WEBPROXY_BINARY_FRAME_SERVER: {
                const wf = decodeWebProxyBinaryFrame(event.data);
                if (!wf) return;
                for (const cb of webProxyFrameListeners) {
                  try {
                    cb(wf);
                  } catch (e) {
                    console.error("[wsStore] webproxy listener error:", e);
                  }
                }
                return;
              }
              default:
                return;
            }
          }

          let data = JSON.parse(event.data);

          // Handle pong response (keepalive ack) explicitly to track connection health
          if (data.type === "pong") {
            lastPongReceived = Date.now();
            return;
          }

          // Handle key exchange from server
          if (
            data.type === "key_exchange" &&
            data.server_public_key &&
            data.session_id
          ) {
            try {
              const clientPublicKey = await initializeEncryption(
                encryptionState,
                data.server_public_key,
                data.session_id
              );
              // Send client's public key back to server
              ws?.send(
                JSON.stringify({
                  type: "key_exchange",
                  client_public_key: clientPublicKey,
                })
              );
              enableEncryption(encryptionState);
              // Resolve the gate — all queued callGRPC / streamGRPC calls can now proceed.
              resolveEncryptionReady();
            } catch (err) {
              console.error("Failed to complete key exchange:", err);
              rejectEncryptionReady("Key exchange failed");
            }
            return;
          }

          // Handle server confirmation that key exchange is complete (belt-and-suspenders).
          if (data.type === "key_exchange_complete") {
            resolveEncryptionReady();
            return;
          }

          // Handle encrypted messages
          if (data.type === "encrypted" && data.ciphertext) {
            if (!isEncryptionEnabled(encryptionState)) {
              console.error(
                "Received encrypted message but encryption not enabled"
              );
              return;
            }
            try {
              const plaintext = await decryptMessage(
                encryptionState,
                data.ciphertext
              );
              data = JSON.parse(plaintext);
            } catch (err) {
              console.error("Failed to decrypt message:", err);
              return;
            }
          }

          // Handle gRPC responses (correlate with pending requests)
          if (data.id && (data.type === "response" || data.type === "error")) {
            const pending = pendingRequests.get(data.id);
            if (pending) {
              clearTimeout(pending.timeout);
              pendingRequests.delete(data.id);

              if (data.type === "response") {
                // console.log(` gRPC response: ${data.id}`, data.response);
                pending.resolve(data.response);
              } else {
                // console.error(`❌ gRPC error: ${data.id}`, data.error);
                if (
                  isAuthExpiredError(data.error || "") &&
                  !pending.suppressAuthExpiredEvent
                ) {
                  notifySessionExpired(data.error || "Session expired");
                }
                pending.reject(
                  new ApiError(
                    "RPC_ERROR",
                    500,
                    data.error || "Unknown gRPC error"
                  )
                );
              }
            }
            return;
          }

          // Handle gRPC streams
          if (
            data.id &&
            (data.type === "stream_started" ||
              data.type === "stream_data" ||
              data.type === "stream_end" ||
              data.type === "stream_error")
          ) {
            const stream = streamRequests.get(data.id);
            if (stream) {
              if (data.type === "stream_started") {
                if (stream.onStart) stream.onStart();
              } else if (data.type === "stream_data") {
                if (stream.onData) stream.onData(data.response);
              } else if (data.type === "stream_end") {
                if (stream.onEnd) stream.onEnd();
                streamRequests.delete(data.id);
              } else if (data.type === "stream_error") {
                if (stream.onError)
                  stream.onError(data.error || "Unknown stream error");
                streamRequests.delete(data.id);
              }
            }
            return;
          }

          // Handle SSH stream messages (bidirectional streaming)
          if (data.type === "ssh_stream") {
            const sessionId = data.session_id;
            const handler = sshStreams.get(sessionId);
            if (handler) {
              if (data.payload?.output) {
                // SSH output data
                const base64Data = data.payload.output.data;
                if (base64Data) {
                  if (!handler.ready) {
                    handler.ready = true;
                    handler.onReady?.();
                  }
                  handler.onData(atob(base64Data));
                }
                if (data.payload.output.error) {
                  handler.onError(data.payload.output.error);
                }
              } else if (data.payload?.ping) {
                if (!handler.ready) {
                  handler.ready = true;
                  handler.onReady?.();
                }
                // Server ping/ready notifications are one-way. Echoing them back
                // creates a ping loop across browser -> portal -> core -> browser
                // that can starve the interactive SSH stream on reconnect.
              } else if (data.payload?.close) {
                // Stream closed by server
                handler.onClose();
                sshStreams.delete(sessionId);
              }
            }
            return;
          }

          if (data.type === "routeros_stream") {
            const sessionId = data.session_id;
            const handler = routerOSStreams.get(sessionId);
            if (handler) {
              if (data.payload?.state) {
                if (!handler.ready) {
                  handler.ready = true;
                  handler.onReady?.();
                }
                handler.onState?.(data.payload.state);
              } else if (data.payload?.resource) {
                if (!handler.ready) {
                  handler.ready = true;
                  handler.onReady?.();
                }
                handler.onResource?.(data.payload.resource);
              } else if (data.payload?.notice) {
                if (!handler.ready) {
                  handler.ready = true;
                  handler.onReady?.();
                }
                handler.onNotice?.(data.payload.notice);
              }
              if (data.payload?.error) {
                handler.onError(data.payload.error);
              }
              if (data.payload?.close) {
                handler.onClose();
                routerOSStreams.delete(sessionId);
              }
            }
            return;
          }

          // Handle real-time messages (peer events, stats, etc.)
          const message: WSMessage = data;
          handleMessage(message);
        } catch (err) {
          console.error("Failed to parse WebSocket message:", err);
        }
      };
      ws.onerror = (error) => {
        console.error("❌ WebSocket error:", error);
        update((state) => ({
          ...state,
          status: "error",
          message: "WebSocket error - retrying...",
        }));
      };

      ws.onclose = () => {
        // console.log(' WebSocket disconnected');
        stopHeartbeat();
        ws = null;
        // Unblock any callGRPC calls that are waiting for key exchange — they
        // should fail immediately rather than hang until their 30-second timeout.
        rejectEncryptionReady("WebSocket disconnected");

        // Fail-fast on every in-flight gRPC request and stream. Without this,
        // a request sent just before the socket dropped sits in pendingRequests
        // for the full 30s REQUEST_TIMEOUT before the awaiter sees an error —
        // long enough for the user to assume the page hung. We close the
        // promise/stream now and let the caller (or its retry loop, if any)
        // decide whether to re-issue against the new socket.
        const closeError = new Error("WebSocket disconnected");
        for (const [, pending] of pendingRequests) {
          clearTimeout(pending.timeout);
          pending.reject(closeError);
        }
        pendingRequests.clear();
        for (const [, stream] of streamRequests) {
          try {
            stream.onError?.("WebSocket disconnected");
            stream.onEnd?.();
          } catch (e) {
            console.warn("[wsStore] stream error/end handler threw on close:", e);
          }
        }
        streamRequests.clear();

        update((state) => ({
          ...state,
          status: "disconnected",
          message: "Disconnected",
          encryptionReady: false,
        }));

        for (const [sessionId, pending] of pendingSSHInputs.entries()) {
          if (pending.timer) {
            clearTimeout(pending.timer);
          }
          pendingSSHInputs.delete(sessionId);
        }
        for (const [sessionId, handler] of sshStreams.entries()) {
          try {
            handler.onClose();
          } finally {
            sshStreams.delete(sessionId);
          }
        }
        for (const [sessionId, handler] of routerOSStreams.entries()) {
          try {
            handler.onClose();
          } finally {
            routerOSStreams.delete(sessionId);
          }
        }

        // Only attempt reconnect if not intentionally disconnected (e.g., logout)
        if (!intentionalDisconnect) {
          attemptReconnect(wsUrl);
        }
      };
    } catch (err) {
      console.error("Failed to create WebSocket:", err);
      update((state) => ({
        ...state,
        status: "error",
        message: `Connection failed: ${err instanceof Error ? err.message : "Unknown error"
          }`,
      }));
      attemptReconnect(wsUrl);
    }
  }

  // Heartbeat mechanism
  let pingInterval: ReturnType<typeof setInterval> | null = null;
  const PING_INTERVAL_MS = 15000; // 15 seconds (reduced to prevent proxy drops)
  let lastPingSent = 0;
  let lastPongReceived = 0;
  const PONG_TIMEOUT_MS = 10000; // 10 seconds to wait for pong

  function startHeartbeat() {
    stopHeartbeat();

    // Initialize timestamps when starting
    lastPingSent = Date.now();
    lastPongReceived = Date.now();

    if (ws && ws.readyState === WebSocket.OPEN) {
      pingInterval = setInterval(() => {
        if (ws && ws.readyState === WebSocket.OPEN) {

          // Check if we missed the last pong (connection is dead but socket hasn't closed)
          if (lastPingSent > lastPongReceived && (Date.now() - lastPingSent > PONG_TIMEOUT_MS)) {
            console.warn("WebSocket ping timeout - connection appears dead. Forcing drop.");
            ws.close(); // This will trigger onclose and standard reconnection logic
            return;
          }

          lastPingSent = Date.now();
          ws.send(JSON.stringify({ type: "ping" }));
        }
      }, PING_INTERVAL_MS);
    }
  }

  function stopHeartbeat() {
    if (pingInterval) {
      clearInterval(pingInterval);
      pingInterval = null;
    }
  }

  /**
   * Handle incoming WebSocket messages
   */
  function handleMessage(message: WSMessage) {
    update((state) => {
      const timestamp = new Date().toISOString();

      switch (message.type) {
        case "peer_event": {
          const eventTimestamp =
            message.payload?.timestamp || message.payload?.data?.timestamp || timestamp;
          const event: PeerEvent = {
            type: message.payload.type,
            peerId: message.payload.peerId,
            timestamp: eventTimestamp,
            data: message.payload.data ?? message.payload,
          };
          return {
            ...state,
            events: [...state.events.slice(-99), event], // Keep last 100 events
            lastUpdate: timestamp,
          };
        }

        case "peer_status": {
          const statusUpdate: PeerStatusUpdate = {
            peerId: message.payload.peerId,
            isOnline: message.payload.isOnline,
            lastSeen: message.payload.lastSeen,
            latency: message.payload.latency,
            transferRx: message.payload.transferRx,
            transferTx: message.payload.transferTx,
          };
          state.peerStatuses.set(statusUpdate.peerId, statusUpdate);
          return {
            ...state,
            lastUpdate: timestamp,
          };
        }

        case "event": {
          if (message.payload.type === "device_auth_popup") {
            return {
              ...state,
              deviceAuthRequests: [
                ...state.deviceAuthRequests,
                message.payload.data,
              ],
              lastUpdate: timestamp,
            };
          }
          return state;
        }

        default:
          return state;
      }
    });
  }

  /**
   * Attempt to reconnect with exponential backoff and jitter
   * Uses capped exponential backoff to avoid overwhelming the server
   */
  function attemptReconnect(wsUrl: string) {
    // Don't reconnect if offline
    if (!isOnline) {
      update((state) => ({
        ...state,
        status: "disconnected",
        message: "Offline - waiting for network...",
      }));
      return;
    }

    // Don't reconnect if intentionally disconnected
    if (intentionalDisconnect) {
      return;
    }

    if (reconnectAttempts >= MAX_RECONNECT_ATTEMPTS) {
      update((state) => ({
        ...state,
        status: "error",
        message: "Connection failed. Click to retry.",
      }));
      // Don't fully give up - allow manual retry or visibility change to reset
      return;
    }

    reconnectAttempts++;

    // Exponential backoff with jitter: delay = min(maxDelay, baseDelay * 2^attempt) + random jitter
    const exponentialDelay = Math.min(
      MAX_RECONNECT_DELAY,
      BASE_RECONNECT_DELAY * Math.pow(2, reconnectAttempts - 1)
    );
    // Add 0-25% random jitter to prevent thundering herd
    const jitter = exponentialDelay * Math.random() * 0.25;
    const delay = Math.floor(exponentialDelay + jitter);

    update((state) => ({
      ...state,
      status: "connecting",
      message: `Reconnecting in ${Math.ceil(
        delay / 1000
      )}s (${reconnectAttempts}/${MAX_RECONNECT_ATTEMPTS})...`,
    }));

    reconnectTimeout = setTimeout(() => {
      reconnectTimeout = null;
      connect(wsUrl);
    }, delay);
  }

  /**
   * Send message to server (for real-time updates)
   */
  function send(type: string, payload: any) {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.warn("WebSocket not connected, cannot send:", type);
      return;
    }

    try {
      ws.send(
        JSON.stringify({
          type,
          payload,
          timestamp: new Date().toISOString(),
        })
      );
    } catch (err) {
      console.error("Failed to send WebSocket message:", err);
    }
  }

  /**
   * Call gRPC service method and wait for response
   * Returns a Promise that resolves with the response data
   */
  async function callGRPC<T>(
    service: string,
    method: string,
    request: any = {},
    options: CallGRPCOptions = {}
  ): Promise<T> {
    const promise = new Promise<T>(async (resolve, reject) => {
      try {
        await waitForSocketOpen(Math.min(REQUEST_TIMEOUT, CONNECTION_READY_TIMEOUT));
      } catch (err) {
        reject(err instanceof Error ? err : new ApiError("WS_ERROR", 503, "WebSocket not connected"));
        return;
      }

      if (!ws || ws.readyState !== WebSocket.OPEN) {
        reject(new ApiError("WS_ERROR", 503, "WebSocket not connected"));
        return;
      }

      const requestId = `${service}-${method}-${Date.now()}-${Math.random()}`;
      const timeoutMs = options.timeoutMs ?? REQUEST_TIMEOUT;
      const timeout = setTimeout(() => {
        pendingRequests.delete(requestId);
        reject(
          new ApiError(
            "TIMEOUT",
            504,
            `gRPC call timeout: ${service}.${method}`
          )
        );
      }, timeoutMs);

      pendingRequests.set(requestId, {
        resolve,
        reject,
        timeout,
        suppressAuthExpiredEvent: options.suppressAuthExpiredEvent,
      });

      try {
        // Wait for E2E encryption to be negotiated before sending.
        // Sending before key exchange completes results in "authentication required".
        if (!isEncryptionEnabled(encryptionState)) {
          await encryptionReadyPromise;
        }

        // Re-check connection — it might have dropped while we were waiting.
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          pendingRequests.delete(requestId);
          clearTimeout(timeout);
          reject(new ApiError("WS_ERROR", 503, "WebSocket disconnected"));
          return;
        }

        const grpcRequest: GRPCRequest = {
          id: requestId,
          service,
          method,
          request,
        };

        const plaintext = JSON.stringify(grpcRequest);
        const ciphertext = await encryptMessage(encryptionState, plaintext);
        ws.send(JSON.stringify({ type: "encrypted", ciphertext }));
      } catch (err) {
        pendingRequests.delete(requestId);
        clearTimeout(timeout);
        reject(
          new ApiError(
            "WS_ERROR",
            503,
            `Failed to send gRPC request: ${err instanceof Error ? err.message : "Unknown error"
            }`
          )
        );
      }
    });
    return markHandled(promise);
  }

  /**
   * Stream gRPC service method
   * Returns a function to close the stream
   */
  function streamGRPC<T>(
    service: string,
    method: string,
    request: any = {},
    handlers: {
      onStart?: () => void;
      onData?: (data: T) => void;
      onError?: (error: string) => void;
      onEnd?: () => void;
    }
  ): () => void {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      if (handlers.onError) handlers.onError("WebSocket not connected");
      return () => { };
    }

    const requestId = `${service}-${method}-${Date.now()}-${Math.random()}`;

    streamRequests.set(requestId, handlers);

    // Send asynchronously so we can await the encryption gate without
    // changing the synchronous return type of streamGRPC.
    (async () => {
      try {
        const grpcRequest: GRPCRequest = {
          id: requestId,
          service,
          method,
          request,
        };

        // Wait for E2E encryption to be negotiated before sending.
        // Sending before key exchange completes results in "authentication required".
        if (!isEncryptionEnabled(encryptionState)) {
          await encryptionReadyPromise;
        }

        // Re-check connection — it might have dropped while we were waiting.
        if (!ws || ws.readyState !== WebSocket.OPEN) {
          streamRequests.delete(requestId);
          if (handlers.onError) handlers.onError("WebSocket disconnected");
          return;
        }

        const ciphertext = await encryptMessage(encryptionState, JSON.stringify(grpcRequest));
        ws.send(JSON.stringify({ type: "encrypted", ciphertext }));
      } catch (err) {
        streamRequests.delete(requestId);
        if (handlers.onError)
          handlers.onError(
            `Failed to send gRPC request: ${err instanceof Error ? err.message : "Unknown error"}`
          );
      }
    })();

    return () => {
      streamRequests.delete(requestId);
    };
  }

  /**
   * Subscribe to peer status updates. The peer ID is recorded in
   * `subscribedPeers` so that resolveEncryptionReady() can replay every
   * active subscription after a reconnect.
   */
  function subscribeToPeer(peerId: string) {
    subscribedPeers.add(peerId);
    send("subscribe_peer", { peer_id: peerId });
  }

  /**
   * Unsubscribe from peer updates
   */
  function unsubscribeFromPeer(peerId: string) {
    subscribedPeers.delete(peerId);
    send("unsubscribe_peer", { peer_id: peerId });
  }

  /**
   * Subscribe to a peer's WUSP live event feed (ValueChange / OperationComplete /
   * ObjectCreation / ObjectDeletion). The proxy refcounts subscribers and
   * registers the canonical Subscribe with the agent on the 0→1 edge so the
   * dashboard sees pushed updates without polling.
   */
  function subscribeToWusp(peerId: string) {
    subscribedWusp.add(peerId);
    send("subscribe_wusp", { peer_id: peerId });
  }

  /**
   * Cancel a peer's WUSP live event feed subscription. Proxy debounces the
   * agent-side teardown for 30 s so a reload/tab-switch doesn't churn it.
   */
  function unsubscribeFromWusp(peerId: string) {
    subscribedWusp.delete(peerId);
    send("unsubscribe_wusp", { peer_id: peerId });
  }

  /**
   * Disconnect WebSocket
   */
  function disconnect() {
    // Set flag to prevent auto-reconnect
    intentionalDisconnect = true;

    if (reconnectTimeout) {
      clearTimeout(reconnectTimeout);
      reconnectTimeout = null;
    }
    if (ws) {
      ws.close();
      ws = null;
    }
    // Reset encryption state for next session
    encryptionState = createEncryptionState();
    // Reset reconnect attempts
    reconnectAttempts = 0;
    // Clear stored URL on intentional disconnect
    lastWsUrl = null;
    // Drop subscription registry on intentional disconnect (logout) — the
    // user is leaving, not just a transient drop, so nothing to replay.
    subscribedPeers.clear();
    subscribedWusp.clear();
    connectionGenerationCounter = 0;
    set({
      status: "disconnected",
      message: "Disconnected",
      events: [],
      peerStatuses: new Map(),
      deviceAuthRequests: [],
      lastUpdate: new Date().toISOString(),
      encryptionReady: false,
      connectionGeneration: 0,
    });
  }

  /**
   * Manually retry connection after max attempts reached
   * Call this when user clicks retry button
   */
  function retry() {
    if (lastWsUrl) {
      reconnectAttempts = 0;
      intentionalDisconnect = false;
      connect(lastWsUrl);
    }
  }

  /**
   * Check if WebSocket is currently connected
   */
  function isConnected(): boolean {
    return ws !== null && ws.readyState === WebSocket.OPEN;
  }

  // ===== SSH Stream Functions =====

  /**
   * Open SSH stream - registers handlers for receiving SSH data
   * Call this after creating an SSH session to start bidirectional streaming
   */
  function openSSHStream(
    sessionId: string,
    handlers: {
      onReady?: () => void;
      onData: (data: string | Uint8Array) => void;
      onError: (error: string) => void;
      onClose: () => void;
    }
  ): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.error("Cannot open SSH stream: WebSocket not connected");
      return false;
    }

    if (sshStreams.has(sessionId)) {
      closeSSHStream(sessionId);
    }

    // Register handlers
    sshStreams.set(sessionId, { ...handlers, ready: false });
    clearPendingSSHInput(sessionId);

    // Send stream start message to server
    ws.send(
      JSON.stringify({
        type: "ssh_stream_start",
        session_id: sessionId,
        timestamp: new Date().toISOString(),
      })
    );

    console.log(`🔗 SSH stream opened for session: ${sessionId}`);
    return true;
  }

  /**
   * Send SSH input data to server
   */
  function sendSSHData(sessionId: string, data: string | Uint8Array): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.error("Cannot send SSH data: WebSocket not connected");
      return false;
    }

    if (!sshStreams.has(sessionId)) {
      console.error("SSH stream not open for session:", sessionId);
      return false;
    }

    const chunk =
      typeof data === "string" ? sshInputEncoder.encode(data) : data;
    let pending = pendingSSHInputs.get(sessionId);
    if (!pending) {
      pending = {
        chunks: [],
        totalBytes: 0,
        timer: null,
      };
      pendingSSHInputs.set(sessionId, pending);
    }

    pending.chunks.push(chunk);
    pending.totalBytes += chunk.length;

    // Always flush immediately — browser timers have 4–16 ms resolution
    // which is unacceptable for an interactive terminal.
    return flushSSHInput(sessionId);
  }

  /**
   * Send SSH terminal resize to server
   */
  function sendSSHResize(
    sessionId: string,
    rows: number,
    cols: number
  ): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.error("Cannot send SSH resize: WebSocket not connected");
      return false;
    }

    if (!sshStreams.has(sessionId)) {
      console.error("SSH stream not open for session:", sessionId);
      return false;
    }

    flushSSHInput(sessionId);

    ws.send(
      JSON.stringify({
        type: "ssh_stream",
        session_id: sessionId,
        payload: {
          resize: {
            rows,
            cols,
          },
        },
      })
    );

    return true;
  }

  /**
   * Send SSH ping (keepalive)
   */
  function sendSSHPing(sessionId: string): void {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      return;
    }

    flushSSHInput(sessionId);

    ws.send(
      JSON.stringify({
        type: "ssh_stream",
        session_id: sessionId,
        payload: {
          ping: {
            timestamp: Date.now(),
          },
        },
      })
    );
  }

  /**
   * Close SSH stream
   */
  function closeSSHStream(sessionId: string): void {
    const handler = sshStreams.get(sessionId);
    if (handler) {
      flushSSHInput(sessionId);
      // Notify server
      if (ws && ws.readyState === WebSocket.OPEN) {
        ws.send(
          JSON.stringify({
            type: "ssh_stream_close",
            session_id: sessionId,
          })
        );
      }

      // Clean up locally
      sshStreams.delete(sessionId);
      clearPendingSSHInput(sessionId);
      console.log(`🔗 SSH stream closed for session: ${sessionId}`);
    }
  }

  /**
   * Check if SSH stream is active
   */
  function isSSHStreamActive(sessionId: string): boolean {
    return sshStreams.has(sessionId);
  }

  function openRouterOSStream(
    sessionId: string,
    peerId: string,
    handlers: {
      onReady?: () => void;
      onState?: (state: any) => void;
      onResource?: (resource: any) => void;
      onNotice?: (notice: any) => void;
      onError: (error: string) => void;
      onClose: () => void;
    },
    options: {
      resource?: number;
    } = {}
  ): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.error("Cannot open RouterOS stream: WebSocket not connected");
      return false;
    }

    if (routerOSStreams.has(sessionId)) {
      closeRouterOSStream(sessionId);
    }

    routerOSStreams.set(sessionId, { ...handlers, ready: false });

    ws.send(
      JSON.stringify({
        type: "routeros_stream_start",
        session_id: sessionId,
        payload: {
          open: {
            peer_id: peerId,
            resource: options.resource || 0,
          },
        },
      })
    );

    return true;
  }

  function sendRouterOSCommand(
    sessionId: string,
    payload: Record<string, unknown>
  ): boolean {
    if (!ws || ws.readyState !== WebSocket.OPEN) {
      console.error("Cannot send RouterOS command: WebSocket not connected");
      return false;
    }

    if (!routerOSStreams.has(sessionId)) {
      console.error("RouterOS stream not open for session:", sessionId);
      return false;
    }

    ws.send(
      JSON.stringify({
        type: "routeros_stream",
        session_id: sessionId,
        payload,
      })
    );

    return true;
  }

  function closeRouterOSStream(sessionId: string): void {
    if (!routerOSStreams.has(sessionId)) {
      return;
    }

    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(
        JSON.stringify({
          type: "routeros_stream_close",
          session_id: sessionId,
        })
      );
    }

    routerOSStreams.delete(sessionId);
  }

  /**
   * Clear a device auth request
   */
  function clearDeviceAuthRequest(userCode: string) {
    update((state) => ({
      ...state,
      deviceAuthRequests: state.deviceAuthRequests.filter(
        (r) => r.user_code !== userCode
      ),
    }));
  }

  return {
    subscribe,
    connect,
    disconnect,
    retry,
    isConnected,
    send,
    callGRPC,
    streamGRPC,
    setSessionToken,
    isSessionValid,
    subscribeToPeer,
    unsubscribeFromPeer,
    subscribeToWusp,
    unsubscribeFromWusp,
    clearDeviceAuthRequest,
    // WebProxy binary-frame transport (used by webproxy-mux.ts)
    sendWebProxyFrame,
    registerWebProxyFrameListener,
    unregisterWebProxyFrameListener,
    // SSH stream functions
    openSSHStream,
    sendSSHData,
    sendSSHResize,
    sendSSHPing,
    closeSSHStream,
    isSSHStreamActive,
    // RouterOS dashboard stream functions
    openRouterOSStream,
    sendRouterOSCommand,
    closeRouterOSStream,
  };
}

export const wsStore = createWebSocketStore();

/**
 * Derived store for connection status
 */
export const wsConnected = derived(
  wsStore,
  ($ws) => $ws.status === "connected"
);

/**
 * Monotonic counter that ticks every time the WebSocket completes a fresh
 * key exchange (i.e. is fully usable). Subscribing stores can compare the
 * value to a locally-cached "last seen" generation to detect a reconnect
 * and refetch any state that may have drifted while the socket was down.
 *
 *   let lastGen = 0;
 *   wsConnectionGeneration.subscribe((gen) => {
 *     if (gen > lastGen) { lastGen = gen; refetchCachedState(); }
 *   });
 *
 * Resets to 0 on intentional disconnect (logout) so a fresh login starts
 * the counter clean.
 */
export const wsConnectionGeneration = derived(
  wsStore,
  ($ws) => $ws.connectionGeneration
);

/**
 * Derived store for connection message
 */
export const wsMessage = derived(wsStore, ($ws) => $ws.message);

/**
 * Derived store for peer statuses
 */
export const peerStatuses = derived(wsStore, ($ws) =>
  Array.from($ws.peerStatuses.values())
);

/**
 * Derived store for getting single peer status
 */
export function getPeerStatus(peerId: string) {
  return derived(wsStore, ($ws) => $ws.peerStatuses.get(peerId));
}
