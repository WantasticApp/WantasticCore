package device

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"net/netip"
	"sync"
	"time"
)

type Coordinator struct {
	device *Device

	// Peer registry: public key -> peer info
	peers     map[NoisePublicKey]*PeerRegistryEntry
	idToPeer  map[uint32]*PeerRegistryEntry
	nextID    uint32
	mu        sync.RWMutex
	stopCh    chan struct{}
	closeOnce sync.Once
}

type PeerRegistryEntry struct {
	ID           uint32
	PublicKey    NoisePublicKey
	LocalAddr    net.UDPAddr
	ObservedAddr net.UDPAddr ``
	LastSeen     time.Time
	Peer         *Peer // Active WireGuard peer reference
	AssignedIP   netip.Addr

	// P2P capability assessment
	NATType       NATType
	P2PCapable    bool
	OfferExitNode bool
}

type NATType int

const (
	NATUnknown NATType = iota
	NATNone
	NATFullCone
	NATRestricted
	NATPortRestricted
	NATSymmetric
)

const (
	coordinatorInitialPeerID    = 1000
	coordinatorPeerListFreshTTL = 5 * time.Minute
	coordinatorPeerRetentionTTL = 15 * time.Minute
	coordinatorJanitorInterval  = 5 * time.Minute
)

func shouldAdvertiseLocalAddr(localAddr net.UDPAddr, assignedIP netip.Addr) bool {
	if !isUsablePunchAddr(localAddr) {
		return false
	}
	if assignedIP.IsValid() {
		addrIP, ok := netip.AddrFromSlice(localAddr.IP)
		if ok && addrIP == assignedIP {
			return false
		}
	}
	return true
}

func NewCoordinator(device *Device) *Coordinator {
	c := &Coordinator{
		device:   device,
		peers:    make(map[NoisePublicKey]*PeerRegistryEntry),
		idToPeer: make(map[uint32]*PeerRegistryEntry),
		nextID:   coordinatorInitialPeerID,
		stopCh:   make(chan struct{}),
	}
	go c.janitorLoop()
	return c
}

func (c *Coordinator) Close() {
	c.closeOnce.Do(func() {
		close(c.stopCh)
	})
}

// HandleP2PMessage processes MessageP2PType (type 6)
func (c *Coordinator) HandleP2PMessage(peer *Peer, msg *P2PMessage, srcAddr *net.UDPAddr) {
	// Update or create registry entry
	c.mu.Lock()
	var pubKey NoisePublicKey
	copy(pubKey[:], msg.PublicKey[:])

	entry, exists := c.peers[pubKey]
	if !exists {
		c.nextID++
		entry = &PeerRegistryEntry{
			ID:        c.nextID,
			PublicKey: pubKey,
		}
		c.peers[pubKey] = entry
		c.idToPeer[entry.ID] = entry
	}

	entry.Peer = peer
	entry.LastSeen = time.Now()
	entry.ObservedAddr = normalizeUDPAddr(*srcAddr)

	// If client reported local addr, store it
	if msg.LocalPort != 0 {
		localAddr := normalizeUDPAddr(msg.LocalAddr())
		if shouldAdvertiseLocalAddr(localAddr, entry.AssignedIP) {
			entry.LocalAddr = localAddr
		} else {
			entry.LocalAddr = net.UDPAddr{}
		}
	}

	// Extract Assigned IP from WireGuard allowed IPs trie
	c.device.allowedips.EntriesForPeer(peer, func(prefix netip.Prefix) bool {
		entry.AssignedIP = prefix.Addr()
		return false // only need the first one
	})

	// Assess NAT type
	c.assessNAT(entry)
	c.mu.Unlock()

	// Process based on subtype
	switch msg.Subtype {
	case P2PSubtypeRegister:
		c.device.log.Verbosef("Coordinator: peer(%s) handling Register", peer)
		c.handleRegister(peer, entry, msg, srcAddr)
	case P2PSubtypePeerList:
		c.device.log.Verbosef("Coordinator: peer(%s) handling PeerList", peer)
		c.handlePeerListRequest(peer, entry)
	case P2PSubtypePunchRequest:
		c.device.log.Verbosef("Coordinator: peer(%s) handling PunchRequest", peer)
		c.handlePunchRequest(peer, entry, msg)
	}
}

func (c *Coordinator) handleRegister(peer *Peer, entry *PeerRegistryEntry, msg *P2PMessage, srcAddr *net.UDPAddr) {
	// Send acknowledgment with assigned ID and observed endpoint
	resp := &P2PMessage{
		Subtype:  P2PSubtypeRegisterAck,
		TargetID: entry.ID,
	}
	copy(resp.PublicKey[:], c.device.staticIdentity.publicKey[:])
	resp.SetObservedAddr(srcAddr)

	c.device.log.Verbosef("Coordinator: Sending RegisterAck to peer(%s)", peer)
	c.sendP2PMessage(peer, resp)
}

func (c *Coordinator) handlePeerListRequest(peer *Peer, requester *PeerRegistryEntry) {
	c.mu.RLock()

	// Build list of other peers
	type PeerInfo struct {
		ID           uint32
		PublicKey    NoisePublicKey
		LocalAddr    net.UDPAddr
		ObservedAddr net.UDPAddr
		NATType      uint8
		P2PCapable   uint8
		AssignedIP   netip.Addr
	}

	var peers []PeerInfo
	staleCutoff := time.Now().Add(-coordinatorPeerListFreshTTL)
	for _, p := range c.peers {
		if p.ID == requester.ID {
			continue // Skip self
		}
		if p.LastSeen.Before(staleCutoff) {
			continue // Skip stale
		}

		peers = append(peers, PeerInfo{
			ID:           p.ID,
			PublicKey:    p.PublicKey,
			LocalAddr:    p.LocalAddr,
			ObservedAddr: p.ObservedAddr,
			NATType:      uint8(p.NATType),
			P2PCapable:   boolToUint8(p.P2PCapable),
			AssignedIP:   p.AssignedIP,
		})
	}
	c.mu.RUnlock()

	// Encode peer list into payload
	// Format: [count:4][peer1:78][peer2:78]...
	// Each peer: [id:4][pubkey:32][local_ip:16][local_port:2][observed_ip:16][observed_port:2][nat_type:1][capable:1][reserved:4]

	payload := make([]byte, 4+len(peers)*78)
	binary.BigEndian.PutUint32(payload[0:4], uint32(len(peers)))

	offset := 4
	for _, p := range peers {
		binary.BigEndian.PutUint32(payload[offset:], p.ID)
		copy(payload[offset+4:], p.PublicKey[:])
		copy(payload[offset+36:], p.LocalAddr.IP.To16())
		binary.BigEndian.PutUint16(payload[offset+52:], uint16(p.LocalAddr.Port))
		copy(payload[offset+54:], p.ObservedAddr.IP.To16())
		binary.BigEndian.PutUint16(payload[offset+70:], uint16(p.ObservedAddr.Port))
		payload[offset+72] = p.NATType
		payload[offset+73] = p.P2PCapable

		// Pack AssignedIP (IPv4) into reserved space
		if p.AssignedIP.Is4() {
			copy(payload[offset+74:offset+78], p.AssignedIP.AsSlice())
		}
		// offset+74:78 used for assigned IP
		offset += 78
	}

	resp := &P2PMessage{
		Subtype: P2PSubtypePeerList,
		Payload: payload,
	}
	copy(resp.PublicKey[:], c.device.staticIdentity.publicKey[:])

	c.sendP2PMessage(peer, resp)
}

func (c *Coordinator) handlePunchRequest(requester *Peer, reqEntry *PeerRegistryEntry, msg *P2PMessage) {
	targetID := msg.TargetID

	c.mu.RLock()
	target, ok := c.idToPeer[targetID]
	c.mu.RUnlock()

	if !ok || target.Peer == nil {
		return // Target unknown or offline
	}

	// Generate nonce for this punch session
	nonce := make([]byte, 8)
	rand.Read(nonce)

	// Send punch relay to both parties
	// To requester: here's target's info
	resp1 := &P2PMessage{
		Subtype:  P2PSubtypePunchRelay,
		TargetID: target.ID,
	}
	copy(resp1.PublicKey[:], c.device.staticIdentity.publicKey[:])
	copy(resp1.Nonce[:], nonce)
		if shouldAdvertiseLocalAddr(target.LocalAddr, target.AssignedIP) {
			resp1.SetLocalAddr(&target.LocalAddr)
		}
	resp1.SetObservedAddr(&target.ObservedAddr)

	c.sendP2PMessage(requester, resp1)

	// To target: here's requester's info
	resp2 := &P2PMessage{
		Subtype:  P2PSubtypePunchRelay,
		TargetID: reqEntry.ID,
	}
	copy(resp2.PublicKey[:], c.device.staticIdentity.publicKey[:])
	copy(resp2.Nonce[:], nonce)
		if shouldAdvertiseLocalAddr(reqEntry.LocalAddr, reqEntry.AssignedIP) {
			resp2.SetLocalAddr(&reqEntry.LocalAddr)
		}
	resp2.SetObservedAddr(&reqEntry.ObservedAddr)

	c.sendP2PMessage(target.Peer, resp2)
}

func (c *Coordinator) assessNAT(entry *PeerRegistryEntry) {
	// Simple NAT assessment
	if entry.LocalAddr.IP.Equal(entry.ObservedAddr.IP) &&
		entry.LocalAddr.Port == entry.ObservedAddr.Port {
		entry.NATType = NATNone
		entry.P2PCapable = true
		return
	}

	// Conservative: assume restricted cone, still try P2P
	entry.NATType = NATRestricted
	entry.P2PCapable = true
}

func (c *Coordinator) sendP2PMessage(peer *Peer, msg *P2PMessage) {
	// Send as MessageP2PType (type 6) directly via UDP
	// This skips the encryption queue and sends cleartext/encoded control message

	encoded := msg.Encode()
	// Use 4-byte LE uint32 header like all other WireGuard message types
	// receive.go reads: binary.LittleEndian.Uint32(packet[:4])
	packet := make([]byte, 4+len(encoded))
	binary.LittleEndian.PutUint32(packet[:4], MessageP2PType)
	copy(packet[4:], encoded)

	peer.SendBuffers([][]byte{packet})
}

func (c *Coordinator) janitorLoop() {
	ticker := time.NewTicker(coordinatorJanitorInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.cleanupStalePeers()
		case <-c.stopCh:
			return
		}
	}
}

func (c *Coordinator) cleanupStalePeers() {
	cutoff := time.Now().Add(-coordinatorPeerRetentionTTL)

	c.mu.Lock()
	defer c.mu.Unlock()

	for publicKey, entry := range c.peers {
		if entry.LastSeen.IsZero() || !entry.LastSeen.Before(cutoff) {
			continue
		}
		delete(c.peers, publicKey)
		delete(c.idToPeer, entry.ID)
	}
}

func boolToUint8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// HandleTUNControl processes MessageTUNControlType (type 7) over P2P tunnel
// These messages are used for negotiating Exit Node routing dynamically without gRPC
func (c *Coordinator) HandleTUNControl(peer *Peer, data []byte) {
	if len(data) == 0 {
		return
	}

	action := data[0]

	switch action {
	case 3: // Action 3: Client requests server to establish exit node route
		if len(data) < 33 {
			return
		}

		c.device.log.Verbosef("Coordinator: peer(%s) requested Exit Node Routing (Action 3)", peer)

		var targetPubKey [32]byte
		copy(targetPubKey[:], data[1:33])

		// Check if it's an all-zeros key (Direct Mode / Disable Exit Node)
		isDisable := true
		for _, b := range targetPubKey {
			if b != 0 {
				isDisable = false
				break
			}
		}

		if isDisable {
			c.device.log.Verbosef("Coordinator: peer(%s) requested to disable exit node (Direct Mode)", peer)
			// Acknowledge back to the requester to clear route
			resp := make([]byte, 33)
			resp[0] = 0 // Action 0: Revoke exit node
			peer.SendTUNControl(resp)
			return
		}

		// Look up the requested target peer
		targetPeer := c.device.LookupPeer(targetPubKey)
		if targetPeer == nil {
			c.device.log.Verbosef("Coordinator: Exit node target peer not found: %x", targetPubKey[:4])
			// Acknowledge failure back to requester by revoking
			resp := make([]byte, 33)
			resp[0] = 0 // Action 0: Revoke
			peer.SendTUNControl(resp)
			return
		}

		// Enforce that the destination has exit node capabilities & P2P capabilities enabled locally
		c.mu.RLock()
		targetEntry, ok := c.peers[targetPubKey]
		c.mu.RUnlock()

		if !ok || !targetEntry.P2PCapable || !targetEntry.OfferExitNode {
			c.device.log.Verbosef("Coordinator: Exit node routing denied! Target %x P2PCapable=%v OfferExitNode=%v", targetPubKey[:4], targetEntry != nil && targetEntry.P2PCapable, targetEntry != nil && targetEntry.OfferExitNode)
			resp := make([]byte, 33)
			resp[0] = 0 // Action 0: Revoke
			peer.SendTUNControl(resp)
			return
		}

		// Success!
		// Tell the target peer to become an exit node for the requester (Action 2)
		targetResp := make([]byte, 33)
		targetResp[0] = 2 // Action 2: You are now an exit node
		targetPeer.SendTUNControl(targetResp)

		// Tell the requester that the route is confirmed (Action 1)
		reqResp := make([]byte, 33)
		reqResp[0] = 1 // Action 1: Confirmed use this as exit node
		copy(reqResp[1:], targetPubKey[:])
		peer.SendTUNControl(reqResp)

		c.device.log.Verbosef("Coordinator: Confirmed Exit Node Routing for %s -> %s", peer, targetPeer)

	case 4: // Action 4: Toggle Offer Exit Node status updates
		if len(data) < 2 {
			return
		}
		enabled := data[1] == 1
		c.device.log.Verbosef("Coordinator: peer(%s) toggled local Exit Node Offer = %v", peer, enabled)

		c.mu.Lock()
		if entry, ok := c.peers[peer.PublicKey()]; ok {
			entry.OfferExitNode = enabled
		}
		c.mu.Unlock()
	}
}
