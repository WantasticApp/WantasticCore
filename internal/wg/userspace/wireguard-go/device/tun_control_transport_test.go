package device

import (
	"bytes"
	"testing"
	"time"
)

func TestTUNControlNoiseTransportRoundTrip(t *testing.T) {
	goroutineLeakCheck(t)
	pair := genTestPair(t, false)
	received := make(chan []byte, 1)
	pair[1].dev.SetTUNControlHandler(func(_ *Peer, payload []byte) { received <- payload })
	pair.Send(t, Ping, nil)

	want := []byte{1, 0xde, 0xad, 0xbe, 0xef}
	mustSinglePeerForTUNControl(t, pair[0].dev).SendTUNControl(want)
	select {
	case got := <-received:
		if !bytes.Equal(got, want) {
			t.Fatalf("TUN control payload = %x, want %x", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for authenticated TUN control payload")
	}
	select {
	case packet := <-pair[1].tun.Inbound:
		t.Fatalf("TUN control payload leaked into network TUN: %x", packet)
	case <-time.After(100 * time.Millisecond):
	}
}

func mustSinglePeerForTUNControl(tb testing.TB, dev *Device) *Peer {
	tb.Helper()
	dev.peers.RLock()
	defer dev.peers.RUnlock()
	if len(dev.peers.keyMap) != 1 {
		tb.Fatalf("peer count = %d, want 1", len(dev.peers.keyMap))
	}
	for _, peer := range dev.peers.keyMap {
		return peer
	}
	tb.Fatal("peer map unexpectedly empty")
	return nil
}
