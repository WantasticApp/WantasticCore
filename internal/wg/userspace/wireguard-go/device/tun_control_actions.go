package device

const (
	tunControlUseExitNode    = 1
	tunControlEnableExitNode = 2
	tunControlPeerKeySize    = 32
)

func EncodeEnableExitNodeTUNControl() []byte {
	payload := make([]byte, 1+tunControlPeerKeySize)
	payload[0] = tunControlEnableExitNode
	return payload
}

func EncodeUseExitNodeTUNControl(exitNode NoisePublicKey) []byte {
	payload := make([]byte, 1+tunControlPeerKeySize)
	payload[0] = tunControlUseExitNode
	copy(payload[1:], exitNode[:])
	return payload
}
