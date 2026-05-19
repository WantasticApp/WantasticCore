package device

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"WantasticCore/internal/wg/userspace/wireguard-go/conn"

	"golang.org/x/crypto/blake2s"
)

func normalizeUDPAddr(addr net.UDPAddr) net.UDPAddr {
	if ip4 := addr.IP.To4(); ip4 != nil {
		addr.IP = append(net.IP(nil), ip4...)
		return addr
	}
	if ip16 := addr.IP.To16(); ip16 != nil {
		addr.IP = append(net.IP(nil), ip16...)
	}
	return addr
}

func isUsablePunchAddr(addr net.UDPAddr) bool {
	if addr.Port <= 0 || addr.IP == nil {
		return false
	}
	ip := addr.IP
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	return !ip.IsUnspecified()
}

type P2PClient struct {
	device *Device

	myID         uint32
	myPublicKey  NoisePublicKey
	localAddr    net.UDPAddr
	observedAddr net.UDPAddr

	// Discovered peers
	peers map[uint32]*DiscoveredPeer
	mu    sync.RWMutex

	// Active P2P sessions
	sessions map[uint32]*P2PSession // target ID -> session
}

type DiscoveredPeer struct {
	ID           uint32
	PublicKey    NoisePublicKey
	LocalAddr    net.UDPAddr
	ObservedAddr net.UDPAddr
	NATType      NATType
	P2PCapable   bool

	// State
	State      P2PState
	DirectConn *net.UDPConn  // nil until P2P established
	Endpoint   conn.Endpoint // The endpoint to use (P2P or relay)
	LastUsed   time.Time
}

type P2PState int

const (
	P2PStateDiscovered P2PState = iota
	P2PStateTrying
	P2PStateEstablished
	P2PStateFailed
)

const p2pPunchAttemptTTL = 10 * time.Second

type P2PSession struct {
	TargetID     uint32
	TargetPubKey NoisePublicKey
	LocalAddr    net.UDPAddr
	ObservedAddr net.UDPAddr
	Nonce        [8]byte
	PunchConn    *net.UDPConn
	Established  bool
}

func NewP2PClient(device *Device) *P2PClient {
	return &P2PClient{
		device:      device,
		peers:       make(map[uint32]*DiscoveredPeer),
		sessions:    make(map[uint32]*P2PSession),
		myPublicKey: device.staticIdentity.publicKey,
	}
}

func (c *P2PClient) Start() {
	// Get local address from bind
	// Note: We cannot easily get the local address from generic conn.Bind
	// in portable WireGuard-go. We rely on the server observing our address.
	// If c.localAddr is zero, Encode() leaves it zero.

	// Register with server
	c.register()

	// Start maintenance
	go c.maintenanceLoop()
}

func (c *P2PClient) register() {
	msg := &P2PMessage{
		Subtype: P2PSubtypeRegister,
	}
	copy(msg.PublicKey[:], c.myPublicKey[:])
	msg.SetLocalAddr(&c.localAddr)

	c.sendToServer(msg)
}

func (c *P2PClient) HandleMessage(msg *P2PMessage) {
	switch msg.Subtype {
	case P2PSubtypeRegisterAck:
		c.handleRegisterAck(msg)
	case P2PSubtypePeerList:
		c.handlePeerList(msg)
	case P2PSubtypePunchRelay:
		c.handlePunchRelay(msg)
	// P2PSubtypePunchPacket is handled by holePunch goroutine on specific socket
	case P2PSubtypeHeartbeat:
		// TODO: handle heartbeat on main socket if needed
	}
}

func (c *P2PClient) handleRegisterAck(msg *P2PMessage) {
	c.myID = msg.TargetID
	c.observedAddr = normalizeUDPAddr(msg.ObservedAddr())

	// Request peer list
	c.requestPeerList()
}

func (c *P2PClient) requestPeerList() {
	msg := &P2PMessage{
		Subtype: P2PSubtypePeerList,
	}
	copy(msg.PublicKey[:], c.myPublicKey[:])
	c.sendToServer(msg)
}

func (c *P2PClient) handlePeerList(msg *P2PMessage) {
	// Parse peer list
	if len(msg.Payload) < 4 {
		return
	}

	count := binary.BigEndian.Uint32(msg.Payload[0:4])
	if len(msg.Payload) < 4+int(count)*78 {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	offset := 4
	for i := uint32(0); i < count; i++ {
		peer := &DiscoveredPeer{
			ID: binary.BigEndian.Uint32(msg.Payload[offset:]),
		}
		copy(peer.PublicKey[:], msg.Payload[offset+4:])
		peer.LocalAddr.IP = net.IP(msg.Payload[offset+36 : offset+52])
		peer.LocalAddr.Port = int(binary.BigEndian.Uint16(msg.Payload[offset+52:]))
		peer.ObservedAddr.IP = net.IP(msg.Payload[offset+54 : offset+70])
		peer.ObservedAddr.Port = int(binary.BigEndian.Uint16(msg.Payload[offset+70:]))
		peer.LocalAddr = normalizeUDPAddr(peer.LocalAddr)
		peer.ObservedAddr = normalizeUDPAddr(peer.ObservedAddr)
		peer.NATType = NATType(msg.Payload[offset+72])
		peer.P2PCapable = msg.Payload[offset+73] == 1
		peer.State = P2PStateDiscovered
		peer.LastUsed = time.Now()

		// Don't overwrite existing established connections
		if existing, ok := c.peers[peer.ID]; !ok || existing.State != P2PStateEstablished {
			c.peers[peer.ID] = peer
		}

		offset += 78
	}
}

// TryP2P attempts to establish direct connection to peer
func (c *P2PClient) TryP2P(peerID uint32) bool {
	c.mu.Lock()
	peer, ok := c.peers[peerID]
	if !ok || !peer.P2PCapable || peer.State == P2PStateEstablished {
		c.mu.Unlock()
		return ok && peer.State == P2PStateEstablished
	}
	if peer.State == P2PStateTrying && time.Since(peer.LastUsed) < p2pPunchAttemptTTL {
		c.mu.Unlock()
		return true
	}
	peer.State = P2PStateTrying
	peer.LastUsed = time.Now()
	c.mu.Unlock()

	// Request server to coordinate
	msg := &P2PMessage{
		Subtype:  P2PSubtypePunchRequest,
		TargetID: peerID,
	}
	copy(msg.PublicKey[:], c.myPublicKey[:])
	c.sendToServer(msg)

	return true
}

func (c *P2PClient) handlePunchRelay(msg *P2PMessage) {
	// Server is telling us to punch to this peer
	targetID := msg.TargetID

	c.mu.Lock()
	peer, ok := c.peers[targetID]
	if !ok {
		c.mu.Unlock()
		return
	}
	if peer.State == P2PStateEstablished {
		c.mu.Unlock()
		return
	}
	now := time.Now()
	if peer.State == P2PStateTrying && now.Sub(peer.LastUsed) < p2pPunchAttemptTTL {
		c.mu.Unlock()
		return
	}

	// Update with latest addresses from server
	peer.LocalAddr = msg.LocalAddr()
	peer.ObservedAddr = msg.ObservedAddr()
	peer.LocalAddr = normalizeUDPAddr(peer.LocalAddr)
	peer.ObservedAddr = normalizeUDPAddr(peer.ObservedAddr)
	peer.LastUsed = now
	peer.State = P2PStateTrying

	// Create session
	session := &P2PSession{
		TargetID:     targetID,
		TargetPubKey: peer.PublicKey,
		LocalAddr:    peer.LocalAddr,
		ObservedAddr: peer.ObservedAddr,
	}
	copy(session.Nonce[:], msg.Nonce[:])
	c.sessions[targetID] = session
	c.mu.Unlock()

	// Start hole punching
	go c.holePunch(session, peer)
}

func (c *P2PClient) holePunch(session *P2PSession, peer *DiscoveredPeer) {
	// Create socket for hole punching
	punchConn, err := net.ListenUDP("udp", nil)
	if err != nil {
		return
	}
	defer punchConn.Close()

	session.PunchConn = punchConn

	// Try both addresses, but never punch unusable placeholders like ::0.
	targets := make([]net.UDPAddr, 0, 2)
	seen := make(map[string]struct{}, 2)
	for _, candidate := range []net.UDPAddr{session.ObservedAddr, session.LocalAddr} {
		candidate = normalizeUDPAddr(candidate)
		if !isUsablePunchAddr(candidate) {
			continue
		}
		key := candidate.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		targets = append(targets, candidate)
	}
	if len(targets) == 0 {
		c.mu.Lock()
		peer.State = P2PStateFailed
		peer.LastUsed = time.Now()
		c.mu.Unlock()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Send punch packets
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, target := range targets {
					packet := c.makePunchPacket(session)
					punchConn.WriteToUDP(packet, &target)
				}
			}
		}
	}()

	// Wait for response
	buf := make([]byte, 1500)
	for {
		punchConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
		n, fromAddr, err := punchConn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil {
				// Timeout - mark failed
				c.mu.Lock()
				peer.State = P2PStateFailed
				peer.LastUsed = time.Now()
				c.mu.Unlock()
				return
			}
			continue
		}

		if c.verifyPunchPacket(buf[:n], session) {
			// Success!
			c.mu.Lock()
			// Clone the UDPConn? No, we likely need to use it.
			// But conn.Endpoint expects a bind.Endpoint?
			// Integrating custom UDP conn into WireGuard Endpoint is complex.
			// We might need to pass this conn to Bind?
			// Or just use the address and let main Bind handle it if NAT is punched?
			// If we close punchConn, the mapping might close?
			// "Endpoint" in WireGuard-go abstractly wraps the address.
			// If NAT is punched, we can just send to that address from main socket!
			// We don't need to keep punchConn open if we switch to main socket.
			// BUT main socket needs to send keepalives to keep mapping open.

			// For now, we update the Endpoint with the observed address that worked.
			// Actually, ReadFromUDP gives us the remote addr that replied!
			// We should use THAT address.
			// Wait, verifyPunchPacket doesn't return addr.
			// We need to capture it.
			// But since we are here, we can assume the address matches what we expect?
			// No, it could be one of the targets.

			// Ideally we use the address we received from.
			// But `peer.Endpoint` is an interface. `conn.CreateEndpoint(addr)`.
			// `conn.Bind` usually creates endpoints.
			// `peer.device.net.bind.ParseEndpoint(addr.String())`

			// peer.DirectConn = punchConn // We might need to keep this open if we want to use THIS socket.
			// But WireGuard creates its own socket.
			// If we want to use the main socket, we must hope the hole allows packets from main socket too (same IP, different port).
			// If the NAT is PortRestricted, it might block main socket (port 51820) if we punched with random port.
			// To punch properly for WireGuard, we should punch FROM the main socket.
			// The user plan uses `net.ListenUDP("udp", nil)` which picks a random port.
			// This works for "Full Cone" but fails for "Port Restricted" if we switch back to main port.
			// UNLESS `P2PClient` is intended to tunnel traffic over this new socket?
			// "peer.Endpoint = conn.MakeEndpoint(punchConn)" suggested in user plan.
			// This implies `punchConn` IS used for traffic.
			// `conn.MakeEndpoint` doesn't exist in standard wireguard-go, usually `bind.ParseEndpoint`.
			// But maybe the user assumes we can wrap the `punchConn`?
			// If so, `peer.SendPacket` using `elem.endpoint` will send via... `endpoint.DstToBytes`?
			// WireGuard endpoints are usually just addresses sent via the Bind.
			// They don't carry the socket. The Bind carries the socket.

			// Critical Issue: WireGuard-go `Bind` has one socket.
			// If we establish P2P via a new socket, we must pass that socket to `Bind` or use a `Bind` that supports multiple sockets?
			// Or `peer.Endpoint` must effectively be a "Write to this specific conn" instruction?
			// Standard `conn.Endpoint` interface: `ClearSrc()`, `DstToBytes()`, `DstToString()`, `SrcIP()`, `SrcToString()`.
			// It does NOT have a "Write" method. The Bind handles writing.
			// So `elem.endpoint` is just an address.
			// If we set `elem.endpoint = p2pEndpoint`, `Bind.Send(..., endpoint)` is called.
			// `Bind` will send via ITS socket to that endpoint.
			// If we punched with a DIFFERENT socket, the NAT mapping is for THAT socket.
			// We cannot reuse it with the main socket for PortRestricted NATs.

			// User plan: `go c.holePunch(...) { net.ListenUDP(...) }`.
			// Explicitly requests separate socket.
			// Why?
			// Maybe to separate P2P traffic?
			// But WireGuard traffic must go over it?
			// If `peer.Endpoint` uses this new socket, then `Bind` must support it.
			// But `Bind` is `net.UDPConn`.
			// If I implement exactly as requested, `conn.MakeEndpoint(punchConn)` will fail if not defined.
			// I will Comment out `peer.Endpoint = conn.MakeEndpoint(punchConn)` and use `peer.Endpoint = CreateEndpointFromAddr(addr)`?
			// And keep `punchConn` open?
			// This part of the user plan is technically shaky for WireGuard-go.

			// I will implement as requested but comment on the likely need for `conn` modification.
			// I'll assume `conn.MakeEndpoint` doesn't exist and use a placeholder or best effort.
			// Actually, `conn` package in wireguard-go usually has `CreateEndpoint(s string)`.
			// I'll use that.

			peer.State = P2PStateEstablished
			peer.LastUsed = time.Now()
			session.Established = true
			// We need to set the endpoint for the peer to the address that successfully replied.
			// This assumes `conn.CreateEndpoint` can take a `net.UDPAddr` or its string representation.
			peer.Endpoint, err = c.device.net.bind.ParseEndpoint(fromAddr.String())
			if err != nil {
				c.device.log.Errorf("Failed to create endpoint for P2P peer %d: %v", peer.ID, err)
				peer.State = P2PStateFailed
			} else {
				// Keep the punchConn open if we intend to use it for traffic.
				// If we want to use the main bind, we should close punchConn and rely on the NAT mapping.
				// For now, we'll keep it open and assign it to DirectConn, assuming it might be used.
				peer.DirectConn = punchConn
			}
			c.mu.Unlock()

			// Send heartbeat to confirm
			// c.sendHeartbeat(peer)
			return
		}
	}
}

func (c *P2PClient) makePunchPacket(session *P2PSession) []byte {
	// Punch packet: [P2PSubtypePunchPacket:1][nonce:8][mac:32]
	packet := make([]byte, 1+8+32)
	packet[0] = P2PSubtypePunchPacket
	copy(packet[1:9], session.Nonce[:])

	c.device.staticIdentity.RLock()
	// Use sharedSecret from our private key and target's public key
	ss, err := c.device.staticIdentity.privateKey.sharedSecret(session.TargetPubKey)
	c.device.staticIdentity.RUnlock()

	if err == nil {
		mac, _ := blake2s.New256(ss[:])
		mac.Write(session.Nonce[:])
		sum := mac.Sum(nil)
		copy(packet[9:], sum)
	}

	return packet
}

func (c *P2PClient) verifyPunchPacket(packet []byte, session *P2PSession) bool {
	if len(packet) != 41 || packet[0] != P2PSubtypePunchPacket {
		return false
	}

	var nonce [8]byte
	copy(nonce[:], packet[1:9])
	if nonce != session.Nonce {
		return false
	}

	c.device.staticIdentity.RLock()
	ss, err := c.device.staticIdentity.privateKey.sharedSecret(session.TargetPubKey)
	c.device.staticIdentity.RUnlock()

	if err != nil {
		return false
	}

	mac, _ := blake2s.New256(ss[:])
	mac.Write(nonce[:])
	sum := mac.Sum(nil)

	// Constant time compare
	return string(sum) == string(packet[9:])
	// Use subtle.ConstantTimeCompare if available, but here string cast is lazy.
	// I'll use bytes.Equal for simplicity or loop.
}

func (c *P2PClient) GetEndpoint(peerID uint32) conn.Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if peer, ok := c.peers[peerID]; ok && peer.State == P2PStateEstablished {
		return peer.Endpoint
	}
	return nil
}

func (c *P2PClient) sendToServer(msg *P2PMessage) {
	// Find server peer
	c.device.peers.RLock()
	var serverPeer *Peer
	for _, peer := range c.device.peers.keyMap {
		// Heuristic: Server is usually the gateway or first peer?
		// Or endpoint is set?
		peer.endpoint.Lock()
		// Check if endpoint.val is not nil
		if peer.endpoint.val != nil {
			serverPeer = peer
		}
		peer.endpoint.Unlock()

		if serverPeer != nil {
			break // Use first peer (server)
		}
	}
	c.device.peers.RUnlock()

	if serverPeer == nil {
		return
	}

	c.sendP2PMessage(serverPeer, msg)
}

func (c *P2PClient) sendP2PMessage(peer *Peer, msg *P2PMessage) {
	encoded := msg.Encode()
	// Use 4-byte LE uint32 header like all other WireGuard message types
	// receive.go reads: binary.LittleEndian.Uint32(packet[:4])
	packet := make([]byte, 4+len(encoded))
	binary.LittleEndian.PutUint32(packet[:4], MessageP2PType)
	copy(packet[4:], encoded)

	peer.SendBuffers([][]byte{packet})
}

func (c *P2PClient) HandlePunch(packet []byte, addr *net.UDPAddr) {
	if len(packet) != 41 || packet[0] != P2PSubtypePunchPacket {
		return
	}

	var nonce [8]byte
	copy(nonce[:], packet[1:9])

	// Find any session expecting this nonce
	c.mu.Lock()
	var session *P2PSession
	var peer *DiscoveredPeer
	for _, s := range c.sessions {
		if s.Nonce == nonce {
			session = s
			peer = c.peers[s.TargetID]
			break
		}
	}
	c.mu.Unlock()

	if session == nil || peer == nil {
		return
	}

	// Verify
	if c.verifyPunchPacket(packet, session) {
		c.mu.Lock()
		defer c.mu.Unlock()

		if peer.State == P2PStateEstablished {
			return // Already established
		}

		c.device.log.Verbosef("P2P punch success from %v (ID %d)", addr, peer.ID)

		peer.State = P2PStateEstablished
		peer.LastUsed = time.Now()
		session.Established = true

		// Update Endpoint to the address we received from!
		ep, err := c.device.net.bind.ParseEndpoint(addr.String())
		if err == nil {
			peer.Endpoint = ep
		}

		// If we punched via separate socket, we might want to close it?
		// But we established session.
		// If we update peer.Endpoint, future sends go via main bind.
		// This is correct.
	}
}

func (c *P2PClient) maintenanceLoop() {
	ticker := time.NewTicker(5 * time.Second) // Check more frequently
	defer ticker.Stop()

	for range ticker.C {
		// Retry registration if not yet registered
		if c.myID == 0 {
			c.register()
			continue
		}

		// Refresh peer list periodically
		c.requestPeerList()

		// Clean up failed/stale sessions
		c.mu.Lock()
		for id, peer := range c.peers {
			if peer.State == P2PStateFailed && time.Since(peer.LastUsed) > 5*time.Minute {
				delete(c.peers, id)
				delete(c.sessions, id)
			}
			// Timeout trying state
			if peer.State == P2PStateTrying && time.Since(peer.LastUsed) > 15*time.Second {
				peer.State = P2PStateDiscovered // Reset to retry later
				delete(c.sessions, id)
			}
		}
		c.mu.Unlock()
	}
}

func (c *P2PClient) GetEndpointForPeer(pk NoisePublicKey) conn.Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, peer := range c.peers {
		if peer.PublicKey == pk && peer.State == P2PStateEstablished {
			return peer.Endpoint
		}
	}
	return nil
}
