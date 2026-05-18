package wuspcontroller

import (
	"context"
	"errors"
	"io"
	"strconv"
	"sync"
	"testing"
	"time"

	"WantasticCore/internal/wusp"
	"github.com/rs/zerolog"
)

// ── helpers ───────────────────────────────────────────────────────────────────

const testPeer = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

// newTestController creates a WUSPController wired to in-memory send/receive
// channels. No postgres, no network required.
func newTestController(t *testing.T, onEvent EventFunc) (*WUSPController, chan []byte) {
	t.Helper()
	outbound := make(chan []byte, 64)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		OnEvent:        onEvent,
		RequestTimeout: 2 * time.Second,
		Log:            zerolog.New(io.Discard),
	})
	ctrl.Start()
	t.Cleanup(ctrl.Stop)
	return ctrl, outbound
}

// ── OnBoardRequest ─────────────────────────────────────────────────────────

// TestWUSPControllerHandlesOnBoardRequest validates the end-to-end receive path
// for an agent OnBoardRequest:
//
//  1. Build a wire frame that matches what the agent's EmitOnBoardRequest emits.
//  2. Feed it to HandleInbound.
//  3. Verify OnEvent fires with the correct type and serial number.
func TestWUSPControllerHandlesOnBoardRequest(t *testing.T) {
	const serial = "test-device-12345"
	const protoVer = "1.0"

	eventCh := make(chan wusp.USPEvent, 1)
	ctrl, _ := newTestController(t, func(_ string, ev wusp.USPEvent) {
		eventCh <- ev
	})

	frame, err := encodeOnBoardFrame(1, serial, protoVer)
	if err != nil {
		t.Fatalf("encodeOnBoardFrame: %v", err)
	}

	ctrl.HandleInbound(context.Background(), testPeer, frame)

	select {
	case ev := <-eventCh:
		if ev.Type != wusp.USPEventTypeOnBoardRequest {
			t.Fatalf("event.Type=%d want %d (OnBoardRequest)", ev.Type, wusp.USPEventTypeOnBoardRequest)
		}
		if ev.OnBoard == nil {
			t.Fatal("event.OnBoard is nil")
		}
		if ev.OnBoard.SerialNumber != serial {
			t.Fatalf("serial=%q want %q", ev.OnBoard.SerialNumber, serial)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: OnEvent was not called")
	}
}

// TestWUSPControllerHandlesFragmentedOnBoardRequest verifies the fragment
// reassembly path: the frame is split into many small fragments and delivered
// one by one; OnEvent must only fire after the last fragment arrives.
func TestWUSPControllerHandlesFragmentedOnBoardRequest(t *testing.T) {
	const serial = "fragmented-device-67890"

	eventCh := make(chan wusp.USPEvent, 1)
	ctrl, _ := newTestController(t, func(_ string, ev wusp.USPEvent) {
		eventCh <- ev
	})

	frame, err := encodeOnBoardFrame(2, serial, "1.0")
	if err != nil {
		t.Fatalf("encodeOnBoardFrame: %v", err)
	}

	// 64-byte datagram budget → many fragments.
	const msgID = uint64(0xbeefcafe12345678)
	fragments, err := wusp.FragmentUSPControlPayload(frame, msgID, 64)
	if err != nil {
		t.Fatalf("FragmentUSPControlPayload: %v", err)
	}
	if len(fragments) < 2 {
		t.Fatalf("expected ≥2 fragments for 64-byte budget, got %d", len(fragments))
	}

	for i, frag := range fragments {
		ctrl.HandleInbound(context.Background(), testPeer, frag)
		if i < len(fragments)-1 {
			// Must not fire until the last fragment.
			select {
			case ev := <-eventCh:
				t.Fatalf("OnEvent fired early after fragment %d/%d: %+v", i+1, len(fragments), ev)
			default:
			}
		}
	}

	select {
	case ev := <-eventCh:
		if ev.Type != wusp.USPEventTypeOnBoardRequest {
			t.Fatalf("event.Type=%d want %d", ev.Type, wusp.USPEventTypeOnBoardRequest)
		}
		if ev.OnBoard == nil || ev.OnBoard.SerialNumber != serial {
			t.Fatalf("unexpected OnBoard: %+v", ev.OnBoard)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout: OnEvent was not called after all fragments")
	}
}

// ── Request / Response round-trip ────────────────────────────────────────────

// TestWUSPControllerGetRoundTrip validates that a controller Get round-trip
// completes when the agent echoes a well-formed response.
func TestWUSPControllerGetRoundTrip(t *testing.T) {
	ctrl, outbound := newTestController(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		wg     sync.WaitGroup
		got    wusp.USPAgentResponse
		gotErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		got, gotErr = ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	}()

	// Collect all outbound fragments sent by the controller and reassemble them
	// into the original request.
	rawReq := collectReassembled(t, outbound)
	req, err := wusp.DecodeUSPAgentRequest(rawReq)
	if err != nil {
		t.Fatalf("DecodeUSPAgentRequest: %v", err)
	}
	if req.Method != wusp.USPAgentMethodGet {
		t.Fatalf("method=%v want Get", req.Method)
	}

	// Build and deliver the agent response directly (no fragmentation needed for small frames).
	respFrame, err := wusp.EncodeUSPAgentResponse(wusp.USPAgentResponse{
		ID:     req.ID,
		Method: req.Method,
		Message: &wusp.Message{
			Fields: []wusp.Field{{
				Path: "Device.DeviceInfo.HostName",
				Val:  wusp.String("test-host"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("EncodeUSPAgentResponse: %v", err)
	}
	ctrl.HandleInbound(context.Background(), testPeer, respFrame)

	wg.Wait()
	if gotErr != nil {
		t.Fatalf("Get: %v", gotErr)
	}
	if got.Error != "" {
		t.Fatalf("response error=%q", got.Error)
	}
	val, ok := got.Message.Get("Device.DeviceInfo.HostName")
	if !ok || val.AsString() != "test-host" {
		t.Fatalf("unexpected response value: %+v", got.Message)
	}
}

// TestWUSPControllerRequestTimeout verifies that when no response arrives the
// controller returns a wrapped context-deadline error.
func TestWUSPControllerRequestTimeout(t *testing.T) {
	ctrl, _ := newTestController(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Get did not respect deadline: took %v", elapsed)
	}
}

// TestWUSPControllerFailureBackoff verifies the dashboard "fast-fail after first
// timeout" resilience claim: the first request to a silent peer times out at
// the configured RequestTimeout (RTT-bounded by ctx); the very next request
// short-circuits with ErrPeerNotReachable in <50ms; the next inbound packet
// from the recovered peer clears LastFailure so subsequent requests proceed.
func TestWUSPControllerFailureBackoff(t *testing.T) {
	ctrl, _ := newTestController(t, nil)

	// First request — agent never responds, ctx deadline trips.
	ctx1, cancel1 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel1()
	if _, err := ctrl.Get(ctx1, testPeer, "Device.DeviceInfo.HostName"); err == nil {
		t.Fatal("expected timeout on first Get, got nil error")
	}

	// roundTrip's ctx.Done branch must have stamped LastFailure on the session.
	ctrl.sessionsMu.RLock()
	s := ctrl.sessions[testPeer]
	ctrl.sessionsMu.RUnlock()
	if s == nil || s.LastFailure.IsZero() {
		t.Fatal("expected LastFailure to be set after timeout")
	}

	// Second request — must fast-fail with ErrPeerNotReachable, not hang.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel2()
	start := time.Now()
	_, err := ctrl.Get(ctx2, testPeer, "Device.DeviceInfo.HostName")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected ErrPeerNotReachable on second Get, got nil")
	}
	if !errors.Is(err, ErrPeerNotReachable) {
		t.Fatalf("expected ErrPeerNotReachable, got %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("second Get did not fast-fail: took %v", elapsed)
	}

	// Simulate a recovery inbound packet — LastFailure must clear so the
	// dashboard works again on the next click instead of waiting out the
	// 30-second backoff window.
	ctrl.markPeerInbound(testPeer)
	ctrl.sessionsMu.RLock()
	s = ctrl.sessions[testPeer]
	ctrl.sessionsMu.RUnlock()
	if s == nil || !s.LastFailure.IsZero() {
		t.Fatal("expected LastFailure to clear on next inbound packet")
	}
	if !ctrl.peerReachable(testPeer) {
		t.Fatal("expected peer to be reachable after recovery inbound")
	}
}

func TestWUSPControllerRetriesIdempotentRequestOnTimeout(t *testing.T) {
	outbound := make(chan []byte, 64)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		RequestTimeout: 100 * time.Millisecond,
		Log:            zerolog.New(io.Discard),
	})
	ctrl.Start()
	defer ctrl.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan struct{})
	var (
		resp wusp.USPAgentResponse
		err  error
	)
	go func() {
		defer close(done)
		resp, err = ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	}()

	first := collectReassembled(t, outbound)
	req1, decErr := wusp.DecodeUSPAgentRequest(first)
	if decErr != nil {
		t.Fatalf("DecodeUSPAgentRequest(first): %v", decErr)
	}

	second := collectReassembled(t, outbound)
	req2, decErr := wusp.DecodeUSPAgentRequest(second)
	if decErr != nil {
		t.Fatalf("DecodeUSPAgentRequest(second): %v", decErr)
	}
	if req1.ID == req2.ID {
		t.Fatalf("retry reused request ID %d", req1.ID)
	}

	respFrame, encErr := wusp.EncodeUSPAgentResponse(wusp.USPAgentResponse{
		ID:     req2.ID,
		Method: req2.Method,
		Message: &wusp.Message{
			Fields: []wusp.Field{{
				Path: "Device.DeviceInfo.HostName",
				Val:  wusp.String("retry-ok"),
			}},
		},
	})
	if encErr != nil {
		t.Fatalf("EncodeUSPAgentResponse: %v", encErr)
	}
	ctrl.HandleInbound(context.Background(), testPeer, respFrame)
	<-done

	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if got, _ := resp.Message.Get("Device.DeviceInfo.HostName"); got.AsString() != "retry-ok" {
		t.Fatalf("unexpected response: %+v", resp.Message)
	}
	if budget := ctrl.peerControlPayloadBudget(testPeer); budget >= wusp.WUSPMaxDatagramPayload {
		t.Fatalf("expected reduced control budget after retry, got %d", budget)
	}
}

func TestWUSPControllerDoesNotRetryMutatingRequest(t *testing.T) {
	outbound := make(chan []byte, 64)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		RequestTimeout: 100 * time.Millisecond,
		Log:            zerolog.New(io.Discard),
	})
	ctrl.Start()
	defer ctrl.Stop()

	msg := wusp.NewMessage()
	msg.Set("Device.ManagementServer.PeriodicInformEnable", wusp.Bool(true))

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := ctrl.Set(ctx, testPeer, msg)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	_ = collectReassembled(t, outbound)
	select {
	case <-outbound:
		t.Fatal("unexpected retry for mutating request")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestWUSPControllerAddsResponseBudgetHint(t *testing.T) {
	ctrl, outbound := newTestController(t, nil)

	ctrl.markPeerInbound(testPeer)
	ctrl.reducePeerControlPayload(testPeer)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	}()

	frame := collectReassembled(t, outbound)
	req, err := wusp.DecodeUSPAgentRequest(frame)
	if err != nil {
		t.Fatalf("DecodeUSPAgentRequest: %v", err)
	}
	got := req.Metadata[wusp.MetadataKeyResponseMaxControlPayload]
	want := strconv.Itoa(ctrl.peerControlPayloadBudget(testPeer))
	if got != want {
		t.Fatalf("response budget hint=%q want=%q", got, want)
	}
	<-done
}

// TestWUSPControllerAgentErrorPropagated verifies that when the agent replies
// with a non-empty Error field, the controller surfaces it as an *AgentError.
func TestWUSPControllerAgentErrorPropagated(t *testing.T) {
	ctrl, outbound := newTestController(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var (
		wg     sync.WaitGroup
		gotErr error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, gotErr = ctrl.Get(ctx, testPeer, "Device.X_CUSTOM.ReadOnly")
	}()

	rawReq := collectReassembled(t, outbound)
	req, err := wusp.DecodeUSPAgentRequest(rawReq)
	if err != nil {
		t.Fatalf("DecodeUSPAgentRequest: %v", err)
	}

	errFrame, err := wusp.EncodeUSPAgentResponse(wusp.USPAgentResponse{
		ID:     req.ID,
		Method: req.Method,
		Error:  "parameter not supported",
	})
	if err != nil {
		t.Fatalf("EncodeUSPAgentResponse: %v", err)
	}
	ctrl.HandleInbound(context.Background(), testPeer, errFrame)

	wg.Wait()
	if gotErr == nil {
		t.Fatal("expected error, got nil")
	}
	var ae *AgentError
	if !IsAgentError(gotErr) {
		t.Fatalf("expected *AgentError, got %T: %v", gotErr, gotErr)
	}
	ae = gotErr.(*AgentError)
	if ae.Message != "parameter not supported" {
		t.Fatalf("AgentError.Message=%q", ae.Message)
	}
}

// TestWUSPControllerFragmentBufferCap verifies that the per-peer fragment buffer
// is capped at maxFragmentBufs to prevent memory exhaustion.
func TestWUSPControllerFragmentBufferCap(t *testing.T) {
	ctrl, _ := newTestController(t, nil)

	// Inject maxFragmentBufs+5 distinct incomplete messages (first fragment only).
	for i := range maxFragmentBufs + 5 {
		frag, err := wusp.EncodeUSPControlFragment(wusp.USPControlFragment{
			MessageID: uint64(i + 1),
			Index:     0,
			Count:     3, // claim 3 fragments but never send the rest
			Data:      []byte("orphan"),
		})
		if err != nil {
			t.Fatalf("EncodeUSPControlFragment[%d]: %v", i, err)
		}
		ctrl.HandleInbound(context.Background(), testPeer, frag)
	}

	ctrl.fragmentsMu.Lock()
	count := len(ctrl.fragments[testPeer])
	ctrl.fragmentsMu.Unlock()

	if count > maxFragmentBufs {
		t.Fatalf("fragment buffer exceeded cap: count=%d max=%d", count, maxFragmentBufs)
	}
}

// TestWUSPControllerConcurrentPeers verifies the controller handles concurrent
// inbound events from many distinct peers without data races.
func TestWUSPControllerConcurrentPeers(t *testing.T) {
	const peerCount = 50

	var mu sync.Mutex
	received := make(map[string]int)
	ctrl, _ := newTestController(t, func(peer string, ev wusp.USPEvent) {
		if ev.Type == wusp.USPEventTypeOnBoardRequest {
			mu.Lock()
			received[peer]++
			mu.Unlock()
		}
	})

	var wg sync.WaitGroup
	for i := range peerCount {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			peer := testPeer[:len(testPeer)-2] + string(rune('A'+idx%26)) + "="
			frame, err := encodeOnBoardFrame(uint64(idx+1), "serial-"+string(rune('A'+idx%26)), "1.0")
			if err != nil {
				t.Errorf("peer %d encodeOnBoardFrame: %v", idx, err)
				return
			}
			ctrl.HandleInbound(context.Background(), peer, frame)
		}(i)
	}
	wg.Wait()

	// Give async goroutines a moment to complete.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	total := 0
	for _, n := range received {
		total += n
	}
	mu.Unlock()

	if total != peerCount {
		t.Fatalf("received %d events want %d", total, peerCount)
	}
}

// TestWUSPControllerStaleFragmentSweep verifies the background sweep evicts
// fragments that arrived longer than fragmentTTL ago.
func TestWUSPControllerStaleFragmentSweep(t *testing.T) {
	ctrl, _ := newTestController(t, nil)

	// Inject an incomplete 2-fragment message.
	frag0, _ := wusp.EncodeUSPControlFragment(wusp.USPControlFragment{
		MessageID: 9999,
		Index:     0,
		Count:     2,
		Data:      []byte("first-half"),
	})
	ctrl.HandleInbound(context.Background(), testPeer, frag0)

	// Back-date the buffer so it appears stale.
	ctrl.fragmentsMu.Lock()
	if byMsgID, ok := ctrl.fragments[testPeer]; ok {
		if buf, ok := byMsgID[9999]; ok {
			buf.arrivedAt = time.Now().Add(-(fragmentTTL + time.Second))
		}
	}
	ctrl.fragmentsMu.Unlock()

	ctrl.sweepStaleFragments()

	ctrl.fragmentsMu.Lock()
	_, still := ctrl.fragments[testPeer][9999]
	ctrl.fragmentsMu.Unlock()

	if still {
		t.Fatal("stale fragment buffer was not evicted by sweep")
	}

	stats := ctrl.StatsSnapshot()
	if stats.FragmentEvictions == 0 {
		t.Fatal("expected stale fragment sweep to increment fragment evictions")
	}
}

func TestWUSPControllerStatsSnapshotTracksRetryBudgetAndLatency(t *testing.T) {
	outbound := make(chan []byte, 64)
	ctrl := New(Options{
		Send: func(_ string, data []byte) error {
			outbound <- append([]byte(nil), data...)
			return nil
		},
		RequestTimeout: 100 * time.Millisecond,
		Log:            zerolog.New(io.Discard),
	})
	ctrl.Start()
	defer ctrl.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = ctrl.Get(ctx, testPeer, "Device.DeviceInfo.HostName")
	}()

	first := collectReassembled(t, outbound)
	req1, err := wusp.DecodeUSPAgentRequest(first)
	if err != nil {
		t.Fatalf("DecodeUSPAgentRequest(first): %v", err)
	}
	second := collectReassembled(t, outbound)
	req2, err := wusp.DecodeUSPAgentRequest(second)
	if err != nil {
		t.Fatalf("DecodeUSPAgentRequest(second): %v", err)
	}
	if req1.ID == req2.ID {
		t.Fatalf("retry reused request ID %d", req1.ID)
	}

	respFrame, err := wusp.EncodeUSPAgentResponse(wusp.USPAgentResponse{
		ID:     req2.ID,
		Method: req2.Method,
		Message: &wusp.Message{
			Fields: []wusp.Field{{
				Path: "Device.DeviceInfo.HostName",
				Val:  wusp.String("retry-ok"),
			}},
		},
	})
	if err != nil {
		t.Fatalf("EncodeUSPAgentResponse: %v", err)
	}
	ctrl.HandleInbound(context.Background(), testPeer, respFrame)
	<-done

	stats := ctrl.StatsSnapshot()
	if stats.RoundTrips != 1 {
		t.Fatalf("RoundTrips=%d want 1", stats.RoundTrips)
	}
	if stats.RoundTripRetries != 1 {
		t.Fatalf("RoundTripRetries=%d want 1", stats.RoundTripRetries)
	}
	if stats.RoundTripTimeouts != 1 {
		t.Fatalf("RoundTripTimeouts=%d want 1", stats.RoundTripTimeouts)
	}
	if stats.RoundTripSuccesses != 1 {
		t.Fatalf("RoundTripSuccesses=%d want 1", stats.RoundTripSuccesses)
	}
	if stats.BudgetReductions == 0 {
		t.Fatal("expected at least one budget reduction")
	}
	if stats.OutboundRequests != 2 {
		t.Fatalf("OutboundRequests=%d want 2", stats.OutboundRequests)
	}
	if stats.RoundTripLatencyTotal <= 0 {
		t.Fatalf("RoundTripLatencyTotal=%v want >0", stats.RoundTripLatencyTotal)
	}

	peerStats := ctrl.PeerSessionSnapshot(testPeer)
	if peerStats.ControlPayloadBudget >= wusp.WUSPMaxDatagramPayload {
		t.Fatalf("ControlPayloadBudget=%d want reduced budget", peerStats.ControlPayloadBudget)
	}
}

func TestWUSPControllerStatsSnapshotTracksFragmentsAndTransferFrames(t *testing.T) {
	ctrl, _ := newTestController(t, nil)

	frame, err := encodeOnBoardFrame(7, "stats-device", "1.0")
	if err != nil {
		t.Fatalf("encodeOnBoardFrame: %v", err)
	}
	fragments, err := wusp.FragmentUSPControlPayload(frame, 7, 64)
	if err != nil {
		t.Fatalf("FragmentUSPControlPayload: %v", err)
	}
	for _, frag := range fragments {
		ctrl.HandleInbound(context.Background(), testPeer, frag)
	}

	streamFrame, err := wusp.EncodeUSPTransferStreamFrame(wusp.USPTransferStreamFrame{
		SessionID: 99,
		RequestID: 1,
		Method:    wusp.USPAgentMethodUpload,
		Phase:     wusp.USPTransferStreamAbort,
	})
	if err != nil {
		t.Fatalf("EncodeUSPTransferStreamFrame: %v", err)
	}
	ctrl.HandleInbound(context.Background(), testPeer, streamFrame)

	stats := ctrl.StatsSnapshot()
	if stats.InboundControlFragments != uint64(len(fragments)) {
		t.Fatalf("InboundControlFragments=%d want %d", stats.InboundControlFragments, len(fragments))
	}
	if stats.FragmentReassemblies != 1 {
		t.Fatalf("FragmentReassemblies=%d want 1", stats.FragmentReassemblies)
	}
	if stats.InboundTransferFrames != 1 {
		t.Fatalf("InboundTransferFrames=%d want 1", stats.InboundTransferFrames)
	}
	if stats.TransferFramesReceived != 1 {
		t.Fatalf("TransferFramesReceived=%d want 1", stats.TransferFramesReceived)
	}
	if stats.TransferUnknownSessions != 1 {
		t.Fatalf("TransferUnknownSessions=%d want 1", stats.TransferUnknownSessions)
	}
}

// ── wire helpers ─────────────────────────────────────────────────────────────

// encodeOnBoardFrame builds the exact wire frame that the agent's
// EmitOnBoardRequest emits, using EncodeEventToRequest + EncodeUSPAgentRequest.
func encodeOnBoardFrame(id uint64, serial, protoVer string) ([]byte, error) {
	req := wusp.EncodeEventToRequest(wusp.USPEvent{
		Type:    wusp.USPEventTypeOnBoardRequest,
		ObjPath: "Device.",
		OnBoard: &wusp.USPOnBoardInfo{
			SerialNumber:                   serial,
			AgentSupportedProtocolVersions: protoVer,
		},
	}, id)
	return wusp.EncodeUSPAgentRequest(req)
}

// collectReassembled drains the outbound channel until it can reassemble a
// complete USP control-fragment message, then returns the payload.
// For single-fragment (or unfragmented) messages it returns the first frame.
func collectReassembled(t *testing.T, ch <-chan []byte) []byte {
	t.Helper()
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()

	var pending []wusp.USPControlFragment
	for {
		select {
		case frame := <-ch:
			frag, isFrag, err := wusp.DecodeUSPControlFragment(frame)
			if err != nil {
				t.Fatalf("DecodeUSPControlFragment: %v", err)
			}
			if !isFrag {
				return frame // unfragmented
			}
			pending = append(pending, frag)
			if uint32(len(pending)) >= frag.Count {
				payload, err := wusp.ReassembleUSPControlFragments(pending)
				if err != nil {
					t.Fatalf("ReassembleUSPControlFragments: %v", err)
				}
				return payload
			}
		case <-timeout.C:
			t.Fatal("timeout waiting for outbound fragments")
		}
	}
}
