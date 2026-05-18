// Session-based encryption for WebSocket messages.
// Uses X25519 ECDH for key exchange and AES-256-GCM for message encryption.

const SESSION_NONCE_SIZE = 12;

export interface EncryptionState {
  clientPrivateKey: Uint8Array | null;
  clientPublicKey: Uint8Array | null;
  serverPublicKey: Uint8Array | null;
  sharedSecret: Uint8Array | null;
  encryptionKey: CryptoKey | null;
  sessionId: string | null;
  enabled: boolean;
  sendCounter: bigint;
  recvCounter: bigint;
}

export interface EncryptedMessage {
  type: "encrypted";
  ciphertext: string;
}

export interface KeyExchangeMessage {
  type: "key_exchange";
  server_public_key: string;
  session_id: string;
}

export interface KeyExchangeResponse {
  type: "key_exchange";
  client_public_key: string;
}

const P = BigInt(
  "57896044618658097711785492504343953926634992332820282019728792003956564819949"
);
const A24 = BigInt(121665);

function modPow(base: bigint, exp: bigint, mod: bigint): bigint {
  let result = BigInt(1);
  base = ((base % mod) + mod) % mod;
  while (exp > 0n) {
    if (exp % 2n === 1n) {
      result = (result * base) % mod;
    }
    exp = exp / 2n;
    base = (base * base) % mod;
  }
  return result;
}

function modInverse(a: bigint, p: bigint): bigint {
  return modPow(a, p - 2n, p);
}

function bytesToBigInt(bytes: Uint8Array): bigint {
  let result = 0n;
  for (let i = 0; i < bytes.length; i++) {
    result |= BigInt(bytes[i]) << BigInt(i * 8);
  }
  return result;
}

function bigIntToBytes(num: bigint, length: number): Uint8Array {
  const bytes = new Uint8Array(length);
  let n = num;
  for (let i = 0; i < length; i++) {
    bytes[i] = Number(n & 0xffn);
    n >>= 8n;
  }
  return bytes;
}

const X25519_BASEPOINT = new Uint8Array(32);
X25519_BASEPOINT[0] = 9;

function x25519ScalarMult(scalar: Uint8Array, point: Uint8Array): Uint8Array {
  const k = new Uint8Array(scalar);
  k[0] &= 248;
  k[31] &= 127;
  k[31] |= 64;

  const kBigInt = bytesToBigInt(k);
  const u = bytesToBigInt(point) % P;

  let x_1 = u;
  let x_2 = 1n;
  let z_2 = 0n;
  let x_3 = u;
  let z_3 = 1n;
  let swap = 0n;

  for (let t = 254; t >= 0; t--) {
    const k_t = (kBigInt >> BigInt(t)) & 1n;
    swap ^= k_t;
    if (swap === 1n) {
      [x_2, x_3] = [x_3, x_2];
      [z_2, z_3] = [z_3, z_2];
    }
    swap = k_t;

    const A = (x_2 + z_2) % P;
    const AA = (A * A) % P;
    const B = (((x_2 - z_2) % P) + P) % P;
    const BB = (B * B) % P;
    const E = (((AA - BB) % P) + P) % P;
    const C = (x_3 + z_3) % P;
    const D = (((x_3 - z_3) % P) + P) % P;
    const DA = (D * A) % P;
    const CB = (C * B) % P;
    const DAaddCB = (DA + CB) % P;
    const DAsubCB = (((DA - CB) % P) + P) % P;
    x_3 = (DAaddCB * DAaddCB) % P;
    z_3 = (x_1 * DAsubCB * DAsubCB) % P;
    x_2 = (AA * BB) % P;
    z_2 = (E * ((AA + A24 * E) % P)) % P;
  }

  if (swap === 1n) {
    [x_2, x_3] = [x_3, x_2];
    [z_2, z_3] = [z_3, z_2];
  }

  const result = (x_2 * modInverse(z_2, P)) % P;
  return bigIntToBytes(result, 32);
}

export function generatePrivateKey(): Uint8Array {
  const privateKey = new Uint8Array(32);
  crypto.getRandomValues(privateKey);
  privateKey[0] &= 248;
  privateKey[31] &= 127;
  privateKey[31] |= 64;
  return privateKey;
}

export function getPublicKey(privateKey: Uint8Array): Uint8Array {
  return x25519ScalarMult(privateKey, X25519_BASEPOINT);
}

export function computeSharedSecret(
  privateKey: Uint8Array,
  peerPublicKey: Uint8Array
): Uint8Array {
  return x25519ScalarMult(privateKey, peerPublicKey);
}

export async function deriveSessionKey(
  sharedSecret: Uint8Array,
  sessionId: string
): Promise<CryptoKey> {
  // Create a proper ArrayBuffer from the Uint8Array to satisfy TypeScript
  const secretBuffer = new Uint8Array(sharedSecret).buffer as ArrayBuffer;
  const keyMaterial = await crypto.subtle.importKey(
    "raw",
    secretBuffer,
    "HKDF",
    false,
    ["deriveBits", "deriveKey"]
  );

  const salt = new TextEncoder().encode("wantastic-ws-session-v1");
  const info = new TextEncoder().encode("ws-session:" + sessionId);

  return crypto.subtle.deriveKey(
    {
      name: "HKDF",
      salt: salt,
      info: info,
      hash: "SHA-256",
    },
    keyMaterial,
    { name: "AES-GCM", length: 256 },
    false,
    ["encrypt", "decrypt"]
  );
}

export function createEncryptionState(): EncryptionState {
  return {
    clientPrivateKey: null,
    clientPublicKey: null,
    serverPublicKey: null,
    sharedSecret: null,
    encryptionKey: null,
    sessionId: null,
    enabled: false,
    sendCounter: 0n,
    recvCounter: 0n,
  };
}

export async function initializeEncryption(
  state: EncryptionState,
  serverPublicKeyBase64: string,
  sessionId: string
): Promise<string> {
  state.clientPrivateKey = generatePrivateKey();
  state.clientPublicKey = getPublicKey(state.clientPrivateKey);
  state.sessionId = sessionId;
  state.serverPublicKey = base64ToBytes(serverPublicKeyBase64);
  state.sharedSecret = computeSharedSecret(
    state.clientPrivateKey,
    state.serverPublicKey
  );
  state.encryptionKey = await deriveSessionKey(state.sharedSecret, sessionId);
  return bytesToBase64(state.clientPublicKey);
}

export function enableEncryption(state: EncryptionState): void {
  if (state.encryptionKey) {
    state.enabled = true;
  }
}

export async function encryptMessage(
  state: EncryptionState,
  plaintext: string
): Promise<string> {
  if (!state.enabled || !state.encryptionKey) {
    throw new Error("Encryption not enabled");
  }

  state.sendCounter++;
  const counter = state.sendCounter;

  const nonce = new Uint8Array(SESSION_NONCE_SIZE);
  let c = counter;
  for (let i = 7; i >= 0; i--) {
    nonce[i] = Number(c & 0xffn);
    c >>= 8n;
  }
  crypto.getRandomValues(nonce.subarray(8));

  const plaintextBytes = new TextEncoder().encode(plaintext);
  const ciphertextWithTag = await crypto.subtle.encrypt(
    { name: "AES-GCM", iv: nonce },
    state.encryptionKey,
    plaintextBytes
  );

  const result = new Uint8Array(
    8 + SESSION_NONCE_SIZE + ciphertextWithTag.byteLength
  );
  c = counter;
  for (let i = 7; i >= 0; i--) {
    result[i] = Number(c & 0xffn);
    c >>= 8n;
  }
  result.set(nonce, 8);
  result.set(new Uint8Array(ciphertextWithTag), 8 + SESSION_NONCE_SIZE);

  return bytesToBase64(result);
}

export async function decryptMessage(
  state: EncryptionState,
  ciphertextBase64: string
): Promise<string> {
  if (!state.enabled || !state.encryptionKey) {
    throw new Error("Encryption not enabled");
  }

  const ciphertext = base64ToBytes(ciphertextBase64);
  if (ciphertext.length < 36) {
    throw new Error("Invalid ciphertext: too short");
  }

  let counter = 0n;
  for (let i = 0; i < 8; i++) {
    counter = (counter << 8n) | BigInt(ciphertext[i]);
  }

  if (counter <= state.recvCounter && state.recvCounter > 0n) {
    throw new Error("Replay attack detected");
  }

  const nonce = ciphertext.slice(8, 8 + SESSION_NONCE_SIZE);
  const encryptedData = ciphertext.slice(8 + SESSION_NONCE_SIZE);

  const plaintextBytes = await crypto.subtle.decrypt(
    { name: "AES-GCM", iv: nonce },
    state.encryptionKey,
    encryptedData
  );

  state.recvCounter = counter;
  return new TextDecoder().decode(plaintextBytes);
}

export function isEncryptionEnabled(state: EncryptionState): boolean {
  return state.enabled && state.encryptionKey !== null;
}

function bytesToBase64(bytes: Uint8Array): string {
  let binary = "";
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

function base64ToBytes(base64: string): Uint8Array {
  const binary = atob(base64);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}
