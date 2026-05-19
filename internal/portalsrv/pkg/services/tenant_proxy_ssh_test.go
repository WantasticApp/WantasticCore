package services

import (
	"context"
	"testing"

	pb "WantasticCore/internal/types"
)

// newTestSSHStream constructs a LocalBidiStream and returns its client side.
// Use it to stand in for a real in-process SSH stream in unit tests.
func newTestSSHStream(ctx context.Context) *LocalBidiStreamClient[pb.SSHStreamMessage, pb.SSHStreamMessage] {
	return NewLocalBidiStream[pb.SSHStreamMessage, pb.SSHStreamMessage](ctx, 4).Client()
}

func TestCleanupSSHStreamHandlerStopsActiveStream(t *testing.T) {
	proxy := &TenantProxy{}
	stream := newTestSSHStream(context.Background())
	cancelCalls := 0

	session := &TenantSession{
		sshStreams: make(map[string]*SSHStreamHandler),
	}

	handler := &SSHStreamHandler{
		sessionID: "ssh-session",
		stream:    stream,
		cancel: func() {
			cancelCalls++
		},
		active:  true,
		inputCh: make(chan *pb.SSHStreamMessage, 1),
	}
	session.sshStreams[handler.sessionID] = handler

	proxy.cleanupSSHStreamHandler(session, handler.sessionID, nil)

	if _, exists := session.sshStreams[handler.sessionID]; exists {
		t.Fatalf("expected handler to be removed from session map")
	}
	if cancelCalls != 1 {
		t.Fatalf("expected cancel to be called once, got %d", cancelCalls)
	}

	handler.mu.Lock()
	defer handler.mu.Unlock()
	if handler.active {
		t.Fatalf("expected handler to be inactive after cleanup")
	}
	if handler.stream != nil {
		t.Fatalf("expected handler stream to be cleared after cleanup")
	}
	if handler.cancel != nil {
		t.Fatalf("expected handler cancel to be cleared after cleanup")
	}
}

func TestCleanupSSHStreamHandlerDoesNotRemoveReplacement(t *testing.T) {
	proxy := &TenantProxy{}
	currentStream := newTestSSHStream(context.Background())
	currentCancelCalls := 0
	staleCancelCalls := 0

	session := &TenantSession{
		sshStreams: make(map[string]*SSHStreamHandler),
	}

	current := &SSHStreamHandler{
		sessionID: "ssh-session",
		stream:    currentStream,
		cancel: func() {
			currentCancelCalls++
		},
		active:  true,
		inputCh: make(chan *pb.SSHStreamMessage, 1),
	}
	stale := &SSHStreamHandler{
		sessionID: "ssh-session",
		cancel: func() {
			staleCancelCalls++
		},
		active:  true,
		inputCh: make(chan *pb.SSHStreamMessage, 1),
	}
	session.sshStreams[current.sessionID] = current

	proxy.cleanupSSHStreamHandler(session, stale.sessionID, stale)

	if session.sshStreams[current.sessionID] != current {
		t.Fatalf("expected current handler to remain registered")
	}
	if currentCancelCalls != 0 {
		t.Fatalf("expected current handler cancel to remain untouched, got %d", currentCancelCalls)
	}
	if staleCancelCalls != 0 {
		t.Fatalf("expected stale handler cancel not to run, got %d", staleCancelCalls)
	}
}
