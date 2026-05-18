package device

import (
	"encoding/binary"
	"net"
)

// P2P message subtypes
const (
	P2PSubtypeRegister     = 1 // Client -> Server: Register my endpoints
	P2PSubtypeRegisterAck  = 2 // Server -> Client: Ack with assigned ID + observed endpoint
	P2PSubtypePeerList     = 3 // Client -> Server: Request peer list / Server -> Client: Peer list
	P2PSubtypePunchRequest = 4 // Client -> Server: Request punch to target
	P2PSubtypePunchRelay   = 5 // Server -> Both: Relay punch intent + endpoints
	P2PSubtypePunchPacket  = 6 // Client <-> Client: Direct punch packets (encrypted)
	P2PSubtypeHeartbeat    = 7 // Client <-> Client: Keepalive for P2P session
)

// P2PMessage is sent as payload in MessageP2PType
// Total header: 4 + 32 + 16 + 2 + 16 + 2 + 8 + 4 = 84 bytes + variable payload
type P2PMessage struct {
	Subtype uint8   // One of P2PSubtype*
	_       [3]byte // Padding/reserved

	// Sender identification
	PublicKey [32]byte // WireGuard public key

	// Local endpoint (what client thinks it has)
	LocalIP   [16]byte // IPv6 or IPv4-mapped IPv6
	LocalPort uint16

	// Observed endpoint (filled by server)
	ObservedIP   [16]byte
	ObservedPort uint16

	// For punch coordination
	TargetID uint32  // Server-assigned peer ID
	Nonce    [8]byte // Punch coordination nonce

	// Variable payload: peer list, punch data, etc
	Payload []byte
}

func (m *P2PMessage) Encode() []byte {
	buf := make([]byte, 84+len(m.Payload))
	buf[0] = m.Subtype
	// buf[1:4] reserved
	copy(buf[4:36], m.PublicKey[:])
	copy(buf[36:52], m.LocalIP[:])
	binary.BigEndian.PutUint16(buf[52:54], m.LocalPort)
	copy(buf[54:70], m.ObservedIP[:])
	binary.BigEndian.PutUint16(buf[70:72], m.ObservedPort)
	binary.BigEndian.PutUint32(buf[72:76], m.TargetID)
	copy(buf[76:84], m.Nonce[:])
	copy(buf[84:], m.Payload)
	return buf
}

func DecodeP2PMessage(data []byte) *P2PMessage {
	if len(data) < 84 {
		return nil
	}
	m := &P2PMessage{
		Subtype: data[0],
		Payload: make([]byte, len(data)-84),
	}
	copy(m.PublicKey[:], data[4:36])
	copy(m.LocalIP[:], data[36:52])
	m.LocalPort = binary.BigEndian.Uint16(data[52:54])
	copy(m.ObservedIP[:], data[54:70])
	m.ObservedPort = binary.BigEndian.Uint16(data[70:72])
	m.TargetID = binary.BigEndian.Uint32(data[72:76])
	copy(m.Nonce[:], data[76:84])
	copy(m.Payload, data[84:])
	return m
}

func (m *P2PMessage) LocalAddr() net.UDPAddr {
	return net.UDPAddr{
		IP:   net.IP(m.LocalIP[:]),
		Port: int(m.LocalPort),
	}
}

func (m *P2PMessage) ObservedAddr() net.UDPAddr {
	return net.UDPAddr{
		IP:   net.IP(m.ObservedIP[:]),
		Port: int(m.ObservedPort),
	}
}

func (m *P2PMessage) SetLocalAddr(addr *net.UDPAddr) {
	if addr == nil {
		return
	}
	copy(m.LocalIP[:], addr.IP.To16())
	m.LocalPort = uint16(addr.Port)
}

func (m *P2PMessage) SetObservedAddr(addr *net.UDPAddr) {
	if addr == nil {
		return
	}
	copy(m.ObservedIP[:], addr.IP.To16())
	m.ObservedPort = uint16(addr.Port)
}

