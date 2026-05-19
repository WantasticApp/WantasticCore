package server

import (
	"context"
	"fmt"
	"runtime/pprof"
	"sync/atomic"
	"time"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/wg/userspace"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Common ports to scan for management and services
var commonScanPorts = []int{
	21,   // FTP
	22,   // SSH
	23,   // Telnet
	53,   // DNS
	80,   // HTTP
	443,  // HTTPS
	161,  // SNMP
	445,  // SMB
	3389, // RDP
	8080, // HTTP Alt
	8291, // Winbox
	8728, // API
	8729, // API-SSL
}

// StartPortScanMonitor starts a background task to periodically scan online peers.
// This ensures that port discovery data is kept fresh for features like "Connect" buttons.
func (s *Server) StartPortScanMonitor(ctx context.Context) {
	// Scan interval (15 minutes as requested)
	ticker := time.NewTicker(15 * time.Minute)

	log.Debug().Msg(" Starting background Port Scan Monitor (Interval: 15m)")

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer ticker.Stop()

		// Run immediately on start? Maybe delay slightly to allow startup
		// time.Sleep(1 * time.Minute)
		// s.runPeriodicScan()

		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("🛑 Port Scan Monitor stopped")
				return
			case <-ticker.C:
				s.runPeriodicScan()
			}
		}
	}()
}

func (s *Server) runPeriodicScan() {
	start := time.Now()
	scannedCount := 0
	skippedCount := 0
	activeCount := 0

	s.mu.RLock()
	// Create a snapshot of devices/peers to iterate
	// We only scan online peers
	type scanTarget struct {
		AccountID string
		PeerID    string
		Device    *userspace.TenantDevice
		IP        string
	}
	var targets []scanTarget

	for accountID, device := range s.tenantDevices {
		statusMap, err := device.GetAllPeersStatus()
		if err != nil {
			continue
		}
		for peerID, status := range statusMap {
			if status.IsOnline && status.AssignedIP != "" {
				targets = append(targets, scanTarget{
					AccountID: accountID,
					PeerID:    peerID,
					Device:    device,
					IP:        status.AssignedIP,
				})
			}
		}
	}
	s.mu.RUnlock()

	activeCount = len(targets)

	// Limit concurrency - User requested "silent worker that doesnt consume a lot of cpu"
	// Lowering to 5 concurrent scans to be extremely lightweight
	sem := make(chan struct{}, 5)

	for _, target := range targets {
		// Generate unique ID for this scan
		scanID := fmt.Sprintf("periodic-%s", uuid.New().String())

		// Check if already scanning (using Redis lock)
		if !s.acquireScanLock(target.AccountID, target.PeerID, scanID) {
			skippedCount++
			continue
		}

		// Launch scan
		go func(t scanTarget, sID string) {
			defer s.releaseScanLock(t.AccountID, t.PeerID)

			sem <- struct{}{}
			defer func() { <-sem }()

			pprof.Do(context.Background(), pprof.Labels(
				"goroutine", "port-scan",
				"account_id", t.AccountID,
				"peer_id", t.PeerID,
			), func(ctx context.Context) {

				// Create progress callback to mirror manual scans.
				// `progress` is the raw count of ports checked, NOT a percentage —
				// the scanner reports it as an op counter. Convert to a percentage
				// here so the UI's progress bar reflects scan completion (was
				// previously sending the raw count, which made the UI show 0%
				// for the whole scan since percent values like 500 don't map to
				// the 0-100 range the bar expects).
				totalPorts := len(commonScanPorts)
				var lastPercent atomic.Int32
				onProgress := func(progress int, currentPort int, found bool) {
					percent := 0
					if totalPorts > 0 {
						percent = int((float64(progress) / float64(totalPorts)) * 100)
					}
					if percent > 100 {
						percent = 100
					}
					lastPercent.Store(int32(percent))

					update := &pb.PortScanStatusUpdate{
						ScanId:          sID,
						Status:          "running",
						ProgressPercent: int32(percent),
						CurrentPort:     int32(currentPort),
						TotalPorts:      int32(totalPorts),
						PeerId:          t.PeerID,
					}

					if found {
						update.OpenPortsCount++
						update.LastFoundPort = &pb.OpenPort{
							Port:     int32(currentPort),
							Protocol: "tcp",
							Service:  "unknown",
						}
						// Add to Redis Set
						if s.redisClient != nil {
							s.redisClient.SAdd(context.Background(), fmt.Sprintf("scan:results:%s", sID), currentPort)
						}
					}

					s.publishScanUpdate(t.AccountID, t.PeerID, update)
				}

				workers := scanWorkerCount(totalPorts, 24)
				scanner := t.Device.NewPortScanner(workers, 2*time.Second, onProgress)

				// Register so manual Pause/Stop/Resume RPCs (which look up by
				// scan_id in s.activeScans) can control the periodic scanner.
				// Without this, clicking Stop/Pause for a periodic scan was a
				// silent no-op — the lookup missed and the broadcast went out
				// to nodes that didn't have it either.
				s.activeScansMu.Lock()
				s.activeScans[sID] = scanner
				s.activeScansMu.Unlock()
				defer func() {
					s.activeScansMu.Lock()
					delete(s.activeScans, sID)
					s.activeScansMu.Unlock()
					s.clearActiveScanID(t.PeerID, sID)
					s.unregisterScanSession(sID)
					if s.redisClient != nil {
						s.redisClient.Del(context.Background(), fmt.Sprintf("scan:results:%s", sID))
					}
				}()
				s.registerScanSession(sID)

				// Publish initial "started" event after the scanner is controllable.
				if s.redisClient != nil {
					startUpdate := &pb.PortScanStatusUpdate{
						ScanId:     sID,
						Status:     "running",
						TotalPorts: int32(totalPorts),
						Message:    "Periodic scan started",
						PeerId:     t.PeerID,
					}
					s.publishScanUpdate(t.AccountID, t.PeerID, startUpdate)
				}

				// Scan
				result := scanner.ScanPorts(t.IP, commonScanPorts)

				// Publish "completed" event
				if s.redisClient != nil {
					endStatus := "completed"
					endMessage := "Periodic scan completed"
					endPercent := int32(100)
					if scanner.IsCancelled() {
						endStatus = "stopped"
						endMessage = "Periodic scan stopped"
						endPercent = lastPercent.Load()
					}

					endUpdate := &pb.PortScanStatusUpdate{
						ScanId:          sID,
						Status:          endStatus,
						ProgressPercent: endPercent,
						TotalPorts:      int32(len(commonScanPorts)),
						OpenPortsCount:  int32(len(result.Ports)),
						Message:         endMessage,
						PeerId:          t.PeerID,
					}
					s.publishScanUpdate(t.AccountID, t.PeerID, endUpdate)
				}

				// Save result directly to Redis using the new helper
				// This bypasses the DB and uses the consistent userspace.ScanResult format
				if err := s.SavePeerScanResult(t.AccountID, t.PeerID, result); err != nil {
					log.Warn().Err(err).Str("peer_id", t.PeerID).Msg("Failed to save periodic scan results to Redis")
				}

				// SSH Discovery: scan ports 22-8000 silently for SSH banner
				// If the main scan didn't find SSH, do a dedicated fast sweep
				hasSSH := false
				for _, pr := range result.Ports {
					if pr.Port == 22 || (pr.Service != "" && len(pr.Service) >= 3 && pr.Service[:3] == "ssh") {
						hasSSH = true
						break
					}
				}
				if !hasSSH {
					sshCtx, sshCancel := context.WithTimeout(ctx, 3*time.Minute)
					sshPort := t.Device.DiscoverSSHPort(sshCtx, t.IP)
					sshCancel()
					if sshPort > 0 {
						log.Debug().Str("peer_id", t.PeerID).Int("ssh_port", sshPort).Msg("🔑 Discovered SSH port via sweep")
						// Update peer in DB
						if peer, err := s.peerStore.GetPeer(t.AccountID, t.PeerID); err == nil {
							peer.ScannedSSHPort = sshPort
							_ = s.peerStore.SavePeer(peer)
						}
					}
				}
			}) // end pprof.Do
		}(target, scanID)
		scannedCount++
	}

	if scannedCount > 0 {
		log.Debug().
			Int("eligible_peers", activeCount).
			Int("triggered", scannedCount).
			Int("skipped_active", skippedCount).
			Str("duration", time.Since(start).String()).
			Msg(" Periodic Port Scan initiated")
	}
}
