package device

import (
	"bytes"
	"testing"
)

func TestEncodeExitNodeTUNControl(t *testing.T) {
	t.Parallel()

	var key NoisePublicKey
	for i := range key {
		key[i] = byte(i + 1)
	}
	tests := []struct {
		name   string
		got    []byte
		action byte
		key    []byte
	}{
		{name: "enable sharing", got: EncodeEnableExitNodeTUNControl(), action: tunControlEnableExitNode, key: make([]byte, 32)},
		{name: "use exit node", got: EncodeUseExitNodeTUNControl(key), action: tunControlUseExitNode, key: key[:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if len(tt.got) != 33 {
				t.Fatalf("payload length = %d, want 33", len(tt.got))
			}
			if tt.got[0] != tt.action || !bytes.Equal(tt.got[1:], tt.key) {
				t.Fatalf("payload = %x, want action %d and key %x", tt.got, tt.action, tt.key)
			}
		})
	}
}
