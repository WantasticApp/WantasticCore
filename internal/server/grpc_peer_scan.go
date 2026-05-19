package server

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"sync/atomic"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/errs"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// activeScans is now a field on Server (see server.go) to avoid global state.

const legacyScanControlChannel = "scan:control"

// Helper to get scan progress channel
func getScanChannel(scanID string) string {
	return fmt.Sprintf("scan:progress:%s", scanID)
}

// Helper to get scan status key
func getScanStatusKey(scanID string) string {
	return fmt.Sprintf("scan:status:%s", scanID)
}

func getActiveScanKey(peerID string) string {
	return fmt.Sprintf("scan:active:%s", peerID)
}

func getScanSessionKey(scanID string) string {
	return fmt.Sprintf("portscan:session:%s", scanID)
}

func targetedScanControlChannel(hubID string) string {
	if hubID == "" {
		return legacyScanControlChannel
	}
	return legacyScanControlChannel + ":" + hubID
}

func scanWorkerCount(totalPorts, ceiling int) int {
	if totalPorts <= 0 {
		return 1
	}
	if ceiling <= 0 || totalPorts < ceiling {
		return totalPorts
	}
	return ceiling
}

func manualScanProfile(totalPorts int, fullScan bool) (int, time.Duration) {
	switch {
	case fullScan:
		return scanWorkerCount(totalPorts, 96), 1500 * time.Millisecond
	case totalPorts <= 64:
		return scanWorkerCount(totalPorts, 32), 2 * time.Second
	default:
		return scanWorkerCount(totalPorts, 64), 2 * time.Second
	}
}

func (s *Server) resolveAccountID(accountID string) string {
	if len(accountID) >= 30 {
		return accountID
	}

	accounts, err := s.accountMgr.ListAccounts()
	if err != nil {
		return accountID
	}

	for _, acc := range accounts {
		if acc.Name == accountID {
			log.Debug().Str("slug", accountID).Str("uuid", acc.ID).Msg(" Resolved Account Slug to UUID for Scan")
			return acc.ID
		}
	}

	return accountID
}

func (s *Server) registerScanSession(scanID string) {
	if s.redisClient == nil || scanID == "" {
		return
	}

	if err := s.redisClient.Set(context.Background(), getScanSessionKey(scanID), s.getHubID(), 2*time.Hour).Err(); err != nil {
		log.Warn().Err(err).Str("scan_id", scanID).Msg("Failed to register scan session in Redis")
		return
	}

	log.Debug().Str("scan_id", scanID).Str("hub_id", s.getHubID()).Msg(" Registered scan session in Redis")
}

func (s *Server) unregisterScanSession(scanID string) {
	if s.redisClient == nil || scanID == "" {
		return
	}

	s.redisClient.Del(context.Background(), getScanSessionKey(scanID))
}

func (s *Server) lookupScanSessionHubID(ctx context.Context, scanID string) string {
	if s.redisClient == nil || scanID == "" {
		return ""
	}

	hubID, err := s.redisClient.Get(ctx, getScanSessionKey(scanID)).Result()
	if err != nil || hubID == "" {
		return ""
	}

	return hubID
}

func (s *Server) clearActiveScanID(peerID, scanID string) {
	if s.redisClient == nil || peerID == "" {
		return
	}

	key := getActiveScanKey(peerID)
	if scanID != "" {
		currentScanID, err := s.redisClient.Get(context.Background(), key).Result()
		if err == nil && currentScanID != "" && currentScanID != scanID {
			return
		}
	}

	s.redisClient.Del(context.Background(), key)
}

func (s *Server) publishScanUpdate(accountID, peerID string, update *pb.PortScanStatusUpdate) {
	if s.redisClient == nil || update == nil {
		return
	}

	data, _ := json.Marshal(update)
	if update.ScanId != "" {
		s.redisClient.Publish(context.Background(), getScanChannel(update.ScanId), data)
		s.redisClient.Set(context.Background(), getScanStatusKey(update.ScanId), data, 1*time.Hour)
	}
	if accountID != "" {
		s.redisClient.Publish(context.Background(), fmt.Sprintf("tenant:%s:scan", accountID), data)
	}
	if peerID == "" {
		peerID = update.PeerId
	}
	if peerID != "" && update.ScanId != "" {
		switch update.Status {
		case "completed", "failed", "stopped":
			s.clearActiveScanID(peerID, update.ScanId)
		default:
			s.redisClient.Set(context.Background(), getActiveScanKey(peerID), update.ScanId, 10*time.Minute)
		}
	}
	if peerID != "" {
		s.redisClient.Publish(context.Background(), fmt.Sprintf("scan:progress:%s", peerID), data)
	}
}

func (s *Server) StartPortScan(ctx context.Context, req *pb.StartPortScanRequest) (*pb.StartPortScanResponse, error) {
	log.Debug().Str("account_id", req.AccountId).Str("peer_id", req.PeerId).Bool("full", req.FullScan).Msg(" StartPortScan requested")

	if req.AccountId == "" || req.PeerId == "" {
		return nil, errs.InvalidArgumentE("account_id and peer_id are required")
	}

	// Resolve Account ID early so locks and control routing stay consistent.
	resolvedAccountID := s.resolveAccountID(req.AccountId)

	// Check if scan is already running for this peer
	if s.redisClient != nil {
		lockKey := s.getScanLockKey(resolvedAccountID, req.PeerId)
		if existingScanID, err := s.redisClient.Get(ctx, lockKey).Result(); err == nil && existingScanID != "" {
			log.Debug().Str("peer_id", req.PeerId).Str("existing_scan_id", existingScanID).Msg(" Scan already running, joining existing session")
			return &pb.StartPortScanResponse{
				ScanId:  existingScanID,
				Status:  "started",
				Message: "Scan already in progress (joined)",
			}, nil
		}
	}

	// Generate scan ID proactively
	scanID := uuid.New().String()

	// Try to start locally
	err := s.startPortScanInternal(ctx, scanID, resolvedAccountID, req.PeerId, req.FullScan, req.Ports, req.Tcp, req.Udp)
	if err == nil {
		return &pb.StartPortScanResponse{
			ScanId:  scanID,
			Status:  "started",
			Message: fmt.Sprintf("Started scan on %s", req.PeerId),
		}, nil
	}

	// If internal error was "Not Found", it means we are not the owner core.
	// Broadcast the command.
	// If internal error was "Not Found", it means we are not the owner core.
	// Broadcast the command.
	if errs.IsCode(err, errs.NotFound) {
		s.publishControlCommandStart(ctx, scanID, resolvedAccountID, req.PeerId, req.FullScan, req.Ports, req.Tcp, req.Udp)
		return &pb.StartPortScanResponse{
			ScanId:  scanID,
			Status:  "started",
			Message: "Scan start broadcasted to cluster",
		}, nil
	}

	return nil, err
}

func (s *Server) publishControlCommandStart(ctx context.Context, scanId, accountId, peerId string, fullScan bool, ports []int32, tcp, udp bool) {
	if s.redisClient == nil {
		return
	}
	msg := ScanControlMessage{
		Command:   "start",
		ScanId:    scanId,
		AccountId: accountId,
		PeerId:    peerId,
		FullScan:  fullScan,
		Ports:     ports,
		Tcp:       tcp,
		Udp:       udp,
	}
	data, _ := json.Marshal(msg)
	s.redisClient.Publish(ctx, legacyScanControlChannel, data)
}

// Internal method to actually start the scan (must be on owner core)
func (s *Server) startPortScanInternal(_ context.Context, scanID, accountID, peerID string, fullScan bool, customPorts []int32, scanTCP, scanUDP bool) error {
	// 1. Get Tenant Device
	s.mu.RLock()
	device, ok := s.tenantDevices[accountID]
	s.mu.RUnlock()
	if !ok {
		// Downgrade to Debug as this is normal in a distributed cluster (not owner)
		log.Debug().Str("account_id", accountID).Msg("account not found locally, delegating")
		return errs.NotFoundE("account not found locally")
	}

	// 2. Get Peer Info (IP)
	statusMap, err := device.GetAllPeersStatus()
	if err != nil {
		return errs.Internalf("failed to get peer status: %v", err)
	}
	peerStatus, ok := statusMap[peerID]
	if !ok {
		return errs.NotFoundE("peer not found")
	}
	if !peerStatus.IsOnline || peerStatus.AssignedIP == "" {
		return errs.UnavailableE("peer is offline")
	}

	// 3. Check and Acquire Lock
	if !s.acquireScanLock(accountID, peerID, scanID) {
		return errs.AlreadyExistsE("port scan already in progress for this peer")
	}

	// 4. Determine ports and scan protocols
	var ports []int

	// If no protocol specified, default to TCP
	if !scanTCP && !scanUDP {
		scanTCP = true
	}

	if len(customPorts) > 0 {
		// Custom ports
		ports = make([]int, len(customPorts))
		for i, p := range customPorts {
			ports[i] = int(p)
		}
	} else if fullScan {
		// Full scan 1-65535
		ports = make([]int, 65535)
		for i := range 65535 {
			ports[i] = i + 1
		}
	} else {
		// Use package-private variable from scan_worker.go
		ports = commonScanPorts
	}

	// 5. Define Progress Callback
	var lastPercent atomic.Int32
	onProgress := func(progress int, currentPort int, found bool) {
		log.Debug().Int("progress", progress).Int("port", currentPort).Bool("found", found).Msg(" Local scanner progress callback")

		// Calculate percentage
		percent := 0
		if len(ports) > 0 {
			percent = int((float64(progress) / float64(len(ports))) * 100)
		}
		if percent > 100 {
			percent = 100
		}
		lastPercent.Store(int32(percent))

		update := &pb.PortScanStatusUpdate{
			ScanId:          scanID,
			Status:          "running",
			ProgressPercent: int32(percent),
			CurrentPort:     int32(currentPort),
			TotalPorts:      int32(len(ports)),
			PeerId:          peerID,
		}

		if found {
			update.OpenPortsCount++ // Stateless approximations
			update.LastFoundPort = &pb.OpenPort{
				Port:     int32(currentPort),
				Protocol: "tcp",
				Service:  "unknown",
			}

			// ACCUMULATE RESULTS IN REDIS
			if s.redisClient != nil {
				// Add to set
				s.redisClient.SAdd(context.Background(), fmt.Sprintf("scan:results:%s", scanID), currentPort)
				log.Debug().Str("scan_id", scanID).Int("port", currentPort).Msg("💾 Added found port to Redis Set")
			}
		}

		// FETCH ALL RESULTS TO BROADCAST FULL STATE
		// (Logic moved to StreamPortScanStatus for catch-up)
		// if s.redisClient != nil {
		// 	// Get all ports found so far
		// 	// portsStr, _ := s.redisClient.SMembers(context.Background(), fmt.Sprintf("scan:results:%s", scanID)).Result()
		// }

		s.publishScanUpdate(accountID, peerID, update)
		log.Debug().Str("scan_id", scanID).Str("channel", getScanChannel(scanID)).Msg("📢 Published scan progress")
	}

	// 6. Create Scanner with a moderate profile based on scan size
	workers, timeout := manualScanProfile(len(ports), fullScan)
	scanner := device.NewPortScanner(workers, timeout, onProgress)

	// Store scanner for local control
	s.activeScansMu.Lock()
	s.activeScans[scanID] = scanner
	s.activeScansMu.Unlock()

	// Register scan session in Redis for routing (so Stop/Pause/Resume can find the right hub)
	s.registerScanSession(scanID)

	// 7. Start in background
	go func() {
		defer s.releaseScanLock(accountID, peerID)
		defer func() {
			s.clearActiveScanID(peerID, scanID)

			// Unregister scan session from Redis on completion
			if s.redisClient != nil {
				s.unregisterScanSession(scanID)

				// CLEANUP: Remove detailed results and progress to prevent garbage accumulation
				// We keep them loosely for a short while if needed, or delete immediately?
				// User requested "clean after and dont leave grabage".
				// The StreamPortScanStatus might rely on them if the user reconnects immediately.
				// Let's set a very short expiry (e.g., 1 minute) instead of immediate delete,
				// OR delete if we are sure streaming is done.
				// For now, let's delete 'scan:results' which is the heavy Set.
				// Status key 'scan:status' is updated below with 1h TTL, maybe reduce that.

				s.redisClient.Del(context.Background(), fmt.Sprintf("scan:results:%s", scanID))
			}

			if r := recover(); r != nil {
				log.Error().Interface("panic", r).Str("stack", string(debug.Stack())).Msg("🔥 Panic in port scan worker")
				// Try to publish failure
				if s.redisClient != nil {
					failUpdate := &pb.PortScanStatusUpdate{
						ScanId:  scanID,
						Status:  "failed",
						Message: fmt.Sprintf("Internal scanner error: %v", r),
						PeerId:  peerID,
					}
					s.publishScanUpdate(accountID, peerID, failUpdate)
				}
			}
			s.activeScansMu.Lock()
			delete(s.activeScans, scanID)
			s.activeScansMu.Unlock()
		}()

		if s.redisClient != nil {
			startUpdate := &pb.PortScanStatusUpdate{
				ScanId:     scanID,
				Status:     "running",
				TotalPorts: int32(len(ports)),
				Message:    "Scan started",
				PeerId:     peerID,
			}
			s.publishScanUpdate(accountID, peerID, startUpdate)
		}

		result := scanner.ScanCustomPorts(peerStatus.AssignedIP, ports, scanTCP, scanUDP)
		log.Debug().Str("scan_id", scanID).Int("open_ports", len(result.Ports)).Msg(" Scan execution finished locally")

		if s.redisClient != nil {
			endStatus := "completed"
			msg := "Scan completed"
			endPercent := int32(100)
			if scanner.IsCancelled() {
				endStatus = "stopped"
				msg = "Scan stopped"
				endPercent = lastPercent.Load()
			}
			// Context check removed as it reflects RPC request lifecycle, not scan lifecycle

			endUpdate := &pb.PortScanStatusUpdate{
				ScanId:          scanID,
				Status:          endStatus,
				ProgressPercent: endPercent,
				TotalPorts:      int32(len(ports)),
				OpenPortsCount:  int32(len(result.Ports)),
				Message:         msg,
				PeerId:          peerID,
			}
			s.publishScanUpdate(accountID, peerID, endUpdate)
		}

		// Also save to DB/Redis persistent storage
		if err := s.SavePeerScanResult(accountID, peerID, result); err != nil {
			log.Warn().Err(err).Msg("Failed to save manual scan result")
		}
	}()

	return nil
}

// ScanControlMessage for Redis Pub/Sub
type ScanControlMessage struct {
	Command   string  `json:"command"` // "stop", "pause", "resume", "start"
	ScanId    string  `json:"scan_id"`
	AccountId string  `json:"account_id"`
	PeerId    string  `json:"peer_id,omitempty"`   // For start
	FullScan  bool    `json:"full_scan,omitempty"` // For start
	Ports     []int32 `json:"ports,omitempty"`
	Tcp       bool    `json:"tcp,omitempty"`
	Udp       bool    `json:"udp,omitempty"`
}

// Subscribe to global scan control channel
func (s *Server) StartScanControlListener(ctx context.Context) {
	if s.redisClient == nil {
		return
	}

	channels := []string{legacyScanControlChannel}
	targetedChannel := targetedScanControlChannel(s.getHubID())
	if targetedChannel != legacyScanControlChannel {
		channels = append(channels, targetedChannel)
	}

	pubsub := s.redisClient.Subscribe(ctx, channels...)
	defer pubsub.Close()

	ch := pubsub.Channel()

	log.Debug().Msg(" Listening for distributed scan control commands")

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			var control ScanControlMessage
			if err := json.Unmarshal([]byte(msg.Payload), &control); err != nil {
				log.Error().Err(err).Msg("Failed to unmarshal scan control message")
				continue
			}

			log.Debug().Str("scan_id", control.ScanId).Str("cmd", control.Command).Msg(" Received distributed scan control command")

			// Handle "start" separately as it doesn't require an active scan
			if control.Command == "start" {
				log.Debug().Str("scan_id", control.ScanId).Msg(" Starting distributed scan")
				// Call internal start (waits for lock, etc)
				// We use background context as the original request context is gone
				err := s.startPortScanInternal(context.Background(), control.ScanId, control.AccountId, control.PeerId, control.FullScan, control.Ports, control.Tcp, control.Udp)

				if err != nil {
					// Only report failure if it's NOT a "Not Found" (wrong node) error.
					// In a broadcast scenario, N-1 nodes will fail with NotFound. We only care if the OWNER fails.
					if errs.IsCode(err, errs.NotFound) {
						log.Debug().Msg("🙈 Distributed start ignored (not owner)")
						continue
					}

					log.Warn().Err(err).Msg("Failed to start distributed scan on owner node")

					// Publish failure to Redis so UI doesn't hang
					failUpdate := &pb.PortScanStatusUpdate{
						ScanId:  control.ScanId,
						Status:  "failed",
						Message: fmt.Sprintf("Failed to start on worker: %v", err),
						PeerId:  control.PeerId,
					}
					s.publishScanUpdate(control.AccountId, control.PeerId, failUpdate)
				}
				continue
			}

			// For stop/pause/resume, check if we have the scan locally
			s.activeScansMu.RLock()
			scanner, ok := s.activeScans[control.ScanId]
			s.activeScansMu.RUnlock()

			if !ok {
				continue // Not ours, ignore
			}

			log.Debug().Str("scan_id", control.ScanId).Str("cmd", control.Command).Msg(" Executing distributed scan control command")

			switch control.Command {
			case "stop":
				scanner.Cancel()
				s.broadcastScanStatus(control.ScanId, control.AccountId, control.PeerId, "stopping", "Stopping scan")
			case "pause":
				scanner.SetPaused(true)
				s.broadcastScanStatus(control.ScanId, control.AccountId, control.PeerId, "paused", "Scan paused")
			case "resume":
				scanner.SetPaused(false)
				s.broadcastScanStatus(control.ScanId, control.AccountId, control.PeerId, "running", "Scan resumed")
			}
		}
	}
}

// Helper to broadcast status updates
func (s *Server) broadcastScanStatus(scanId, accountId, peerId, statusStr, msg string) {
	if s.redisClient == nil {
		return
	}
	update := &pb.PortScanStatusUpdate{
		ScanId:  scanId,
		Status:  statusStr,
		Message: msg,
		PeerId:  peerId,
	}
	s.publishScanUpdate(accountId, peerId, update)
}

func (s *Server) publishControlCommand(ctx context.Context, cmd, scanId, accountId, peerId string) {
	if s.redisClient == nil {
		return
	}

	channel := legacyScanControlChannel
	if hubID := s.lookupScanSessionHubID(ctx, scanId); hubID != "" {
		channel = targetedScanControlChannel(hubID)
	}

	msg := ScanControlMessage{
		Command:   cmd,
		ScanId:    scanId,
		AccountId: accountId,
		PeerId:    peerId,
	}
	data, _ := json.Marshal(msg)
	s.redisClient.Publish(ctx, channel, data)
}

func (s *Server) StopPortScan(ctx context.Context, req *pb.StopPortScanRequest) (*pb.StopPortScanResponse, error) {
	resolvedAccountID := s.resolveAccountID(req.AccountId)

	s.activeScansMu.RLock()
	scanner, ok := s.activeScans[req.ScanId]
	s.activeScansMu.RUnlock()

	if !ok {
		// Not found locally, broadcast stop command
		s.publishControlCommand(ctx, "stop", req.ScanId, resolvedAccountID, req.PeerId)
		return &pb.StopPortScanResponse{Success: true, Message: "Stop command broadcasted"}, nil
	}

	scanner.Cancel()
	s.broadcastScanStatus(req.ScanId, resolvedAccountID, req.PeerId, "stopping", "Stopping scan")
	return &pb.StopPortScanResponse{Success: true, Message: "Scan stopped locally"}, nil
}

func (s *Server) PausePortScan(ctx context.Context, req *pb.PausePortScanRequest) (*pb.PausePortScanResponse, error) {
	resolvedAccountID := s.resolveAccountID(req.AccountId)

	s.activeScansMu.RLock()
	scanner, ok := s.activeScans[req.ScanId]
	s.activeScansMu.RUnlock()

	if !ok {
		// Not found locally, broadcast pause command
		s.publishControlCommand(ctx, "pause", req.ScanId, resolvedAccountID, req.PeerId)
		return &pb.PausePortScanResponse{Success: true, Message: "Pause command broadcasted"}, nil
	}

	scanner.SetPaused(true)
	s.broadcastScanStatus(req.ScanId, resolvedAccountID, req.PeerId, "paused", "Scan paused")

	return &pb.PausePortScanResponse{Success: true, Message: "Scan paused locally"}, nil
}

func (s *Server) ResumePortScan(ctx context.Context, req *pb.ResumePortScanRequest) (*pb.ResumePortScanResponse, error) {
	resolvedAccountID := s.resolveAccountID(req.AccountId)

	s.activeScansMu.RLock()
	scanner, ok := s.activeScans[req.ScanId]
	s.activeScansMu.RUnlock()

	if !ok {
		// Not found locally, broadcast resume command
		s.publishControlCommand(ctx, "resume", req.ScanId, resolvedAccountID, req.PeerId)
		return &pb.ResumePortScanResponse{Success: true, Message: "Resume command broadcasted"}, nil
	}

	scanner.SetPaused(false)
	s.broadcastScanStatus(req.ScanId, resolvedAccountID, req.PeerId, "running", "Scan resumed")

	return &pb.ResumePortScanResponse{Success: true, Message: "Scan resumed locally"}, nil
}

// PortScanStatusStream is the minimal interface needed for streaming port-scan
// status updates. Both the in-process channel-backed ServerStream and gRPC's
// proto-gen stream type satisfy it.
type PortScanStatusStream interface {
	Send(*pb.PortScanStatusUpdate) error
	Context() context.Context
}

func (s *Server) StreamPortScanStatus(req *pb.StreamPortScanStatusRequest, stream PortScanStatusStream) error {
	var scanID string
	var channel string

	// 1. Resolve ScanID and Channel
	if req.ScanId != "" {
		scanID = req.ScanId
		channel = getScanChannel(scanID)
	} else if req.PeerId != "" {
		// Try to find active scan
		activeScanID, err := s.redisClient.Get(stream.Context(), fmt.Sprintf("scan:active:%s", req.PeerId)).Result()
		if err == nil && activeScanID != "" {
			scanID = activeScanID
			channel = getScanChannel(scanID)
			log.Debug().Str("peer_id", req.PeerId).Str("scan_id", scanID).Msg(" Found active scan for peer, redirecting")
		} else {
			// No active scan, listen on peer channel for new scans
			channel = fmt.Sprintf("scan:progress:%s", req.PeerId)
			log.Debug().Str("peer_id", req.PeerId).Msg(" No active scan, listening on peer channel")
		}
	} else {
		return errs.InvalidArgumentE("scan_id or peer_id is required")
	}

	log.Debug().Str("scan_id", scanID).Str("channel", channel).Msg(" Client subscribing to scan updates")

	// 2. Subscribe FIRST (to avoid missing updates during catch-up)
	pubsub := s.redisClient.Subscribe(stream.Context(), channel)
	defer pubsub.Close()

	if _, err := pubsub.Receive(stream.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to subscribe to redis channel")
		return errs.Internalf("failed to subscribe: %v", err)
	}
	ch := pubsub.Channel()

	// 3. Send Catch-up State (if we have a ScanID)
	// This runs AFTER subscription to ensure no gap, but sends BEFORE live updates
	if scanID != "" {
		// A. Send latest status
		statusKey := getScanStatusKey(scanID)
		result, err := s.redisClient.Get(stream.Context(), statusKey).Result()
		var currentStatus string
		if err == nil && result != "" {
			var cachedUpdate pb.PortScanStatusUpdate
			if err := json.Unmarshal([]byte(result), &cachedUpdate); err == nil {
				log.Debug().Str("scan_id", scanID).Str("status", cachedUpdate.Status).Msg(" Sending cached status")
				stream.Send(&cachedUpdate)
				currentStatus = cachedUpdate.Status
			}
		}

		// B. Send ALL discovered ports (Accumulated History)
		// We do this REGARDLESS of running/completed status to ensure UI is in sync
		portsStr, _ := s.redisClient.SMembers(stream.Context(), fmt.Sprintf("scan:results:%s", scanID)).Result()
		if len(portsStr) > 0 {
			log.Debug().Str("scan_id", scanID).Int("count", len(portsStr)).Msg(" Sending accumulated ports history")
			for _, pStr := range portsStr {
				var p int
				fmt.Sscanf(pStr, "%d", &p)
				// Send as "found port" update
				catchUpUpdate := &pb.PortScanStatusUpdate{
					ScanId: scanID,
					Status: currentStatus,
					LastFoundPort: &pb.OpenPort{
						Port:     int32(p),
						Protocol: "tcp",
						Service:  "unknown",
					},
				}
				stream.Send(catchUpUpdate)
			}
		}
	}

	// 4. Live Update Loop
	timeout := time.NewTimer(30 * time.Minute)
	defer timeout.Stop()

	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-timeout.C:
			return nil
		case msg := <-ch:
			log.Debug().Str("payload", msg.Payload).Msg(" Received Redis update")
			var update pb.PortScanStatusUpdate
			if err := json.Unmarshal([]byte(msg.Payload), &update); err != nil {
				continue
			}

			// If we started without a ScanID, update it now so we know for next time?
			// (Not strictly needed if frontend handles capturing scan_id)

			if err := stream.Send(&update); err != nil {
				return err
			}
			if update.Status == "completed" || update.Status == "failed" || update.Status == "stopped" {
				return nil
			}
		}
	}
}
