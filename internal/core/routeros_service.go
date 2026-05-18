package core

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/account"
	"WantasticCore/internal/crypto"
	rosapi "WantasticCore/internal/routerosapi"
	"WantasticCore/internal/server"
	"WantasticCore/internal/wg/userspace/wireguard-go/wgctrl/wgtypes"

	"github.com/rs/zerolog/log"
)

type RouterOSService struct {
	server  *server.Server
	manager *rosapi.Manager
}

type routerOSResolvedAccess struct {
	accountID  string
	peer       *server.PeerMetadata
	session    *server.WinboxSession
	capability *pb.RouterOSCapability
	dialer     rosapi.DialContextFunc
	params     rosapi.ConnectParams
}

const (
	routerOSCredentialSourceNone   = "none"
	routerOSCredentialSourcePeer   = "peer"
	routerOSCredentialSourceWinbox = "winbox"
)

func NewRouterOSService(srv *server.Server) *RouterOSService {
	return &RouterOSService{
		server:  srv,
		manager: rosapi.NewManager(),
	}
}

type routerOSDashboardStreamRuntime struct {
	service   *RouterOSService
	stream    BidiStream[*pb.StreamRouterOSDashboardRequest, *pb.StreamRouterOSDashboardEvent]
	peerID    string
	accountID string
	access    *routerOSResolvedAccess
	api       *rosapi.Session
	apiKey    string
	sendMu    sync.Mutex
	// Reconnect bookkeeping. reconnectAttempts is incremented on every
	// failed open / probe failure and reset on success — drives the
	// exponential-backoff schedule in reconnectBackoff(). reconnectJitter
	// is a small running counter used to spread simultaneous reconnects.
	reconnectAttempts int
	reconnectJitter   int
}

func (r *routerOSDashboardStreamRuntime) close() {
	if r.api != nil {
		_ = r.api.Close()
		r.api = nil
	}
	r.apiKey = ""
}

func (r *routerOSDashboardStreamRuntime) updateAccess(access *routerOSResolvedAccess) {
	if access == nil {
		return
	}
	r.access = access
	if strings.TrimSpace(access.accountID) != "" {
		r.accountID = access.accountID
	}
	if access.peer != nil && strings.TrimSpace(access.peer.ID) != "" {
		r.peerID = access.peer.ID
	}
}

func (r *routerOSDashboardStreamRuntime) closeSessionIfKeyChanged(nextKey string) {
	if r.api == nil || r.apiKey == nextKey {
		return
	}
	_ = r.api.Close()
	r.api = nil
	r.apiKey = ""
}

// reconnectBackoff is the delay applied before a forced reconnect, so a
// router that just rebooted (or is mid-handshake) is not hammered by an
// instant retry. Uses tiny jitter to avoid thundering-herd when several
// streams reconnect simultaneously.
func (r *routerOSDashboardStreamRuntime) reconnectBackoff() time.Duration {
	r.reconnectAttempts++
	if r.reconnectAttempts > 5 {
		r.reconnectAttempts = 5
	}
	// 200ms, 400ms, 800ms, 1.6s, 3s — capped.
	base := time.Duration(200*(1<<(r.reconnectAttempts-1))) * time.Millisecond
	if base > 3*time.Second {
		base = 3 * time.Second
	}
	// ±25% jitter, deterministic per stream via a small running counter.
	jitter := base / 4
	r.reconnectJitter = (r.reconnectJitter + 1) % 7
	offset := time.Duration(int64(jitter) * int64(r.reconnectJitter) / 7)
	return base + offset - jitter/2
}

func (r *routerOSDashboardStreamRuntime) noteReconnectSuccess() {
	r.reconnectAttempts = 0
}

func (r *routerOSDashboardStreamRuntime) ensureConnected(ctx context.Context, forceReconnect bool) error {
	if !forceReconnect && r.api != nil {
		return nil
	}
	if forceReconnect {
		// Backoff before re-opening so a rebooting router has time to come
		// back up. Skip on the very first attempt (no prior failure yet).
		if r.reconnectAttempts > 0 {
			delay := r.reconnectBackoff()
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		r.close()
	}

	if !forceReconnect && r.api == nil && r.access != nil && r.access.dialer != nil && r.access.params.Address != "" {
		api, openErr := r.service.manager.OpenSession(ctx, r.access.dialer, r.access.params)
		if openErr == nil {
			r.api = api
			r.apiKey = fmt.Sprintf(
				"%s|%s|%s|%t",
				r.access.params.Address,
				r.access.params.Username,
				r.access.params.Password,
				r.access.params.UseTLS,
			)
			r.noteReconnectSuccess()
			return nil
		}
	}

	access, err := ensureRouterOSAccess(ctx, r.service.server, r.service.manager, r.accountID, r.peerID)
	r.updateAccess(access)
	if err != nil {
		r.close()
		r.reconnectAttempts++
		return err
	}

	nextKey := fmt.Sprintf(
		"%s|%s|%s|%t",
		access.params.Address,
		access.params.Username,
		access.params.Password,
		access.params.UseTLS,
	)
	if !forceReconnect {
		r.closeSessionIfKeyChanged(nextKey)
	}

	if r.api != nil {
		r.noteReconnectSuccess()
		return nil
	}

	api, openErr := r.service.manager.OpenSession(ctx, access.dialer, access.params)
	if openErr != nil {
		r.reconnectAttempts++
		return openErr
	}
	r.api = api
	r.apiKey = nextKey
	r.noteReconnectSuccess()
	return nil
}

func (r *routerOSDashboardStreamRuntime) sendState(err error, connected bool, overview *rosapi.Overview) error {
	capability := &pb.RouterOSCapability{}
	if r.access != nil && r.access.capability != nil {
		capability = r.access.capability
	}
	state := &pb.RouterOSDashboardState{
		PeerId:         r.peerID,
		AccountId:      r.accountID,
		Success:        err == nil,
		Error:          routerOSDisplayError(err),
		Connected:      connected,
		AccessRequired: routerOSStreamAccessRequired(r.access, err),
		Capability:     capability,
	}
	if overview != nil {
		state.Identity = &pb.RouterOSIdentity{
			Identity:     overview.Identity.Identity,
			Version:      overview.Identity.Version,
			BoardName:    overview.Identity.BoardName,
			Model:        overview.Identity.Model,
			Platform:     overview.Identity.Platform,
			Architecture: overview.Identity.Architecture,
			Cpu:          overview.Identity.CPU,
		}
		state.SystemResource = overview.SystemResource
		state.Routerboard = overview.Routerboard
	}
	return r.sendEvent(&pb.StreamRouterOSDashboardEvent{
		Payload: &pb.StreamRouterOSDashboardEvent_State{State: state},
	})
}

func (r *routerOSDashboardStreamRuntime) sendResource(resource pb.RouterOSResource, records []rosapi.Record, err error) error {
	out := make([]*pb.RouterOSRecord, 0, len(records))
	for _, record := range records {
		out = append(out, &pb.RouterOSRecord{
			Id:     record.ID,
			Fields: record.Fields,
		})
	}
	return r.sendEvent(&pb.StreamRouterOSDashboardEvent{
		Payload: &pb.StreamRouterOSDashboardEvent_Resource{
			Resource: &pb.RouterOSResourceSnapshot{
				Resource: resource,
				Success:  err == nil,
				Error:    routerOSDisplayError(err),
				Records:  out,
			},
		},
	})
}

func (r *routerOSDashboardStreamRuntime) sendNotice(action string, resource pb.RouterOSResource, id string, err error) error {
	return r.sendEvent(&pb.StreamRouterOSDashboardEvent{
		Payload: &pb.StreamRouterOSDashboardEvent_Notice{
			Notice: &pb.RouterOSMutationNotice{
				Action:   action,
				Resource: resource,
				Success:  err == nil,
				Error:    routerOSDisplayError(err),
				Id:       id,
			},
		},
	})
}

func (r *routerOSDashboardStreamRuntime) sendEvent(event *pb.StreamRouterOSDashboardEvent) error {
	r.sendMu.Lock()
	defer r.sendMu.Unlock()
	return r.stream.Send(event)
}

func (r *routerOSDashboardStreamRuntime) refreshOverview(ctx context.Context, forceReconnect bool) error {
	overview, err := r.withOverview(ctx, forceReconnect)
	if err != nil {
		return r.sendState(err, false, nil)
	}
	return r.sendState(nil, true, overview)
}

func (r *routerOSDashboardStreamRuntime) loadResource(ctx context.Context, resource pb.RouterOSResource, forceReconnect bool) error {
	records, err := r.withRecords(ctx, mapRouterOSResource(resource), forceReconnect)
	if err != nil {
		return r.sendResource(resource, nil, err)
	}
	return r.sendResource(resource, records, nil)
}

func (r *routerOSDashboardStreamRuntime) withMutation(ctx context.Context, forceReconnect bool, fn func(api *rosapi.Session) error) error {
	if err := r.ensureConnected(ctx, forceReconnect); err != nil {
		return err
	}

	err := fn(r.api)
	if err == nil {
		return nil
	}
	if !routerOSShouldReconnectSession(err) {
		return err
	}

	r.close()
	if err := r.ensureConnected(ctx, true); err != nil {
		return err
	}
	return fn(r.api)
}

func (r *routerOSDashboardStreamRuntime) withOverview(ctx context.Context, forceReconnect bool) (*rosapi.Overview, error) {
	if err := r.ensureConnected(ctx, forceReconnect); err != nil {
		return nil, err
	}

	overview, err := r.api.GetOverview(ctx)
	if err == nil {
		return overview, nil
	}
	if !routerOSShouldReconnectSession(err) {
		return nil, err
	}

	r.close()
	if err := r.ensureConnected(ctx, true); err != nil {
		return nil, err
	}
	return r.api.GetOverview(ctx)
}

func (r *routerOSDashboardStreamRuntime) withRecords(ctx context.Context, resource rosapi.Resource, forceReconnect bool) ([]rosapi.Record, error) {
	if err := r.ensureConnected(ctx, forceReconnect); err != nil {
		return nil, err
	}

	records, err := r.api.ListRecords(ctx, resource)
	if err == nil {
		return records, nil
	}
	if !routerOSShouldReconnectSession(err) {
		return nil, err
	}

	r.close()
	if err := r.ensureConnected(ctx, true); err != nil {
		return nil, err
	}
	return r.api.ListRecords(ctx, resource)
}

func (s *RouterOSService) StreamDashboard(stream BidiStream[*pb.StreamRouterOSDashboardRequest, *pb.StreamRouterOSDashboardEvent]) error {
	runtime := &routerOSDashboardStreamRuntime{
		service: s,
		stream:  stream,
	}
	defer runtime.close()

	// Heartbeat — runs every 18s while the stream is live.
	//
	// Previously this only emitted a state-only event reflecting whether
	// `runtime.api != nil`. That is a poor liveness signal: a TCP RST or
	// silent socket death leaves `api` non-nil but unusable until the
	// next user action — up to the live-refresh tick (22s) plus retry —
	// before the UI learns the connection is gone.
	//
	// Now we actively probe: when there is a live session, fetch a
	// fresh /system/identity. On success we emit a normal connected
	// state; on failure we close the session, mark the runtime as
	// disconnected, and let the next user action drive a backoff
	// reconnect via withMutation/withOverview/withRecords.
	heartbeatStop := make(chan struct{})
	defer close(heartbeatStop)
	go func() {
		ticker := time.NewTicker(18 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if strings.TrimSpace(runtime.peerID) == "" {
					continue
				}
				if runtime.api == nil {
					_ = runtime.sendState(nil, false, nil)
					continue
				}
				probeCtx, cancel := context.WithTimeout(stream.Context(), 6*time.Second)
				_, probeErr := runtime.api.GetOverview(probeCtx)
				cancel()
				if probeErr == nil {
					_ = runtime.sendState(nil, true, nil)
					continue
				}
				// Probe failed — surface it to the UI as a disconnect
				// signal so the operator sees the change immediately
				// rather than discovering it on next click.
				log.Debug().
					Str("peer_id", runtime.peerID).
					Err(probeErr).
					Msg("routeros heartbeat probe failed; marking session disconnected")
				runtime.close()
				_ = runtime.sendState(probeErr, false, nil)
			case <-heartbeatStop:
				return
			case <-stream.Context().Done():
				return
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		switch payload := req.Payload.(type) {
		case *pb.StreamRouterOSDashboardRequest_Open:
			runtime.peerID = strings.TrimSpace(payload.Open.PeerId)
			runtime.accountID = strings.TrimSpace(payload.Open.AccountId)
			if runtime.peerID == "" {
				if sendErr := runtime.sendState(errors.New("peer not found"), false, nil); sendErr != nil {
					return sendErr
				}
				continue
			}
			if err := runtime.refreshOverview(stream.Context(), true); err != nil {
				return err
			}
			if payload.Open.Resource != pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN {
				if err := runtime.loadResource(stream.Context(), payload.Open.Resource, false); err != nil {
					return err
				}
			}

		case *pb.StreamRouterOSDashboardRequest_LoadResource:
			if runtime.peerID == "" {
				if err := runtime.sendResource(payload.LoadResource.Resource, nil, errors.New("peer not found")); err != nil {
					return err
				}
				continue
			}
			if err := runtime.loadResource(stream.Context(), payload.LoadResource.Resource, payload.LoadResource.ForceReload); err != nil {
				return err
			}

		case *pb.StreamRouterOSDashboardRequest_Refresh:
			if runtime.peerID == "" {
				if err := runtime.sendState(errors.New("peer not found"), false, nil); err != nil {
					return err
				}
				continue
			}
			if payload.Refresh.Overview || len(payload.Refresh.Resources) == 0 {
				if err := runtime.refreshOverview(stream.Context(), false); err != nil {
					return err
				}
			}
			for _, resource := range payload.Refresh.Resources {
				if resource == pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN {
					continue
				}
				if err := runtime.loadResource(stream.Context(), resource, false); err != nil {
					return err
				}
			}

		case *pb.StreamRouterOSDashboardRequest_ConfigureAccess:
			if runtime.peerID == "" {
				if err := runtime.sendNotice("configure_access", pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN, "", errors.New("peer not found")); err != nil {
					return err
				}
				continue
			}
			req := payload.ConfigureAccess
			req.PeerId = runtime.peerID
			req.AccountId = runtime.accountID
			resp, callErr := s.ConfigureAccess(stream.Context(), req)
			if callErr != nil {
				if err := runtime.sendNotice("configure_access", pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN, "", callErr); err != nil {
					return err
				}
				continue
			}
			if runtime.access == nil {
				runtime.access = &routerOSResolvedAccess{accountID: runtime.accountID}
			}
			runtime.access.capability = resp.Capability
			if err := runtime.sendNotice("configure_access", pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN, "", errorFromString(resp.Error, resp.Success)); err != nil {
				return err
			}
			if err := runtime.refreshOverview(stream.Context(), true); err != nil {
				return err
			}

		case *pb.StreamRouterOSDashboardRequest_AddResource:
			req := payload.AddResource
			req.PeerId = runtime.peerID
			req.AccountId = runtime.accountID
			err := runtime.withMutation(stream.Context(), false, func(api *rosapi.Session) error {
				return api.AddRecord(stream.Context(), mapRouterOSResource(req.Resource), req.Fields)
			})
			if sendErr := runtime.sendNotice("add", req.Resource, "", err); sendErr != nil {
				return sendErr
			}
			if err == nil {
				if sendErr := runtime.loadResource(stream.Context(), req.Resource, false); sendErr != nil {
					return sendErr
				}
			}

		case *pb.StreamRouterOSDashboardRequest_UpdateResource:
			req := payload.UpdateResource
			req.PeerId = runtime.peerID
			req.AccountId = runtime.accountID
			err := runtime.withMutation(stream.Context(), false, func(api *rosapi.Session) error {
				if req.Id == "" {
					return errors.New("routeros id is required")
				}
				return api.UpdateRecord(stream.Context(), mapRouterOSResource(req.Resource), req.Id, req.Fields)
			})
			if sendErr := runtime.sendNotice("update", req.Resource, req.Id, err); sendErr != nil {
				return sendErr
			}
			if err == nil {
				if sendErr := runtime.loadResource(stream.Context(), req.Resource, false); sendErr != nil {
					return sendErr
				}
			}

		case *pb.StreamRouterOSDashboardRequest_DeleteResource:
			req := payload.DeleteResource
			req.PeerId = runtime.peerID
			req.AccountId = runtime.accountID
			err := runtime.withMutation(stream.Context(), false, func(api *rosapi.Session) error {
				if req.Id == "" {
					return errors.New("routeros id is required")
				}
				return api.DeleteRecord(stream.Context(), mapRouterOSResource(req.Resource), req.Id)
			})
			if sendErr := runtime.sendNotice("delete", req.Resource, req.Id, err); sendErr != nil {
				return sendErr
			}
			if err == nil {
				if sendErr := runtime.loadResource(stream.Context(), req.Resource, false); sendErr != nil {
					return sendErr
				}
			}

		default:
			if err := runtime.sendNotice("unknown", pb.RouterOSResource_ROUTEROS_RESOURCE_UNKNOWN, "", errors.New("unsupported dashboard command")); err != nil {
				return err
			}
		}
	}
}

func (s *RouterOSService) GetOverview(ctx context.Context, req *pb.GetRouterOSOverviewRequest) (*pb.GetRouterOSOverviewResponse, error) {
	access, err := ensureRouterOSAccess(ctx, s.server, s.manager, req.AccountId, req.PeerId)
	if err != nil {
		return &pb.GetRouterOSOverviewResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	overview, err := s.manager.GetOverview(ctx, access.dialer, access.params)
	if err != nil {
		return &pb.GetRouterOSOverviewResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	return &pb.GetRouterOSOverviewResponse{
		Success:    true,
		Capability: access.capability,
		Identity: &pb.RouterOSIdentity{
			Identity:     overview.Identity.Identity,
			Version:      overview.Identity.Version,
			BoardName:    overview.Identity.BoardName,
			Model:        overview.Identity.Model,
			Platform:     overview.Identity.Platform,
			Architecture: overview.Identity.Architecture,
			Cpu:          overview.Identity.CPU,
		},
		SystemResource: overview.SystemResource,
		Routerboard:    overview.Routerboard,
	}, nil
}

func (s *RouterOSService) ConfigureAccess(ctx context.Context, req *pb.ConfigureRouterOSAccessRequest) (*pb.ConfigureRouterOSAccessResponse, error) {
	peer, accountID, capability, session, err := resolveRouterOSPeerContext(s.server, req.PeerId)
	if err != nil {
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      routerOSUserError(err),
			Capability: capability,
		}, nil
	}

	if !capability.Candidate {
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      "RouterOS access is only available for MikroTik devices.",
			Capability: capability,
		}, nil
	}

	device, err := s.server.GetTenantDevice(accountID)
	if err != nil {
		userErr := routerOSUserError(fmt.Errorf("tenant device is not available: %w", err))
		if persistErr := persistRouterOSProbeFailure(s.server, accountID, peer, nil, userErr); persistErr != nil {
			log.Warn().Err(persistErr).Str("peer_id", peer.ID).Msg("Failed to persist RouterOS probe failure")
		}
		updatedCapability := routerOSCapabilityForPeer(s.server, accountID, peer, bestRouterOSSession(peer.WinboxSessions))
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      userErr,
			Capability: updatedCapability,
		}, nil
	}

	dialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		return device.Net.DialContext(ctx, network, address)
	}
	host := routerOSProbeHost(peer, session)

	var (
		username string
		password string
		source   string
	)

	if req.UseSavedWinbox {
		if session == nil {
			return &pb.ConfigureRouterOSAccessResponse{
				Success:    false,
				Error:      "No saved Winbox account is available for this device.",
				Capability: capability,
			}, nil
		}
		username, password, err = decryptWinboxCredentials(s.server, accountID, session)
		source = routerOSCredentialSourceWinbox
	} else {
		username = strings.TrimSpace(req.Username)
		password = req.Password
		source = routerOSCredentialSourcePeer
		if username == "" || password == "" {
			return &pb.ConfigureRouterOSAccessResponse{
				Success:    false,
				Error:      "RouterOS username and password are required.",
				Capability: capability,
			}, nil
		}
	}

	if err != nil {
		userErr := routerOSUserError(fmt.Errorf("could not decrypt saved device credentials: %w", err))
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      userErr,
			Capability: capability,
		}, nil
	}

	params, probeErr := probeRouterOSWithCredentials(ctx, s.manager, dialer, host, username, password, int(req.Port), req.UseTls, session)
	if probeErr != nil {
		userErr := routerOSUserError(probeErr)
		if persistErr := persistRouterOSProbeFailure(s.server, accountID, peer, session, userErr); persistErr != nil {
			log.Warn().Err(persistErr).Str("peer_id", peer.ID).Msg("Failed to persist RouterOS credential failure")
		}
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      userErr,
			Capability: routerOSCapabilityForPeer(s.server, accountID, peer, bestRouterOSSession(peer.WinboxSessions)),
		}, nil
	}

	if err := persistRouterOSAccessSuccess(s.server, accountID, peer, session, username, password, source, params); err != nil {
		log.Warn().Err(err).Str("peer_id", peer.ID).Msg("Failed to persist RouterOS credential success")
		return &pb.ConfigureRouterOSAccessResponse{
			Success:    false,
			Error:      "RouterOS credentials were verified but could not be saved. Please try again.",
			Capability: routerOSCapabilityForPeer(s.server, accountID, peer, bestRouterOSSession(peer.WinboxSessions)),
		}, nil
	}

	return &pb.ConfigureRouterOSAccessResponse{
		Success:    true,
		Capability: routerOSCapabilityForPeer(s.server, accountID, peer, bestRouterOSSession(peer.WinboxSessions)),
	}, nil
}

func (s *RouterOSService) ListResource(ctx context.Context, req *pb.ListRouterOSResourceRequest) (*pb.ListRouterOSResourceResponse, error) {
	access, err := ensureRouterOSAccess(ctx, s.server, s.manager, req.AccountId, req.PeerId)
	if err != nil {
		return &pb.ListRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	records, err := s.manager.ListRecords(ctx, access.dialer, access.params, mapRouterOSResource(req.Resource))
	if err != nil {
		return &pb.ListRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	out := make([]*pb.RouterOSRecord, 0, len(records))
	for _, record := range records {
		out = append(out, &pb.RouterOSRecord{
			Id:     record.ID,
			Fields: record.Fields,
		})
	}

	return &pb.ListRouterOSResourceResponse{
		Success:    true,
		Capability: access.capability,
		Records:    out,
	}, nil
}

func (s *RouterOSService) AddResource(ctx context.Context, req *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error) {
	access, err := ensureRouterOSAccess(ctx, s.server, s.manager, req.AccountId, req.PeerId)
	if err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	if err := s.manager.AddRecord(ctx, access.dialer, access.params, mapRouterOSResource(req.Resource), req.Fields); err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	return &pb.MutateRouterOSResourceResponse{Success: true, Capability: access.capability}, nil
}

func (s *RouterOSService) UpdateResource(ctx context.Context, req *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error) {
	access, err := ensureRouterOSAccess(ctx, s.server, s.manager, req.AccountId, req.PeerId)
	if err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	if req.Id == "" {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      "routeros id is required",
			Capability: access.capability,
		}, nil
	}

	if err := s.manager.UpdateRecord(ctx, access.dialer, access.params, mapRouterOSResource(req.Resource), req.Id, req.Fields); err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	return &pb.MutateRouterOSResourceResponse{Success: true, Capability: access.capability}, nil
}

func (s *RouterOSService) DeleteResource(ctx context.Context, req *pb.DeleteRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error) {
	access, err := ensureRouterOSAccess(ctx, s.server, s.manager, req.AccountId, req.PeerId)
	if err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	if req.Id == "" {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      "routeros id is required",
			Capability: access.capability,
		}, nil
	}

	if err := s.manager.DeleteRecord(ctx, access.dialer, access.params, mapRouterOSResource(req.Resource), req.Id); err != nil {
		return &pb.MutateRouterOSResourceResponse{
			Success:    false,
			Error:      routerOSDisplayError(err),
			Capability: access.capability,
		}, nil
	}

	return &pb.MutateRouterOSResourceResponse{Success: true, Capability: access.capability}, nil
}

func mapRouterOSResource(resource pb.RouterOSResource) rosapi.Resource {
	switch resource {
	case pb.RouterOSResource_ROUTEROS_RESOURCE_IP_ADDRESSES:
		return rosapi.ResourceIPAddresses
	case pb.RouterOSResource_ROUTEROS_RESOURCE_ROUTES:
		return rosapi.ResourceRoutes
	case pb.RouterOSResource_ROUTEROS_RESOURCE_FIREWALL:
		return rosapi.ResourceFirewall
	case pb.RouterOSResource_ROUTEROS_RESOURCE_PACKAGES:
		return rosapi.ResourcePackages
	case pb.RouterOSResource_ROUTEROS_RESOURCE_FILES:
		return rosapi.ResourceFiles
	case pb.RouterOSResource_ROUTEROS_RESOURCE_WIRELESS:
		return rosapi.ResourceWireless
	case pb.RouterOSResource_ROUTEROS_RESOURCE_TR069_CLIENT:
		return rosapi.ResourceTR069Client
	case pb.RouterOSResource_ROUTEROS_RESOURCE_BRIDGE:
		return rosapi.ResourceBridge
	default:
		return rosapi.ResourceUnknown
	}
}

func ensureRouterOSAccess(ctx context.Context, backend ServerBackend, manager *rosapi.Manager, accountID, peerID string) (*routerOSResolvedAccess, error) {
	peer, resolvedAccountID, capability, session, err := resolveRouterOSPeerContext(backend, peerID)
	if err != nil {
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: capability,
		}, errors.New(routerOSUserError(err))
	}

	if !capability.Candidate {
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: capability,
		}, errors.New("RouterOS access is only available for MikroTik devices")
	}

	device, err := backend.GetTenantDevice(resolvedAccountID)
	if err != nil {
		userErr := routerOSUserError(fmt.Errorf("tenant device is not available: %w", err))
		_ = persistRouterOSProbeFailure(backend, resolvedAccountID, peer, session, userErr)
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
		}, errors.New(userErr)
	}

	dialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		return device.Net.DialContext(ctx, network, address)
	}
	host := routerOSProbeHost(peer, session)

	if peerHasRouterOSCredentials(peer) {
		username, password, derr := decryptPeerRouterOSCredentials(backend, resolvedAccountID, peer)
		if derr == nil {
			params, probeErr := probeRouterOSWithCredentials(ctx, manager, dialer, host, username, password, peer.RouterOSAPIPort, peer.RouterOSAPITLS, session)
			if probeErr == nil {
				if saveErr := persistRouterOSAccessSuccess(backend, resolvedAccountID, peer, session, username, password, routerOSCredentialSourcePeer, params); saveErr != nil {
					return &routerOSResolvedAccess{
						accountID:  resolvedAccountID,
						peer:       peer,
						session:    session,
						capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
					}, errors.New("RouterOS credentials were verified but could not be saved. Please try again.")
				}
				return &routerOSResolvedAccess{
					accountID:  resolvedAccountID,
					peer:       peer,
					session:    session,
					capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
					dialer:     dialer,
					params:     params,
				}, nil
			}
			if session == nil {
				userErr := routerOSUserError(probeErr)
				_ = persistRouterOSProbeFailure(backend, resolvedAccountID, peer, nil, userErr)
				return &routerOSResolvedAccess{
					accountID:  resolvedAccountID,
					peer:       peer,
					session:    session,
					capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
				}, errors.New(userErr)
			}
		}
	}

	if session == nil {
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			capability: capability,
		}, errors.New("RouterOS credentials are required before this dashboard can connect.")
	}

	username, password, err := decryptWinboxCredentials(backend, resolvedAccountID, session)
	if err != nil {
		userErr := routerOSUserError(fmt.Errorf("could not decrypt saved device credentials: %w", err))
		_ = persistRouterOSProbeFailure(backend, resolvedAccountID, peer, session, userErr)
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
		}, errors.New(userErr)
	}

	params, probeErr := probeRouterOSWithCredentials(ctx, manager, dialer, host, username, password, 0, false, session)
	if probeErr != nil {
		userErr := routerOSUserError(probeErr)
		_ = persistRouterOSProbeFailure(backend, resolvedAccountID, peer, session, userErr)
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
		}, errors.New(userErr)
	}

	if err := persistRouterOSAccessSuccess(backend, resolvedAccountID, peer, session, username, password, routerOSCredentialSourceWinbox, params); err != nil {
		return &routerOSResolvedAccess{
			accountID:  resolvedAccountID,
			peer:       peer,
			session:    session,
			capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
		}, errors.New("RouterOS credentials were verified but could not be saved. Please try again.")
	}

	return &routerOSResolvedAccess{
		accountID:  resolvedAccountID,
		peer:       peer,
		session:    session,
		capability: routerOSCapabilityForPeer(backend, resolvedAccountID, peer, session),
		dialer:     dialer,
		params:     params,
	}, nil
}

func probeRouterOSSessionWithCredentials(ctx context.Context, backend ServerBackend, manager *rosapi.Manager, accountID string, peer *server.PeerMetadata, session *server.WinboxSession, username, password string) {
	device, err := backend.GetTenantDevice(accountID)
	if err != nil {
		session.RouterOSAPIVerified = false
		session.RouterOSAPIError = routerOSUserError(fmt.Errorf("tenant device is not available: %w", err))
		session.RouterOSAPILastValidated = time.Now().UTC()
		return
	}

	host := routerOSProbeHost(peer, session)
	dialer := func(ctx context.Context, network, address string) (net.Conn, error) {
		return device.Net.DialContext(ctx, network, address)
	}

	params, probeErr := probeRouterOSWithCredentials(ctx, manager, dialer, host, username, password, 0, false, session)
	if probeErr == nil {
		now := time.Now().UTC()
		session.RouterOSAPIVerified = true
		session.RouterOSAPILastValidated = now
		session.RouterOSAPIError = ""
		session.RouterOSAPIPort = routerOSPortFromAddress(params.Address)
		session.RouterOSAPITLS = params.UseTLS
		session.CredentialsValid = true
		session.LastValidated = now
		session.ValidationError = ""
		return
	}

	session.RouterOSAPIVerified = false
	session.RouterOSAPILastValidated = time.Now().UTC()
	session.RouterOSAPIError = routerOSUserError(probeErr)
}

func decryptWinboxCredentials(backend ServerBackend, accountID string, session *server.WinboxSession) (string, string, error) {
	cipher, err := credentialCipherForAccount(backend, accountID)
	if err != nil {
		return "", "", err
	}
	usernameBytes, err := cipher.Decrypt(session.EncryptedUsername)
	if err != nil {
		return "", "", err
	}
	passwordBytes, err := cipher.Decrypt(session.EncryptedPassword)
	if err != nil {
		return "", "", err
	}
	return string(usernameBytes), string(passwordBytes), nil
}

func credentialCipherForAccount(backend interface {
	GetAccount(accountID string) (*account.Account, error)
}, accountID string) (*crypto.CredentialCipher, error) {
	acc, err := backend.GetAccount(accountID)
	if err != nil {
		return nil, fmt.Errorf("account not found: %w", err)
	}
	privateKey, err := wgtypes.ParseKey(acc.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	return crypto.NewCredentialCipher(privateKey[:])
}

func decryptPeerRouterOSCredentials(backend ServerBackend, accountID string, peer *server.PeerMetadata) (string, string, error) {
	cipher, err := credentialCipherForAccount(backend, accountID)
	if err != nil {
		return "", "", err
	}
	usernameBytes, err := cipher.Decrypt(peer.EncryptedRouterOSUsername)
	if err != nil {
		return "", "", err
	}
	passwordBytes, err := cipher.Decrypt(peer.EncryptedRouterOSPassword)
	if err != nil {
		return "", "", err
	}
	return string(usernameBytes), string(passwordBytes), nil
}

type routerOSProbeTarget struct {
	Port   int
	UseTLS bool
}

func routerOSProbeTargets(preferredPort int, preferredTLS bool, session *server.WinboxSession) []routerOSProbeTarget {
	targets := make([]routerOSProbeTarget, 0, 3)
	seen := make(map[string]struct{})
	appendTarget := func(port int, useTLS bool) {
		if port <= 0 {
			return
		}
		key := fmt.Sprintf("%d/%t", port, useTLS)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		targets = append(targets, routerOSProbeTarget{Port: port, UseTLS: useTLS})
	}

	appendTarget(preferredPort, preferredTLS)
	if session != nil {
		appendTarget(session.RouterOSAPIPort, session.RouterOSAPITLS)
	}
	appendTarget(8728, false)
	appendTarget(8729, true)
	return targets
}

func resolveRouterOSPeerContext(backend ServerBackend, peerID string) (*server.PeerMetadata, string, *pb.RouterOSCapability, *server.WinboxSession, error) {
	peer, err := backend.FindPeer(peerID)
	if err != nil {
		return nil, "", &pb.RouterOSCapability{}, nil, fmt.Errorf("peer not found")
	}
	accountID := peer.AccountID
	session := bestRouterOSSession(peer.WinboxSessions)
	return peer, accountID, routerOSCapabilityForPeer(backend, accountID, peer, session), session, nil
}

func peerHasRouterOSCredentials(peer *server.PeerMetadata) bool {
	return peer != nil && len(peer.EncryptedRouterOSUsername) > 0 && len(peer.EncryptedRouterOSPassword) > 0
}

func routerOSProbeHost(peer *server.PeerMetadata, session *server.WinboxSession) string {
	if session != nil && strings.TrimSpace(session.RouterIP) != "" {
		return strings.TrimSpace(session.RouterIP)
	}
	if peer == nil {
		return ""
	}
	return strings.Split(peer.AssignedIP, "/")[0]
}

func probeRouterOSWithCredentials(ctx context.Context, manager *rosapi.Manager, dialer rosapi.DialContextFunc, host, username, password string, preferredPort int, preferredTLS bool, session *server.WinboxSession) (rosapi.ConnectParams, error) {
	var lastErr error
	for _, target := range routerOSProbeTargets(preferredPort, preferredTLS, session) {
		params := rosapi.ConnectParams{
			Address:            net.JoinHostPort(host, fmt.Sprintf("%d", target.Port)),
			Username:           username,
			Password:           password,
			UseTLS:             target.UseTLS,
			InsecureSkipVerify: true,
		}
		if _, err := manager.Probe(ctx, dialer, params); err != nil {
			lastErr = err
			continue
		}
		return params, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("routeros api verification failed")
	}
	return rosapi.ConnectParams{}, lastErr
}

func persistRouterOSAccessSuccess(backend ServerBackend, accountID string, peer *server.PeerMetadata, session *server.WinboxSession, username, password, source string, params rosapi.ConnectParams) error {
	cipher, err := credentialCipherForAccount(backend, accountID)
	if err != nil {
		return err
	}
	encryptedUsername, err := cipher.Encrypt([]byte(username))
	if err != nil {
		return err
	}
	encryptedPassword, err := cipher.Encrypt([]byte(password))
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	peer.EncryptedRouterOSUsername = encryptedUsername
	peer.EncryptedRouterOSPassword = encryptedPassword
	peer.RouterOSCredentialSource = source
	peer.RouterOSAPIVerified = true
	peer.RouterOSAPILastValidated = now
	peer.RouterOSAPIError = ""
	peer.RouterOSAPIPort = routerOSPortFromAddress(params.Address)
	peer.RouterOSAPITLS = params.UseTLS
	peer.UpdatedAt = now

	if err := backend.GetPeerStore().SavePeer(peer); err != nil {
		return err
	}

	if session != nil {
		session.RouterOSAPIVerified = true
		session.RouterOSAPILastValidated = now
		session.RouterOSAPIError = ""
		session.RouterOSAPIPort = peer.RouterOSAPIPort
		session.RouterOSAPITLS = peer.RouterOSAPITLS
		session.CredentialsValid = true
		session.LastValidated = now
		session.ValidationError = ""
		session.UpdatedAt = now
		if err := backend.GetPeerStore().SaveWinboxSession(accountID, peer.ID, session); err != nil {
			return err
		}
	}

	return nil
}

func persistRouterOSProbeFailure(backend ServerBackend, accountID string, peer *server.PeerMetadata, session *server.WinboxSession, userErr string) error {
	now := time.Now().UTC()
	peer.RouterOSAPIVerified = false
	peer.RouterOSAPILastValidated = now
	peer.RouterOSAPIError = userErr
	peer.UpdatedAt = now

	if err := backend.GetPeerStore().SavePeer(peer); err != nil {
		return err
	}

	if session != nil {
		session.RouterOSAPIVerified = false
		session.RouterOSAPILastValidated = now
		session.RouterOSAPIError = userErr
		session.UpdatedAt = now
		if err := backend.GetPeerStore().SaveWinboxSession(accountID, peer.ID, session); err != nil {
			return err
		}
	}

	return nil
}

func routerOSPortFromAddress(address string) int {
	_, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return 0
	}
	var port int
	fmt.Sscanf(rawPort, "%d", &port)
	return port
}

func bestRouterOSSession(sessions []server.WinboxSession) *server.WinboxSession {
	for i := range sessions {
		if sessions[i].Enabled && sessions[i].RouterOSAPIVerified {
			return &sessions[i]
		}
	}
	for i := range sessions {
		if sessions[i].Enabled {
			return &sessions[i]
		}
	}
	return nil
}

func routerOSCandidate(backend ServerBackend, _ string, peer *server.PeerMetadata) bool {
	if peer == nil {
		return false
	}
	if peer.HasWinbox || peer.ScannedWinboxPort > 0 || len(peer.WinboxSessions) > 0 {
		return true
	}
	scan, err := backend.GetPeerScanResult(peer.ID)
	if err != nil || scan == nil {
		return false
	}
	if scan.Fingerprint != nil {
		vendor := strings.ToLower(scan.Fingerprint.Vendor)
		osFamily := strings.ToLower(scan.Fingerprint.OSFamily)
		if vendor == "mikrotik" || osFamily == "routeros" {
			return true
		}
	}
	for _, port := range scan.OpenPorts() {
		if port.Port == 8291 || port.Port == 8728 || port.Port == 8729 {
			return true
		}
		service := strings.ToLower(port.Service)
		banner := strings.ToLower(port.Banner)
		if strings.Contains(service, "winbox") || strings.Contains(service, "mikrotik") || strings.Contains(banner, "routeros") || strings.Contains(banner, "mikrotik") {
			return true
		}
	}
	return false
}

func routerOSCapabilityForPeer(backend ServerBackend, accountID string, peer *server.PeerMetadata, session *server.WinboxSession) *pb.RouterOSCapability {
	candidate := routerOSCandidate(backend, accountID, peer)
	capability := &pb.RouterOSCapability{
		Candidate:        candidate,
		HasSavedWinbox:   session != nil,
		HasSavedAccess:   peerHasRouterOSCredentials(peer),
		CredentialSource: routerOSCredentialSource(peer, session),
	}
	if session != nil {
		capability.SessionId = session.ID
	}
	if peer == nil {
		return capability
	}
	if peer.RouterOSAPIVerified || peer.RouterOSAPIPort > 0 || peer.RouterOSAPIError != "" || !peer.RouterOSAPILastValidated.IsZero() {
		capability.ApiReady = peer.RouterOSAPIVerified
		capability.ApiPort = int32(peer.RouterOSAPIPort)
		capability.ApiTls = peer.RouterOSAPITLS
		capability.LastError = peer.RouterOSAPIError
		if !peer.RouterOSAPILastValidated.IsZero() {
			capability.LastValidated = pb.TimestampFromTime(peer.RouterOSAPILastValidated)
		}
	} else if session != nil {
		capability.ApiReady = session.RouterOSAPIVerified
		capability.ApiPort = int32(session.RouterOSAPIPort)
		capability.ApiTls = session.RouterOSAPITLS
		capability.LastError = session.RouterOSAPIError
		capability.SessionId = session.ID
		if !session.RouterOSAPILastValidated.IsZero() {
			capability.LastValidated = pb.TimestampFromTime(session.RouterOSAPILastValidated)
		}
	}
	capability.PreferredUsername = routerOSPreferredUsername(backend, accountID, peer, session)
	return capability
}

func routerOSCapabilityFromSession(candidate bool, session *server.WinboxSession) *pb.RouterOSCapability {
	capability := &pb.RouterOSCapability{
		Candidate: candidate,
	}
	if session == nil {
		return capability
	}
	capability.ApiReady = session.RouterOSAPIVerified
	capability.ApiPort = int32(session.RouterOSAPIPort)
	capability.ApiTls = session.RouterOSAPITLS
	capability.LastError = session.RouterOSAPIError
	capability.SessionId = session.ID
	capability.HasSavedWinbox = true
	capability.CredentialSource = routerOSCredentialSourceWinbox
	if !session.RouterOSAPILastValidated.IsZero() {
		capability.LastValidated = pb.TimestampFromTime(session.RouterOSAPILastValidated)
	}
	return capability
}

func routerOSCredentialSource(peer *server.PeerMetadata, session *server.WinboxSession) string {
	if peerHasRouterOSCredentials(peer) {
		if strings.TrimSpace(peer.RouterOSCredentialSource) != "" {
			return peer.RouterOSCredentialSource
		}
		return routerOSCredentialSourcePeer
	}
	if session != nil {
		return routerOSCredentialSourceWinbox
	}
	return routerOSCredentialSourceNone
}

func routerOSPreferredUsername(backend ServerBackend, accountID string, peer *server.PeerMetadata, session *server.WinboxSession) string {
	if peerHasRouterOSCredentials(peer) {
		if username, _, err := decryptPeerRouterOSCredentials(backend, accountID, peer); err == nil {
			return username
		}
	}
	if session != nil {
		if username, _, err := decryptWinboxCredentials(backend, accountID, session); err == nil {
			return username
		}
	}
	return ""
}

func routerOSUserError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "only available for mikrotik"):
		return "RouterOS access is only available for MikroTik devices."
	case strings.Contains(message, "peer not found"):
		return "This device is no longer available."
	case strings.Contains(message, "credentials are required"), strings.Contains(message, "no saved winbox account"):
		return "RouterOS credentials are required before this dashboard can connect."
	case strings.Contains(message, "could not decrypt"), strings.Contains(message, "decrypt"):
		return "Saved device credentials are unavailable. Please enter them again."
	case strings.Contains(message, "tenant device is not available"):
		return "The overlay tunnel for this tenant is not ready yet. Please try again in a moment."
	case strings.Contains(message, "login failed"), strings.Contains(message, "invalid user"), strings.Contains(message, "not enough permissions"), strings.Contains(message, "wrong password"):
		return "The RouterOS username or password was rejected."
	case strings.Contains(message, "connection refused"), strings.Contains(message, "no route to host"), strings.Contains(message, "network is unreachable"), strings.Contains(message, "i/o timeout"), strings.Contains(message, "deadline exceeded"), strings.Contains(message, "connection reset"):
		return "The device did not answer on the RouterOS API. Check that the API service is enabled and reachable over the tunnel."
	default:
		return "Could not connect to the RouterOS API. Check the device credentials and try again."
	}
}

func routerOSDisplayError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(message, "routeros id is required"),
		strings.Contains(message, "unsupported routeros resource"),
		strings.Contains(message, "add is not supported"),
		strings.Contains(message, "set is not supported"),
		strings.Contains(message, "remove is not supported"):
		return err.Error()
	default:
		return routerOSUserError(err)
	}
}

func errorFromString(message string, success bool) error {
	if success || strings.TrimSpace(message) == "" {
		return nil
	}
	return errors.New(message)
}

func routerOSStreamAccessRequired(access *routerOSResolvedAccess, err error) bool {
	if err == nil || access == nil || access.capability == nil {
		return false
	}
	if !access.capability.Candidate {
		return false
	}
	if access.capability.ApiReady {
		return false
	}
	return true
}

func routerOSShouldReconnectSession(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "eof") ||
		strings.Contains(message, "broken pipe") ||
		strings.Contains(message, "connection reset") ||
		strings.Contains(message, "closed network connection") ||
		strings.Contains(message, "use of closed network connection")
}

func routerOSPeerFlags(peer *server.PeerMetadata, fingerprint *pb.OSFingerprint, ports []*pb.OpenPort) (candidate bool, ready bool, port int32, useTLS bool) {
	candidate = peer != nil && (peer.HasWinbox || peer.ScannedWinboxPort > 0 || len(peer.WinboxSessions) > 0)
	if fingerprint != nil {
		if strings.EqualFold(fingerprint.Vendor, "MikroTik") || strings.EqualFold(fingerprint.OsFamily, "routeros") {
			candidate = true
		}
	}
	for _, p := range ports {
		if p == nil {
			continue
		}
		if p.Port == 8291 || p.Port == 8728 || p.Port == 8729 {
			candidate = true
		}
	}
	if peer == nil {
		return candidate, false, 0, false
	}
	if peer.RouterOSAPIVerified {
		return candidate, true, int32(peer.RouterOSAPIPort), peer.RouterOSAPITLS
	}
	for _, session := range peer.WinboxSessions {
		if session.Enabled && session.RouterOSAPIVerified {
			return candidate, true, int32(session.RouterOSAPIPort), session.RouterOSAPITLS
		}
	}
	return candidate, false, 0, false
}
