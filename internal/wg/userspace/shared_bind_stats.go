package userspace

func (b *SharedPortBindV2) recordRouteMiss(msgType byte) {
	switch msgType {
	case messageTypeHandshakeResponse:
		b.routeMissHandshakeResponse.Add(1)
	case messageTypeCookieReply:
		b.routeMissCookieReply.Add(1)
	case messageTypeTransportData:
		b.routeMissTransport.Add(1)
	case messageTypeStats:
		b.routeMissStats.Add(1)
	}
}

func (b *SharedPortBindV2) recordReceiverParseFailure() {
	b.receiverParseFailures.Add(1)
}

func (b *SharedPortBindV2) recordTenantQueueFullDrop() {
	b.tenantQueueFullDrops.Add(1)
}

func (b *SharedPortBindV2) recordHandshakeBroadcastQueueFullDrop() {
	b.handshakeBroadcastQueueFullDrops.Add(1)
}

func (b *SharedPortBindV2) recordStaleDataQueueDrop() {
	b.staleDataQueueDrops.Add(1)
}

func (b *SharedPortBindV2) recordHandshakeRateLimited() {
	b.handshakeRateLimited.Add(1)
}
