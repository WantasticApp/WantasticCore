package core

import (
	"WantasticCore/internal/errs"
	"context"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/server"
	"WantasticCore/internal/wg/userspace"

)

// PeerServiceServer implements PeerService.
type PeerServiceServer struct {
	UnimplementedPeerService
	srv *server.Server
}

// NewPeerServiceServer creates a new PeerService gRPC server
func NewPeerServiceServer(srv *server.Server) *PeerServiceServer {
	return &PeerServiceServer{
		srv: srv,
	}
}

// Port Scan Control - Delegated directly to Server implementations (which handle gRPC types)

func (s *PeerServiceServer) StartPortScan(ctx context.Context, req *pb.StartPortScanRequest) (*pb.StartPortScanResponse, error) {
	return s.srv.StartPortScan(ctx, req)
}

func (s *PeerServiceServer) StopPortScan(ctx context.Context, req *pb.StopPortScanRequest) (*pb.StopPortScanResponse, error) {
	return s.srv.StopPortScan(ctx, req)
}

func (s *PeerServiceServer) PausePortScan(ctx context.Context, req *pb.PausePortScanRequest) (*pb.PausePortScanResponse, error) {
	return s.srv.PausePortScan(ctx, req)
}

func (s *PeerServiceServer) ResumePortScan(ctx context.Context, req *pb.ResumePortScanRequest) (*pb.ResumePortScanResponse, error) {
	return s.srv.ResumePortScan(ctx, req)
}

func (s *PeerServiceServer) StreamPortScanStatus(req *pb.StreamPortScanStatusRequest, stream ServerStream[*pb.PortScanStatusUpdate]) error {
	return s.srv.StreamPortScanStatus(req, stream)
}

// Peer Management Wrappers

func (s *PeerServiceServer) PingPeer(ctx context.Context, req *pb.PingPeerRequest) (*pb.PingPeerResponse, error) {
	if req.AccountId == "" || req.PeerId == "" {
		return nil, errs.InvalidArgumentE("account_id and peer_id are required")
	}
	res, err := s.srv.PingPeer(req.AccountId, req.PeerId, int(req.Count), int(req.TimeoutMs))
	if err != nil {
		return nil, errs.Internalf("ping failed: %v", err)
	}
	// Convert userspace.PingResult to pb.PingPeerResponse
	pbPings := make([]*pb.PingDetail, len(res.Pings))
	for i, p := range res.Pings {
		pbPings[i] = &pb.PingDetail{
			Sequence:  int32(p.Sequence),
			RttMs:     float32(p.RTTMs),
			Success:   p.Success,
			Error:     p.Error,
			Timestamp: p.Timestamp.UnixMilli(),
		}
	}

	return &pb.PingPeerResponse{
		PeerIp:            res.PeerIP,
		PacketsSent:       int32(res.PacketsSent),
		PacketsReceived:   int32(res.PacketsReceived),
		PacketLossPercent: float32(res.PacketLossPercent),
		MinRttMs:          float32(res.MinRTTMs),
		AvgRttMs:          float32(res.AvgRTTMs),
		MaxRttMs:          float32(res.MaxRTTMs),
		Success:           res.Success,
		Error:             res.Error,
		Pings:             pbPings,
	}, nil
}

// StreamPing sends ICMP pings and streams each result as it arrives.
func (s *PeerServiceServer) StreamPing(req *pb.StreamPingRequest, stream ServerStream[*pb.PingEvent]) error {
	if req.AccountId == "" || req.PeerId == "" {
		return errs.InvalidArgumentE("account_id and peer_id are required")
	}

	device, peerIP, err := s.srv.ResolvePeerDevice(req.AccountId, req.PeerId)
	if err != nil {
		return errs.NotFoundf("peer not found: %v", err)
	}

	result, err := device.StreamICMPPing(peerIP, int(req.Count), int(req.TimeoutMs), func(detail userspace.PingDetail) error {
		return stream.Send(&pb.PingEvent{
			Sequence: int32(detail.Sequence),
			RttMs:    float32(detail.RTTMs),
			Success:  detail.Success,
			Error:    detail.Error,
		})
	})
	if err != nil {
		return errs.Internalf("ping failed: %v", err)
	}

	// Send final summary event
	return stream.Send(&pb.PingEvent{
		IsSummary:         true,
		PeerIp:            result.PeerIP,
		PacketsSent:       int32(result.PacketsSent),
		PacketsReceived:   int32(result.PacketsReceived),
		PacketLossPercent: float32(result.PacketLossPercent),
		MinRttMs:          float32(result.MinRTTMs),
		AvgRttMs:          float32(result.AvgRTTMs),
		MaxRttMs:          float32(result.MaxRTTMs),
	})
}

// UpdatePeerNotes updates the markdown notes for a peer
func (s *PeerServiceServer) UpdatePeerNotes(ctx context.Context, req *pb.UpdatePeerNotesRequest) (*pb.UpdatePeerNotesResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id required")
	}
	if req.PeerId == "" {
		return nil, errs.InvalidArgumentE("peer_id required")
	}

	// Verify peer exists and belongs to account
	peer, err := s.srv.GetPeer(req.AccountId, req.PeerId)
	if err != nil {
		return nil, errs.NotFoundf("peer not found: %v", err)
	}
	if peer.AccountID != req.AccountId {
		return nil, errs.PermissionDeniedE("peer does not belong to this account")
	}

	// Update notes
	if err := s.srv.GetPeerStore().UpdatePeerNotes(req.AccountId, req.PeerId, req.Notes); err != nil {
		return nil, errs.Internalf("failed to update notes: %v", err)
	}

	return &pb.UpdatePeerNotesResponse{
		Peer: &pb.Peer{
			Id:    req.PeerId,
			Notes: req.Notes,
		},
	}, nil
}

// Basic methods left unimplemented for now (frontend uses TenantPeerService)
