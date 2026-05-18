package core

import (
	"WantasticCore/internal/errs"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/server"
	"WantasticCore/internal/webssh"

	"github.com/rs/zerolog/log"
)

// WebSSHServiceServer implements WebSSHService.
type WebSSHServiceServer struct {
	srv           *server.Server
	websshBaseURL string // Base URL for WebSocket connections (e.g., "wss://localhost:8081")
}

// NewWebSSHServiceServer creates a new WebSSHService gRPC server
func NewWebSSHServiceServer(srv *server.Server, websshBaseURL string) *WebSSHServiceServer {
	return &WebSSHServiceServer{
		srv:           srv,
		websshBaseURL: websshBaseURL,
	}
}

// CreateWebSSHSession creates a new WebSSH session with SSH credentials
func (s *WebSSHServiceServer) CreateWebSSHSession(ctx context.Context, req *pb.CreateWebSSHSessionRequest) (*pb.CreateWebSSHSessionResponse, error) {
	// Validate request
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}
	if req.PeerIp == "" {
		return nil, errs.InvalidArgumentE("peer_ip is required")
	}
	if req.Username == "" {
		return nil, errs.InvalidArgumentE("username is required")
	}
	if req.PrivateKeyPassphrase != "" && req.PrivateKey == "" {
		return nil, errs.InvalidArgumentE("private_key is required when private_key_passphrase is set")
	}

	// Set defaults
	sshPort := req.SshPort
	if sshPort <= 0 {
		sshPort = 22
	}
	if sshPort > 65535 {
		return nil, errs.InvalidArgumentE("ssh_port must be between 1 and 65535")
	}

	rows := req.TerminalRows
	if rows <= 0 {
		rows = 24
	}
	cols := req.TerminalCols
	if cols <= 0 {
		cols = 80
	}

	// Look up peer ID by IP address for metadata tracking
	peers, err := s.srv.ListPeers(req.TenantId)
	if err != nil {
		return nil, errs.Internalf("failed to list peers: %v", err)
	}

	peerID := ""
	peerIP := req.PeerIp
	var foundPeer *server.PeerMetadata
	for _, peer := range peers {
		// Match IP (strip /32 suffix if present)
		assignedIP := strings.TrimSuffix(peer.AssignedIP, "/32")
		if assignedIP == peerIP {
			peerID = peer.ID
			foundPeer = peer
			break
		}
	}

	if peerID == "" {
		return nil, errs.NotFoundE("peer not found with specified IP address")
	}

	// Use scanned SSH port if available (from daily port scan), fallback to provided/default port
	if req.SshPort <= 0 && foundPeer != nil && foundPeer.ScannedSSHPort > 0 {
		sshPort = int32(foundPeer.ScannedSSHPort)
		log.Debug().
			Str("tenant_id", req.TenantId).
			Str("peer_id", peerID).
			Int32("scanned_ssh_port", sshPort).
			Msg(" Using scanned SSH port from peer metadata")
	}

	// Create WebSSH session
	sessionID, err := s.srv.CreateWebSSHSession(
		req.TenantId,
		peerID,
		req.PeerIp,
		int(sshPort),
		req.Username,
		req.Password,
		req.PrivateKey,
		req.PrivateKeyPassphrase,
		requestUserAgent(ctx, req.UserAgent),
		int(rows),
		int(cols),
	)
	if err != nil {
		return &pb.CreateWebSSHSessionResponse{
			Success:      false,
			ErrorMessage: err.Error(),
		}, nil
	}

	// Session created - client will use gRPC StreamSSH for terminal I/O
	return &pb.CreateWebSSHSessionResponse{
		SessionId:    sessionID,
		WebsocketUrl: "", // Deprecated: now using gRPC streaming
		Success:      true,
	}, nil
}

// GetWebSSHSession returns information about an active WebSSH session
func (s *WebSSHServiceServer) GetWebSSHSession(ctx context.Context, req *pb.GetWebSSHSessionRequest) (*pb.GetWebSSHSessionResponse, error) {
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}

	session, err := s.srv.GetWebSSHSession(req.SessionId)
	if err != nil {
		return nil, errs.NotFoundf("WebSSH session not found: %v", err)
	}

	// Session existence in the handler's map indicates it's active
	// Sessions are removed when closed, so if we got here, it's active
	return &pb.GetWebSSHSessionResponse{
		Session: &pb.WebSSHSession{
			Id:           session.ID,
			TenantId:     session.TenantID,
			PeerIp:       session.PeerIP,
			SshPort:      int32(session.Port),
			Username:     session.Username,
			StartedAt:    pb.TimestampFromTime(session.StartedAt),
			LastActive:   pb.TimestampFromTime(session.LastActive),
			Active:       session.Status == "active", // Session in handler map = active; removed on close
			BytesSent:    session.BytesSent,
			BytesRecv:    session.BytesRecv,
			TerminalRows: int32(session.TerminalRows),
			TerminalCols: int32(session.TerminalCols),
		},
	}, nil
}

// ListWebSSHSessions lists all active WebSSH sessions for a tenant
func (s *WebSSHServiceServer) ListWebSSHSessions(ctx context.Context, req *pb.ListWebSSHSessionsRequest) (*pb.ListWebSSHSessionsResponse, error) {
	if req.TenantId == "" {
		return nil, errs.InvalidArgumentE("tenant_id is required")
	}

	sessions, err := s.srv.ListWebSSHSessions(req.TenantId)
	if err != nil {
		log.Error().Err(err).Str("tenant_id", req.TenantId).Msg("Failed to list WebSSH sessions")
		return nil, errs.Internalf("failed to list sessions: %v", err)
	}

	pbSessions := make([]*pb.WebSSHSession, 0, len(sessions))
	for _, session := range sessions {
		pbSessions = append(pbSessions, &pb.WebSSHSession{
			Id:           session.ID,
			TenantId:     session.TenantID,
			PeerIp:       session.PeerIP,
			SshPort:      int32(session.Port),
			Username:     session.Username,
			StartedAt:    pb.TimestampFromTime(session.StartedAt),
			LastActive:   pb.TimestampFromTime(session.LastActive),
			Active:       session.Status == "active",
			BytesSent:    session.BytesSent,
			BytesRecv:    session.BytesRecv,
			TerminalRows: int32(session.TerminalRows),
			TerminalCols: int32(session.TerminalCols),
		})
	}

	return &pb.ListWebSSHSessionsResponse{
		Sessions: pbSessions,
	}, nil
}

// DisconnectWebSSHSession disconnects an active WebSSH session
func (s *WebSSHServiceServer) DisconnectWebSSHSession(ctx context.Context, req *pb.DisconnectWebSSHSessionRequest) (*pb.DisconnectWebSSHSessionResponse, error) {
	if req.SessionId == "" {
		return nil, errs.InvalidArgumentE("session_id is required")
	}

	err := s.srv.DisconnectWebSSHSession(req.SessionId)
	if err != nil {
		return &pb.DisconnectWebSSHSessionResponse{
			Success: false,
		}, errs.Internalf("Failed to disconnect session: %v", err)
	}

	return &pb.DisconnectWebSSHSessionResponse{
		Success: true,
	}, nil
}

// grpcInteractiveAuth implements webssh.InteractiveAuthHandler to prompt users over the SSH stream.
type grpcInteractiveAuth struct {
	stream    BidiStream[*pb.SSHStreamMessage, *pb.SSHStreamMessage]
	sessionID string
}

func (h *grpcInteractiveAuth) Prompt(question string, echo bool) (string, error) {
	// Send prompt
	err := h.stream.Send(&pb.SSHStreamMessage{
		SessionId: h.sessionID,
		Payload: &pb.SSHStreamMessage_Output{
			Output: &pb.SSHOutput{
				Data: []byte("\r\n" + question),
			},
		},
	})
	if err != nil {
		return "", err
	}

	// Read answer char by char from the gRPC stream
	var answer string
	for {
		msg, err := h.stream.Recv()
		if err != nil {
			return "", err
		}
		if input, ok := msg.Payload.(*pb.SSHStreamMessage_Input); ok {
			var done bool
			for _, b := range input.Input.GetData() {
				if b == '\r' || b == '\n' {
					done = true
					break
				} else if b == '\x03' { // Ctrl-C
					return "", fmt.Errorf("interrupted")
				} else if b == '\x7f' || b == '\b' {
					if len(answer) > 0 {
						answer = answer[:len(answer)-1]
						if echo {
							_ = h.stream.Send(&pb.SSHStreamMessage{
								SessionId: h.sessionID,
								Payload:   &pb.SSHStreamMessage_Output{Output: &pb.SSHOutput{Data: []byte("\b \b")}},
							})
						}
					}
				} else {
					answer += string(b)
					if echo {
						_ = h.stream.Send(&pb.SSHStreamMessage{
							SessionId: h.sessionID,
							Payload:   &pb.SSHStreamMessage_Output{Output: &pb.SSHOutput{Data: []byte{b}}},
						})
					}
				}
			}
			if done {
				_ = h.stream.Send(&pb.SSHStreamMessage{
					SessionId: h.sessionID,
					Payload:   &pb.SSHStreamMessage_Output{Output: &pb.SSHOutput{Data: []byte("\r\n")}},
				})
				return answer, nil
			}
		}
	}
}

func (h *grpcInteractiveAuth) Banner(message string) error {
	return h.stream.Send(&pb.SSHStreamMessage{
		SessionId: h.sessionID,
		Payload: &pb.SSHStreamMessage_Output{
			Output: &pb.SSHOutput{
				Data: []byte(message + "\r\n"),
			},
		},
	})
}

// StreamSSH handles bidirectional streaming of SSH terminal data via gRPC
// This replaces the previous WebSocket-based approach for SSH streaming
func (s *WebSSHServiceServer) StreamSSH(stream BidiStream[*pb.SSHStreamMessage, *pb.SSHStreamMessage]) error {
	// First message must contain the session_id to identify the session
	firstMsg, err := stream.Recv()
	if err != nil {
		return errs.InvalidArgumentf("failed to receive initial message: %v", err)
	}

	sessionID := firstMsg.SessionId
	if sessionID == "" {
		return errs.InvalidArgumentE("session_id is required in first message")
	}

	log.Debug().
		Str("session_id", sessionID).
		Msg(" SSH gRPC stream connected")

	// Get the session
	session, err := s.srv.GetWebSSHSession(sessionID)
	if err != nil {
		return errs.NotFoundf("session not found: %v", err)
	}

	// We no longer defer ReleaseSSHStream here because GetSSHStream might fail if auth fails,
	// and if it succeeds, ReleaseSSHStream is handled below after the stream is fully established.

	// ctx is scoped to this stream; goroutines call cancel() to signal each other.
	ctx, cancel := context.WithCancel(stream.Context())
	defer cancel()

	// Get the SSH stream from the DirectSSHHandler (with interactive fallback support!)
	sshStream, err := s.srv.GetSSHStream(stream.Context(), sessionID, &grpcInteractiveAuth{stream: stream, sessionID: sessionID})
	if err != nil {
		errorCode := "SSH_CONNECTION_FAILED"
		errCode := errs.Internal
		switch {
		case errors.Is(err, webssh.ErrSSHTunnelUnavailable):
			errorCode = "SSH_TUNNEL_UNAVAILABLE"
			errCode = errs.Unavailable
		case strings.Contains(err.Error(), "unable to authenticate"):
			errorCode = "SSH_AUTH_FAILED"
			errCode = errs.Unauthenticated
		}

		// Send error to client
		sendErr := stream.Send(&pb.SSHStreamMessage{
			SessionId: sessionID,
			Payload: &pb.SSHStreamMessage_Error{
				Error: &pb.SSHError{
					Code:    errorCode,
					Message: err.Error(),
					Fatal:   true,
				},
			},
		})
		if sendErr != nil {
			log.Error().Err(sendErr).Msg("Failed to send SSH error")
		}
		return errs.Wrap(errCode, "failed to get SSH stream", err)
	}
	// Important: we successfully got the stream, which means the handshake/pool dialing succeeded.
	// Now we must ensure we release it when we're finally done.
	defer s.srv.ReleaseSSHStream(sessionID)
	if err := s.srv.ActivateWebSSHSession(session.TenantID, sessionID); err != nil {
		log.Warn().
			Err(err).
			Str("session_id", sessionID).
			Str("tenant_id", session.TenantID).
			Msg("Failed to mark WebSSH session active in Redis")
	}
	defer s.srv.DeactivateWebSSHSession(session.TenantID, sessionID)

	clientIP := requestClientIP(stream.Context())
	userAgent := requestUserAgent(stream.Context(), session.UserAgent)

	// Log SSH activity start with correct client info
	s.srv.LogSSHActivityStart(sessionID, clientIP, userAgent)

	log.Debug().
		Str("session_id", sessionID).
		Str("peer_ip", session.PeerIP).
		Int("ssh_port", session.Port).
		Str("username", session.Username).
		Str("client_ip", clientIP).
		Msg(" SSH gRPC stream established")

	// Replay history if available (for session resumption)
	// We need to access the DirectSSHSession underlying the SSHStream to get history.
	// Since we don't have direct access here easily without casting or changing GetSSHStream return,
	// let's assume we can get it or we already have it from `session` variable (which is DirectSSHSession).
	if len(session.History) > 0 {
		log.Debug().Str("session_id", sessionID).Int("bytes", len(session.History)).Msg("Replaying session history")
		if err := stream.Send(&pb.SSHStreamMessage{
			SessionId: sessionID,
			Payload: &pb.SSHStreamMessage_Output{
				Output: &pb.SSHOutput{
					Data: session.History,
				},
			},
		}); err != nil {
			log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to replay session history")
		}
	}

	// Track bytes for activity logging
	var bytesSent, bytesRecv uint64

	// WaitGroup for goroutines
	var wg sync.WaitGroup
	wg.Add(2)

	// Error channel; capacity 2 so both goroutines can report without blocking.
	errCh := make(chan error, 2)

	// Goroutine 1: gRPC stream → SSH stdin (client keystrokes / resize / ping)
	go func() {
		defer wg.Done()
		defer cancel()

		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				log.Debug().Str("session_id", sessionID).Msg("gRPC stream closed by client")
				return
			}
			if err != nil {
				if ctx.Err() != nil || errs.IsCode(err, errs.Unavailable) {
					return
				}
				select {
				case errCh <- fmt.Errorf("recv error: %w", err):
				default:
				}
				return
			}

			switch payload := msg.Payload.(type) {
			case *pb.SSHStreamMessage_Input:
				if payload.Input != nil {
					switch input := payload.Input.InputType.(type) {
					case *pb.SSHInput_Data:
						n, werr := sshStream.Write(input.Data)
						if werr != nil {
							log.Error().Err(werr).Str("session_id", sessionID).Msg("Failed to write to SSH stdin")
							select {
							case errCh <- fmt.Errorf("ssh write error: %w", werr):
							default:
							}
							return
						}
						atomic.AddUint64(&bytesSent, uint64(n))
					case *pb.SSHInput_Resize:
						if input.Resize != nil {
							if rerr := s.srv.ResizeSSHTerminal(sessionID, int(input.Resize.Rows), int(input.Resize.Cols)); rerr != nil {
								log.Warn().Err(rerr).
									Str("session_id", sessionID).
									Int32("rows", input.Resize.Rows).
									Int32("cols", input.Resize.Cols).
									Msg("Failed to resize terminal")
							}
						}
					}
				}
			case *pb.SSHStreamMessage_Ping:
				if err := stream.Send(&pb.SSHStreamMessage{
					SessionId: sessionID,
					Payload: &pb.SSHStreamMessage_Ping{
						Ping: &pb.SSHPing{Timestamp: time.Now().UnixMilli()},
					},
				}); err != nil {
					log.Warn().Err(err).Str("session_id", sessionID).Msg("Failed to send ping response")
				}
			}
		}
	}()

	// Goroutine 2: SSH stdout → gRPC stream (terminal output to client).
	//
	// Deadlock guard: sshStream.Read blocks on an io.Pipe whose write-end is
	// only closed when the SSH session is torn down.  If goroutine 1 exits
	// (client disconnected) and calls cancel(), this goroutine would stay
	// blocked indefinitely for an idle SSH session.  The watcher below calls
	// AbortRead() which closes the MuxSession and therefore the pipe, causing
	// Read to return io.EOF and allowing wg.Wait() to complete.
	go func() {
		defer wg.Done()
		defer cancel()

		// Watcher: unblock Read when the context is cancelled.
		stopWatcher := make(chan struct{})
		defer close(stopWatcher)
		go func() {
			select {
			case <-ctx.Done():
				sshStream.AbortRead()
			case <-stopWatcher:
			}
		}()

		buf := make([]byte, 32*1024) // 32 KB: large enough to batch SSH output bursts
		for {
			n, err := sshStream.Read(buf)
			if err != nil {
				if err != io.EOF {
					if ctx.Err() == nil { // only log unexpected errors
						log.Error().Err(err).Str("session_id", sessionID).Msg("SSH stdout read error")
						select {
						case errCh <- fmt.Errorf("ssh read error: %w", err):
						default:
						}
					}
				} else {
					log.Debug().Str("session_id", sessionID).Msg("SSH stdout closed (EOF)")
				}
				return
			}
			if n == 0 {
				continue
			}
			atomic.AddUint64(&bytesRecv, uint64(n))
			if err := stream.Send(&pb.SSHStreamMessage{
				SessionId: sessionID,
				Payload: &pb.SSHStreamMessage_Output{
					Output: &pb.SSHOutput{Data: buf[:n]},
				},
			}); err != nil {
				if ctx.Err() == nil {
					log.Error().Err(err).Str("session_id", sessionID).Msg("Failed to send SSH output to client")
					select {
					case errCh <- fmt.Errorf("grpc send error: %w", err):
					default:
					}
				}
				return
			}
		}
	}()

	// Wait for both goroutines to complete
	wg.Wait()

	// Log SSH activity end with byte counts
	finalBytesSent := atomic.LoadUint64(&bytesSent)
	finalBytesRecv := atomic.LoadUint64(&bytesRecv)
	s.srv.LogSSHActivityEnd(sessionID, finalBytesSent, finalBytesRecv)

	// Check for errors
	select {
	case err := <-errCh:
		log.Error().Err(err).Str("session_id", sessionID).Msg("SSH stream error")
		return errs.Internalf("stream error: %v", err)
	default:
	}

	log.Debug().
		Str("session_id", sessionID).
		Uint64("bytes_sent", finalBytesSent).
		Uint64("bytes_recv", finalBytesRecv).
		Msg(" SSH gRPC stream closed")
	return nil
}
