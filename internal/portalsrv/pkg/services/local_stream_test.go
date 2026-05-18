package services

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// Tiny payload types — we don't need protobuf for testing the channel
// plumbing itself.
type pingReq struct{ N int }
type pongResp struct{ N int }

func TestLocalBidiStream_RoundTrip(t *testing.T) {
	ls := NewLocalBidiStream[pingReq, pongResp](context.Background(), 4)
	t.Cleanup(ls.Close)

	client := ls.Client()
	server := ls.Server()

	// Server echoes N back as a pong.
	done := make(chan error, 1)
	go func() {
		for {
			req, err := server.Recv()
			if errors.Is(err, io.EOF) {
				done <- nil
				return
			}
			if err != nil {
				done <- err
				return
			}
			if err := server.Send(&pongResp{N: req.N}); err != nil {
				done <- err
				return
			}
		}
	}()

	// Client sends 5 requests and collects 5 responses.
	for i := 0; i < 5; i++ {
		if err := client.Send(&pingReq{N: i}); err != nil {
			t.Fatalf("client.Send(%d): %v", i, err)
		}
	}
	if err := client.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	got := 0
	for i := 0; i < 5; i++ {
		resp, err := client.Recv()
		if err != nil {
			t.Fatalf("client.Recv(%d): %v", i, err)
		}
		if resp.N != i {
			t.Errorf("response %d: N = %d, want %d", i, resp.N, i)
		}
		got++
	}
	if got != 5 {
		t.Errorf("got %d responses, want 5", got)
	}

	// Server should exit cleanly when it sees EOF.
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("server exited with error: %v", err)
		}
	case <-time.After(time.Second):
		t.Error("server did not exit within 1s")
	}
}

func TestLocalBidiStream_ContextCancel_FailsBothSides(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	ls := NewLocalBidiStream[pingReq, pongResp](ctx, 1)
	t.Cleanup(ls.Close)

	client := ls.Client()
	server := ls.Server()

	cancel() // cancel the parent context

	if err := client.Send(&pingReq{N: 1}); !errors.Is(err, context.Canceled) {
		t.Errorf("client.Send after cancel = %v, want context.Canceled", err)
	}
	if err := server.Send(&pongResp{N: 1}); !errors.Is(err, context.Canceled) {
		t.Errorf("server.Send after cancel = %v, want context.Canceled", err)
	}
	if _, err := client.Recv(); !errors.Is(err, context.Canceled) {
		t.Errorf("client.Recv after cancel = %v, want context.Canceled", err)
	}
	if _, err := server.Recv(); !errors.Is(err, context.Canceled) {
		t.Errorf("server.Recv after cancel = %v, want context.Canceled", err)
	}
}

func TestLocalBidiStream_CloseSend_ServerSeesEOF(t *testing.T) {
	ls := NewLocalBidiStream[pingReq, pongResp](context.Background(), 1)
	t.Cleanup(ls.Close)

	client := ls.Client()
	server := ls.Server()

	if err := client.Send(&pingReq{N: 42}); err != nil {
		t.Fatalf("client.Send: %v", err)
	}
	if err := client.CloseSend(); err != nil {
		t.Fatalf("CloseSend: %v", err)
	}

	// First Recv on the server should yield the queued message.
	req, err := server.Recv()
	if err != nil {
		t.Fatalf("server.Recv #1: %v", err)
	}
	if req.N != 42 {
		t.Errorf("req.N = %d, want 42", req.N)
	}

	// Second Recv should see io.EOF.
	_, err = server.Recv()
	if !errors.Is(err, io.EOF) {
		t.Errorf("server.Recv #2 = %v, want io.EOF", err)
	}
}

func TestLocalBidiStream_ContextProperty(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ls := NewLocalBidiStream[pingReq, pongResp](ctx, 1)
	t.Cleanup(ls.Close)

	if ls.Client().Context() == nil {
		t.Error("Client.Context() = nil")
	}
	if ls.Server().Context() == nil {
		t.Error("Server.Context() = nil")
	}
	if ls.Client().Context().Err() != nil {
		t.Errorf("Client.Context() Err() = %v, want nil", ls.Client().Context().Err())
	}
}

func TestLocalBidiStream_Concurrent(t *testing.T) {
	const N = 200
	ls := NewLocalBidiStream[pingReq, pongResp](context.Background(), 16)
	t.Cleanup(ls.Close)

	client := ls.Client()
	server := ls.Server()

	// Server: count requests, echo each.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			req, err := server.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				t.Errorf("server.Recv: %v", err)
				return
			}
			if err := server.Send(&pongResp{N: req.N}); err != nil {
				return
			}
		}
	}()

	// Client: fan-out sends and fan-in recvs.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < N; i++ {
			if err := client.Send(&pingReq{N: i}); err != nil {
				t.Errorf("client.Send(%d): %v", i, err)
				return
			}
		}
		_ = client.CloseSend()
	}()

	got := 0
	for got < N {
		_, err := client.Recv()
		if err != nil {
			t.Fatalf("client.Recv after %d responses: %v", got, err)
		}
		got++
	}
	wg.Wait()
}
