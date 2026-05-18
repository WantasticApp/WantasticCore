# WUSP White Paper

## Wantastic USP over WireGuard

**Status:** Implementation-backed engineering white paper  
**Scope:** Current repository implementation as evaluated on 2026-04-26  
**Audience:** Platform engineers, protocol designers, device-management teams

**Authors**

- Abdelkarim Ouazmir <karim@wantastic.app>
- Adnane A. Zaid <adnane@wantastic.app>

**Primary references**

- [Broadband Forum TR-369 User Services Platform](https://www.broadband-forum.org/pdfs/tr-369-1-0-0.pdf)
- [Broadband Forum USP Data Models - TR-181 Device:2.20 with WireGuard](https://usp-data-models.broadband-forum.org/tr-181-2-20-1-usp-diffs.html)
- [RFC 8085 - UDP Usage Guidelines](https://datatracker.ietf.org/doc/html/rfc8085)
- [RFC 8899 - Packetization Layer Path MTU Discovery for Datagram Transports](https://datatracker.ietf.org/doc/html/rfc8899)
- [WireGuard official overview](https://www.wireguard.com/)
- [WireGuard protocol and cryptography](https://www.wireguard.com/protocol/)
- [WireGuard technical paper](https://www.wireguard.com/papers/wireguard.pdf)
- [WireGuard formal verification](https://www.wireguard.com/formal-verification/)

## Abstract

WUSP, short for **Wantastic USP over WireGuard**, is a custom transmission
protocol that carries USP-style device management operations inside an existing
WireGuard tunnel instead of exposing a separate management socket. It preserves
the core management semantics expected by USP controllers and agents - `Get`,
`Set`, `Add`, `Delete`, `Operate`, `Notify`, capability discovery, and transfer
initiation - while replacing the usual message transport with a compact binary
framing system optimized for encrypted in-tunnel delivery.

The implementation in this repository is not a generic Broadband Forum
transport module. It is a purpose-built protocol stack for the Wantastic
platform. Its strongest properties are transport locality, reuse of WireGuard's
cryptographic session, explicit request/response correlation, schema-aware
encoding, and direct alignment with TR-181 device data. Its main tradeoffs are
custom interoperability, UDP-fragment sensitivity at large response sizes, and
an incomplete end-to-end realization of the advertised bulk-transfer stream.

The design direction is easy to defend from first principles. TR-369 defines
USP around CRUD-ON style management messages and a formal device information
model, while WireGuard already gives Wantastic a secure, simple, roaming-capable
UDP tunnel with strong key agreement and authentication. WUSP takes advantage
of that existing trust and reachability layer instead of recreating it in a
separate management channel.

The honest caveat is equally important: TR-369 defines USP Records and standard
message-transfer expectations, while WUSP is a custom Wantastic transport. So
the right way to describe WUSP today is this:

**WUSP is a strong private transport for USP semantics in a WireGuard-native
system, but it is not a drop-in standard USP MTP and it still needs a few
critical production-hardening steps.**

## 1. Problem Statement

Traditional USP deployments usually require a management transport surface that
is distinct from the overlay tunnel carrying application traffic. That can mean
extra listeners, additional firewall policy, controller discovery complexity,
or another authentication layer on top of an already trusted encrypted tunnel.

That conventional split makes sense in multi-vendor deployments, but Wantastic
already operates a WireGuard-based overlay. WireGuard's own design goal is to
securely encapsulate packets over UDP using simple public-key configuration,
cryptokey routing, and built-in roaming. In a platform that already depends on
that model, keeping management inside the tunnel is a coherent engineering
choice rather than an arbitrary one.

WUSP addresses a different operational assumption:

- the device is already attached to the platform using WireGuard
- the management plane should stay inside that tunnel
- the controller and agent should reuse the same authenticated session
- device state should map directly to TR-181 objects and parameters
- management traffic should avoid opening another externally reachable port

The result is a tunnel-only protocol whose control plane rides inside a custom
WireGuard message type dedicated to USP device management.

## 2. Protocol Identity

WUSP is defined in this codebase as a combination of:

- a custom WireGuard payload type: `MessageWUSPType = 8`
- a WUSP control fragment format for MTU-safe delivery
- a WUSP request/response transport envelope for USP operations
- a nested typed message codec for TR-181 parameter payloads
- a WUSP data-model extension under `Device.WUSP.*`
- an optional transfer-stream frame format for larger uploads/downloads

The protocol also reports its own capabilities through a USP-style supported
protocol structure:

- **Name:** `wantastic-wusp-over-wireguard`
- **Version:** `1`
- **Control transport:** `wireguard-noise-fragmented-datagram`
- **Transfer transport:** `wireguard-noise-stream-packets`
- **Tunnel only:** `true`

## 3. Design Goals

The implementation strongly suggests five design goals:

1. **No extra management socket**
   Management traffic should stay inside WireGuard instead of opening another
   listener.

2. **USP semantics without USP protobuf transport**
   The protocol preserves the meaning of USP operations while using a custom
   binary encoding.

3. **Small, typed, schema-aware payloads**
   TR-181 parameters are encoded with stable type tags and optional path codes.

4. **Operational resilience on mobile or lossy links**
   The controller tracks reachability, request IDs, fragment TTLs, and failure
   backoff to avoid repeated long hangs.

5. **Strong alignment with device inventory and snapshots**
   The controller persists WUSP snapshots into the platform's database and
   exposes them through gRPC and the portal.

These goals are consistent with the source material:

- TR-369 defines USP as a management architecture with CRUD, operate, and
  notify semantics over a formal data model.
- TR-181 already models the managed object space that Wantastic wants to carry.
- Broadband Forum now includes `Device.WireGuard.*` in the standard model,
  which validates WireGuard as a first-class management concern.
- RFC 8085 explicitly treats UDP tunnels and reminds protocol designers that
  reliability, congestion behavior, and message sizing remain their
  responsibility.
- RFC 8899 recommends datagram-aware path MTU discovery above UDP when packet
  size needs to adapt robustly.

## 4. Layered Architecture

WUSP is best understood as a layered transport stack:

```text
USP semantics
  -> WUSP request/response envelope
    -> optional nested TR-181 message codec
      -> optional WUSP control fragmentation
        -> WireGuard encrypted transport packet
          -> UDP
```

For control messages the layers are:

1. **WireGuard custom message type**
   The transport marks WUSP packets with message type `8`.

2. **Control fragment envelope**
   Large control messages are split into MTU-safe fragments.

3. **USP transport envelope**
   The envelope carries method, request ID, selectors, metadata, and optional
   nested message or transfer request/result data.

4. **Nested message frame**
   Parameter values are encoded using a typed binary TR-181 message codec.

For bulk transfer, a separate stream frame format also exists, with phases for
open, chunk, ack, complete, and abort.

## 5. Data Model Strategy

WUSP is not only a transport. It also extends the managed device model.

The repository merges three model surfaces into one runtime device schema:

- imported TR-181 Device model
- TR-181 WireGuard model additions
- Wantastic-specific `Device.WUSP.*` objects and parameters

The `Device.WUSP.*` surface describes transport state and controller-facing
capabilities such as:

- `Enable`
- `Status`
- `ProtocolVersion`
- `ControllerEndpointID`
- `ControllerPublicKey`
- `MaxControlPayload`
- `ControlCompression`
- `TransferCompression`
- `RecommendedChunkSize`
- `TransferWindowSize`
- `TunnelOnly`
- `ReliableControl`

This is an important architectural choice: WUSP is self-describing through the
same USP-style model that it transports.

## 6. Wire Format Summary

### 6.1 WireGuard Message Layer

WUSP traffic is injected into a dedicated WireGuard transport class rather than
being routed as ordinary IP packets. That creates a clean separation between:

- overlay data packets
- P2P coordination packets
- tunnel-control packets
- WUSP management packets

This is a good design because it lets the device dispatch management traffic
before IP routing and before higher-level application parsing.

It also aligns well with the way WireGuard itself couples authenticated peers
to tunnel behavior: the official WireGuard documentation emphasizes that peers
are identified by public keys, packet authenticity is verified before traffic
is accepted, and the most recent authenticated endpoint can be learned for
roaming.

### 6.2 Control Fragment Layer

Control messages use a dedicated fragment format with:

- **Magic:** `0x54`
- **Version:** `1`
- **Header size:** `23` bytes
- **Default max datagram payload:** `1200` bytes

Each fragment carries:

- `MessageID`
- `Index`
- `Count`
- `RawSize`
- compression flag
- fragment payload bytes

This design is simple and appropriate for a UDP-carried control plane. The
implementation also supports whole-payload LZ4 compression before fragmenting,
which reduces fragment count when the payload is compressible.

RFC 8085 is directly relevant here: it warns that IP fragmentation hurts both
efficiency and reliability, that loss of a single fragment loses the entire
packet, and that applications should avoid sending UDP datagrams that exceed
path MTU. That makes WUSP's explicit fragment layer and conservative packet
budget a defensible choice. The exact `1200` byte budget is an implementation
decision, not a standards requirement, but it is directionally sound as a
conservative ceiling for Internet-facing UDP tunnels.

### 6.3 USP Transport Envelope

The control envelope uses a compact binary header:

- `Version` - 1 byte
- `Kind` - request or response
- `Method` - USP operation code
- `Flags` - presence bits
- `ID` - 64-bit correlation ID

Request methods currently include:

- `Get`
- `Set`
- `Add`
- `Delete`
- `GetInstances`
- `Operate`
- `Notify`
- `GetSupportedDM`
- `GetSupportedProtocol`
- `Upload`
- `Download`

The envelope can carry:

- string paths
- compact path selectors
- object selectors
- metadata
- nested typed message frames
- transfer request/result structures
- supported data-model descriptors
- supported protocol descriptors

### 6.4 Nested TR-181 Message Codec

The nested message codec is the part that carries parameter values.

Its frame begins with:

- magic bytes `"WU"` (`0x57 0x55`)
- format version `1`
- schema fingerprint (FNV-16 over registered paths)
- flags for compression, device ID, and timestamp

Each field contains:

- a field ID or inline path
- a one-byte type tag
- a type-specific encoded value

Supported value types include:

- null, boolean, uint, int, float
- string, bytes, time
- IPv4, IPv6, IPv4 prefix, IPv6 prefix
- MAC address
- list

This codec is one of WUSP's strongest technical choices. It gives the protocol
a typed, compact, forward-compatible representation without depending on a
protobuf binding of the BBF data model.

### 6.5 Compatibility Behavior

The compatibility model is thoughtful:

- registered parameters get stable numeric field IDs
- unknown future fields can still be sent inline by path
- tombstoned IDs do not shift
- decoders can skip unknown fields by type rules

There is also a notable implementation safeguard: nested parameter messages are
currently emitted with string paths rather than relying on numeric field IDs
across controller and agent binaries. This avoids path-registry drift between
independently built endpoints, at the cost of giving up some binary compactness
in exchanged field payloads.

## 7. Reliability Model

WUSP is carried over UDP through WireGuard, so it cannot assume in-order or
retransmitted delivery at the packet layer. The implementation compensates in
four ways. This framing is not just theory: RFC 8085 describes UDP as a
minimal, unreliable, best-effort transport and places the burden of message
size, reliability, and congestion behavior on the application or encapsulating
protocol.

### 7.1 Request/Response Correlation

Every request carries a non-zero 64-bit ID, and the controller keeps a pending
map until the matching response arrives or the request times out.

### 7.2 Fragment Reassembly Control

The controller keeps per-peer fragment buffers with:

- **fragment TTL:** 30 seconds
- **sweep interval:** 10 seconds
- **max incomplete buffers per peer:** 64

This is a strong operational safety feature because it bounds memory use and
prevents stale fragment accumulation from turning into a denial-of-service
condition.

### 7.3 Reachability and Fast Failure

The controller tracks per-peer liveness using:

- **online window:** 3 minutes since last inbound WUSP traffic
- **failure backoff:** 30 seconds after send failure or timeout

After one failed round trip, subsequent requests fast-fail instead of making
the dashboard wait through repeated full timeouts. That is an excellent product
decision and a practical transport heuristic.

### 7.4 Practical Limitation

WUSP control reliability is **not** full transport-level reliability in the
TCP sense. The current implementation detects loss by timeout and correlates
responses, but it does **not** implement:

- per-fragment acknowledgements
- control-fragment retransmission
- selective retransmission
- congestion control

As a result, large multi-fragment control responses remain vulnerable to loss.
The controller code explicitly avoids routine full-tree sync for this reason
and instead fetches a targeted subset of the data model.

This is the single biggest engineering gap between "good protocol design" and
"fully production-hardened transport." RFC 8085 and RFC 8899 both point toward
the same conclusion: UDP-based designs that care about correctness at scale
must treat packet sizing, loss detection, and recovery as first-class transport
problems, not as incidental edge cases.

## 8. Transfer Stream Design

WUSP defines a separate transfer-stream frame with:

- **Magic:** `0x53`
- **Version:** `1`
- **Phases:** `Open`, `Chunk`, `Ack`, `Complete`, `Abort`
- `SessionID`
- `RequestID`
- `Sequence`
- `AckSequence`
- `Offset`
- `TotalSize`
- optional path, filename, content type, metadata, and payload

Chunk compression is supported with LZ4 when the payload is likely to benefit.

This is a sensible design for larger uploads and downloads because it separates
bulk movement from the smaller RPC-oriented control envelope.

However, the current repository shows an important maturity gap:

- the stream frame codec is implemented
- upload/download control verbs exist
- transfer handlers exist on the agent abstraction
- but the stream layer is not visibly wired end-to-end through the controller
  receive loop, gRPC surface, and portal flows in the same way as the core
  `Get`/`Set`/`Notify` path

This means the transfer-stream portion should currently be treated as
**specified and partially implemented**, not as equally production-proven as
the core control plane.

From a standards perspective, that is fine as long as the product is honest
about it. It is better to say "the transfer stream exists but is not yet fully
productionized" than to imply parity with the core request/response path.

## 9. Security Model

WUSP inherits most of its security posture from WireGuard:

- confidentiality from the encrypted tunnel
- peer authentication from WireGuard keys
- no separate public management listener
- control traffic never leaves the established tunnel path

This is a major strength. It collapses the number of exposed trust boundaries.

That strength is real. WireGuard's public documentation emphasizes simple key
exchange, authenticated packet acceptance, modern cryptography, minimal attack
surface, and formal verification work covering key agreement, authenticity,
forward secrecy, and related properties. For Wantastic, that is a strong base
to build on.

That said, there are two different security layers in the code:

1. **Transport security**, which is strong and immediate because it rides on
   WireGuard.
2. **Controller trust modeling**, which is represented in the data model
   (`ControllerPublicKey`, certificates, trust roles) but appears more
   declarative than enforced in the current transport path.

So the transport itself is already strongly secured by WireGuard, but the more
formal WUSP-specific trust administration model looks ahead of its present
runtime enforcement.

This is also where WUSP departs from standard USP expectations. TR-369 spends a
lot of effort on certificate handling, endpoint IDs, trusted roles, and USP
Record security logic. WUSP replaces much of that transport-layer trust with
WireGuard peer identity, which is reasonable for a private overlay but should
be described as an architectural substitution, not as strict wire-level
conformance to standard USP security flows.

## 10. Performance Characteristics

WUSP is tuned for compact control traffic:

- control datagram budget is capped at `1200` bytes
- recommended transfer chunk size is `1120` bytes
- control payloads can be compressed before fragmentation
- nested message frames are compressed when large enough
- stream chunks are compressed only when the data looks compressible

These heuristics are reasonable. They avoid wasting CPU on tiny payloads and
reserve compression for messages where fragment reduction is likely to matter.

The next production step is to make packet sizing adaptive instead of mostly
static. RFC 8899 recommends Datagram PLPMTUD as the robust way for datagram
transports to discover whether a path can support the current packet size and
to reduce size when a black hole is encountered. WUSP does not implement that
yet.

The controller comments reveal the protocol's real-world scaling boundary: a
full `Device.*` snapshot can expand to roughly hundreds of control fragments,
which materially increases loss risk on UDP. The implementation's targeted sync
strategy is therefore not a workaround by accident - it is part of the current
performance model.

## 11. USP Alignment

WUSP is **USP-aligned**, but it is **not native USP record transport**.

It preserves the semantics of USP operations:

- object/parameter access
- operation invocation
- event notification
- capability discovery
- subscription management

But it does so using Wantastic-specific encodings and transport framing.

That distinction matters. TR-369 explicitly defines USP messages and describes
them as being carried in USP Records, with the message set expressed in
Protocol Buffers v3. WUSP keeps the semantics but changes the wire format and
transport assumptions. In other words, it is **semantic compatibility**, not
**wire compatibility**.

That has two consequences:

- **Positive:** it integrates tightly with WireGuard and the platform's TR-181
  runtime model.
- **Negative:** interoperability is limited to controllers and agents that
  implement this exact WUSP stack.

For private infrastructure, that tradeoff is acceptable. For multi-vendor USP
ecosystems, it is a substantial limitation.

## 12. Evaluation

### 12.1 What WUSP Does Well

WUSP is technically strong in the following areas:

- It keeps device management inside an existing encrypted tunnel.
- It avoids opening another management port.
- It maps naturally onto TR-181 parameter trees.
- It uses a compact binary codec with stable typing.
- It has a practical and well-implemented controller-side resilience model.
- It includes eventing and subscription semantics rather than only request/reply.
- It already integrates with snapshot persistence and portal workflows.

### 12.2 Main Risks and Weaknesses

The main concerns are:

- **Custom interoperability:** WUSP is platform-specific and not drop-in with
  generic USP controllers.
- **Control-plane loss sensitivity:** large multi-fragment control payloads do
  not have retransmission support.
- **Advertised reliability vs actual mechanics:** the protocol reports reliable
  behavior at the semantic level, but the control path still depends on timeout
  and retry rather than true fragment-level ARQ.
- **Transfer maturity gap:** the stream layer exists but does not appear as
  operationally complete as the core control plane.
- **Trust-model completeness:** WUSP controller trust objects exist in the
  schema, but the transport presently leans mostly on WireGuard identity.

### 12.3 Overall Assessment

WUSP is a **good private management transport** for a system that already
standardizes on WireGuard and controls both controller and agent software.

It is **not yet a general-purpose open USP transport profile**.

Its design quality is highest in:

- tunnel reuse
- schema-aware encoding
- controller ergonomics
- operational simplicity

Its weakest area is:

- end-to-end transport completeness for large or streamed exchanges

My realistic reading of the code today is:

- the control plane for targeted `Get`, `Set`, `Operate`, `Notify`, and
  state-sync workflows is promising and fairly coherent
- the protocol is well chosen for a **controlled, single-operator WireGuard
  environment**, which RFC 8085 explicitly recognizes as a special case
- the current implementation is **not yet ready** to be treated as a fully
  mature, general-purpose USP transport for arbitrary message sizes or bulk
  transfer paths

## 13. Production Verdict for Wantastic

If the question is "can Wantastic run this in production?", the honest answer
is:

**Yes, for a controlled rollout with targeted management operations. Not yet as
the final, fully hardened transport layer for all USP-like traffic.**

### Ready enough today for a limited production phase

- targeted device-state sync
- low-volume `Get` and `Set`
- controller-to-agent operations
- event delivery and dashboard live updates
- snapshot-oriented workflows

### Not ready enough yet for full production confidence

- arbitrary full-tree syncs over lossy Internet paths
- large multi-fragment control responses without recovery
- end-to-end upload/download stream workflows
- standards-grade transport claims around reliability and path sizing
- WUSP-specific controller trust enforcement beyond raw WireGuard identity

### What should be completed before broad production on Wantastic

1. **Datagram PLPMTUD or equivalent path-size adaptation**
   Adopt an RFC 8899 style approach, or at minimum implement active probing
   and per-peer size reduction when loss is detected.

2. **Fragment-loss recovery**
   Add ACK/NACK, replay, or another deterministic recovery mechanism for
   fragmented control messages.

3. **Rate control and circuit-breaker behavior**
   RFC 8085 recommends circuit-breaker style protection for UDP applications
   and tunnels that do not implement full congestion control. WUSP should add
   explicit safeguards rather than rely only on timeouts and operator caution.

4. **Truthful capability advertisement**
   Do not advertise `ReliableTransfer` or imply robust transfer support unless
   the end-to-end stream path is actually deployed and tested.

5. **Controller authorization hardening**
   Make `ControllerPublicKey`, endpoint ID, and trust-role logic part of real
   authorization decisions, or document clearly that WireGuard identity is the
   sole source of trust.

6. **Version-compatibility test matrix**
   Test older and newer controller/agent builds against each other for path
   drift, inline-path fallback, selector decoding, and partial-schema changes.

7. **Loss and soak testing**
   Test under packet loss, duplication, reordering, roaming, and long-lived
   sessions before calling the transport broadly production-ready.

8. **Operational observability**
   Export metrics for fragment counts, request timeouts, fast-fail rates,
   average response sizes, path-size downgrades, and stream aborts.

## 14. Recommendations

If WUSP is intended to become a long-lived production protocol, the next steps
should be:

1. **Add explicit control-message recovery**
   Introduce fragment ACK/NACK or request replay for large control exchanges.

2. **Complete the bulk-transfer path**
   Wire the stream frame format through controller, service, and portal layers,
   or mark it experimental until done.

3. **Publish a normative wire specification**
   Freeze header layouts, flags, limits, and compatibility rules in one formal
   document separate from code comments.

4. **Tighten capability truthfulness**
   Ensure `ReliableControl` and `ReliableTransfer` claims exactly match runtime
   behavior.

5. **Formalize trust enforcement**
   Bind controller authorization more explicitly to `ControllerPublicKey` and
   related trust objects if the platform needs security policy beyond raw
   WireGuard peering.

6. **Expand interoperability tests**
   Maintain controller/agent version-matrix tests to verify schema drift,
   inline-path fallback, and mixed-version decoding.

## 15. Conclusion

WUSP is a serious protocol design, not a thin wrapper around ad hoc device
commands. It combines a custom WireGuard control class, a fragment envelope, a
typed USP-style binary message format, controller-side request correlation, and
TR-181-aligned device semantics into one coherent management stack.

Its current implementation is already strong enough to support in-tunnel device
state sync, parameter access, operations, notifications, and snapshot-based
workflows. The protocol's architecture is well suited to a WireGuard-native
zero-trust platform.

The decisive conclusion is this:

**WUSP is well designed for private USP-style management over WireGuard, and it
is a credible foundation for Wantastic's production control plane. But it still
needs path-size adaptation, fragment-loss recovery, and a completed transfer
story before it should be presented as fully production-hardened.**
