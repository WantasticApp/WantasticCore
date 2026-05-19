package webssh

import (
	"bytes"
	"io"
	"testing"
	"time"
)

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Close() error { return nil }

func TestSSHKeepaliveFailureBudgetUsesActiveSessionAllowance(t *testing.T) {
	if got := sshKeepaliveFailureBudget(0); got != sshKeepaliveMaxFailures {
		t.Fatalf("sshKeepaliveFailureBudget(0) = %d, want %d", got, sshKeepaliveMaxFailures)
	}
	if got := sshKeepaliveFailureBudget(1); got != sshKeepaliveActiveSessionMaxFailures {
		t.Fatalf("sshKeepaliveFailureBudget(1) = %d, want %d", got, sshKeepaliveActiveSessionMaxFailures)
	}
}

func TestSSHStreamReadWriteTouchesMuxActivityAndCounters(t *testing.T) {
	var stdin bytes.Buffer
	mux := &SSHMultiplexer{lastActive: time.Now().Add(-time.Minute)}
	session := &DirectSSHSession{}
	stream := &SSHStream{
		muxSess: &MuxSession{
			stdin:  nopWriteCloser{Writer: &stdin},
			stdout: bytes.NewReader([]byte("hello")),
		},
		mux:     mux,
		session: session,
	}

	beforeRead := mux.LastActive()
	buf := make([]byte, 5)
	n, err := stream.Read(buf)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if n != 5 || string(buf[:n]) != "hello" {
		t.Fatalf("Read() = (%d, %q), want (5, %q)", n, string(buf[:n]), "hello")
	}
	if stream.muxSess.BytesRecv != 5 {
		t.Fatalf("BytesRecv = %d, want 5", stream.muxSess.BytesRecv)
	}
	if !mux.LastActive().After(beforeRead) {
		t.Fatalf("LastActive after read = %v, want after %v", mux.LastActive(), beforeRead)
	}

	beforeWrite := mux.LastActive()
	written, err := stream.Write([]byte("ls\n"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != 3 {
		t.Fatalf("Write() = %d, want 3", written)
	}
	if stdin.String() != "ls\n" {
		t.Fatalf("stdin = %q, want %q", stdin.String(), "ls\n")
	}
	if stream.muxSess.BytesSent != 3 {
		t.Fatalf("BytesSent = %d, want 3", stream.muxSess.BytesSent)
	}
	if !mux.LastActive().After(beforeWrite) {
		t.Fatalf("LastActive after write = %v, want after %v", mux.LastActive(), beforeWrite)
	}
}
