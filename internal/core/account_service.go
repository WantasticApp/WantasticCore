package core

import (
	"WantasticCore/internal/errs"
	"context"
	"fmt"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/server"

)

// AccountServiceServer implements AccountService.
type AccountServiceServer struct {
	srv *server.Server
}

// NewAccountServiceServer creates a new AccountService gRPC server
func NewAccountServiceServer(srv *server.Server) *AccountServiceServer {
	return &AccountServiceServer{
		srv: srv,
	}
}

// maxPeersForRequestTier converts a proto AccountTier into a MaxPeers integer.
// Replaces the AccLevel-based mapping (Phase 2: billing/tier removed).
func maxPeersForRequestTier(tier pb.AccountTier) int {
	switch tier {
	case pb.AccountTier_TIER_STANDARD:
		return 29
	case pb.AccountTier_TIER_PREMIUM:
		return 232
	default:
		return 3
	}
}

// CreateAccount creates a new tenant account
func (s *AccountServiceServer) CreateAccount(ctx context.Context, req *pb.CreateAccountRequest) (*pb.CreateAccountResponse, error) {
	if req.Name == "" {
		return nil, errs.InvalidArgumentE("account name is required")
	}

	maxPeers := maxPeersForRequestTier(req.Tier)

	acc, err := s.srv.CreateAccount(req.Name, maxPeers)
	if err != nil {
		return nil, errs.Internalf("failed to create account: %v", err)
	}

	return &pb.CreateAccountResponse{
		Account: &pb.Account{
			Id:        acc.ID,
			Name:      acc.Name,
			Networks:  acc.Networks,
			Tier:      pb.AccountTier_TIER_FREE,
			CreatedAt: pb.TimestampFromTime(acc.CreatedAt),
			UpdatedAt: pb.TimestampFromTime(acc.UpdatedAt),
		},
	}, nil
}

// GetAccount retrieves account details by ID
func (s *AccountServiceServer) GetAccount(ctx context.Context, req *pb.GetAccountRequest) (*pb.GetAccountResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id is required")
	}

	acc2, err := s.srv.GetAccount(req.AccountId)
	if err != nil {
		return nil, errs.NotFoundf("account not found: %v", err)
	}

	return &pb.GetAccountResponse{
		Account: &pb.Account{
			Id:        acc2.ID,
			Name:      acc2.Name,
			Networks:  acc2.Networks,
			Tier:      pb.AccountTier_TIER_FREE,
			CreatedAt: pb.TimestampFromTime(acc2.CreatedAt),
			UpdatedAt: pb.TimestampFromTime(acc2.UpdatedAt),
		},
	}, nil
}

// ListAccounts lists all accounts with pagination
func (s *AccountServiceServer) ListAccounts(ctx context.Context, req *pb.ListAccountsRequest) (*pb.ListAccountsResponse, error) {
	accounts, err := s.srv.ListAccounts()
	if err != nil {
		return nil, errs.Internalf("failed to list accounts: %v", err)
	}

	pbAccounts := make([]*pb.Account, len(accounts))
	for i, acc := range accounts {
		pbAccounts[i] = &pb.Account{
			Id:        acc.ID,
			Name:      acc.Name,
			Networks:  acc.Networks,
			Tier:      pb.AccountTier_TIER_FREE,
			CreatedAt: pb.TimestampFromTime(acc.CreatedAt),
			UpdatedAt: pb.TimestampFromTime(acc.UpdatedAt),
		}
	}

	return &pb.ListAccountsResponse{
		Accounts:   pbAccounts,
		TotalCount: int32(len(accounts)),
	}, nil
}

// DeleteAccount deletes an account and all its resources
func (s *AccountServiceServer) DeleteAccount(ctx context.Context, req *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id is required")
	}

	if err := s.srv.DeleteAccount(req.AccountId); err != nil {
		return nil, errs.Internalf("failed to delete account: %v", err)
	}

	return &pb.DeleteAccountResponse{Success: true}, nil
}

// UpdateAccountQuotas sets an admin-controlled peer-count cap via the
// max_peers_per_network field. Pass 0 to revert to the default cap.
func (s *AccountServiceServer) UpdateAccountQuotas(ctx context.Context, req *pb.UpdateAccountQuotasRequest) (*pb.UpdateAccountQuotasResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id is required")
	}
	if err := s.srv.SetPeerLimitOverride(req.AccountId, int(req.MaxPeersPerNetwork)); err != nil {
		return nil, errs.Internalf("set peer limit: %v", err)
	}
	acc, err := s.srv.GetAccount(req.AccountId)
	if err != nil {
		return nil, errs.Internalf("get account: %v", err)
	}
	return &pb.UpdateAccountQuotasResponse{
		Account: &pb.Account{
			Id:        acc.ID,
			Name:      acc.Name,
			Networks:  acc.Networks,
			Tier:      pb.AccountTier_TIER_FREE,
			CreatedAt: pb.TimestampFromTime(acc.CreatedAt),
			UpdatedAt: pb.TimestampFromTime(acc.UpdatedAt),
		},
	}, nil
}

// UpdateAccountTier — billing removed (Phase 2). The proto method is kept to
// satisfy the gRPC server interface, but it now simply updates the MaxPeers
// cap derived from the requested tier rather than performing any billing or
// IPAM block reshuffling.
func (s *AccountServiceServer) UpdateAccountTier(ctx context.Context, req *pb.UpdateAccountTierRequest) (*pb.UpdateAccountTierResponse, error) {
	if req.AccountId == "" {
		return nil, errs.InvalidArgumentE("account_id is required")
	}

	maxPeers := maxPeersForRequestTier(pb.AccountTier(req.Tier))
	acc, err := s.srv.SetAccountMaxPeers(req.AccountId, maxPeers)
	if err != nil {
		return nil, errs.Internalf("failed to update account: %v", err)
	}

	message := fmt.Sprintf("Account max-peers updated to %d", acc.MaxPeers)
	return &pb.UpdateAccountTierResponse{
		Account: &pb.Account{
			Id:        acc.ID,
			Name:      acc.Name,
			Networks:  acc.Networks,
			Tier:      pb.AccountTier_TIER_FREE,
			CreatedAt: pb.TimestampFromTime(acc.CreatedAt),
			UpdatedAt: pb.TimestampFromTime(acc.UpdatedAt),
		},
		Message: message,
	}, nil
}
