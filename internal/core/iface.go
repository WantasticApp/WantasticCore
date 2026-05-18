// Code generated from api/proto/overlay_grpc.pb.go. DO NOT EDIT manually.
// This file replaces the proto-generated server interfaces so the project
// has zero `google.golang.org/grpc` dependency.
//
// Streaming methods use local BidiStream / ServerStream interfaces that
// don't require the gRPC runtime; the in-process implementations live in
// internal/portalsrv/pkg/services/local_stream.go.

package core

import (
	"context"

	pb "WantasticCore/internal/types"
)

// BidiStream is the in-process replacement for grpc.BidiStreamingServer[Req, Resp].
// The portal's WebSocket dispatcher provides a channel-backed implementation
// (services.LocalBidiStream); service impls only need Send / Recv / Context.
type BidiStream[Req, Resp any] interface {
	Send(Resp) error
	Recv() (Req, error)
	Context() context.Context
}

// ServerStream is the in-process replacement for grpc.ServerStreamingServer[Resp].
type ServerStream[Resp any] interface {
	Send(Resp) error
	Context() context.Context
}

type AccountService interface {
	// Create a new tenant account
	CreateAccount(context.Context, *pb.CreateAccountRequest) (*pb.CreateAccountResponse, error)
	// Get account details by ID
	GetAccount(context.Context, *pb.GetAccountRequest) (*pb.GetAccountResponse, error)
	// List all accounts (admin only)
	ListAccounts(context.Context, *pb.ListAccountsRequest) (*pb.ListAccountsResponse, error)
	// Delete an account and all its resources
	DeleteAccount(context.Context, *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error)
	// Update account quotas (deprecated - use UpdateAccountTier)
	UpdateAccountQuotas(context.Context, *pb.UpdateAccountQuotasRequest) (*pb.UpdateAccountQuotasResponse, error)
	// Update account tier level (Free, Standard, Premium)
	UpdateAccountTier(context.Context, *pb.UpdateAccountTierRequest) (*pb.UpdateAccountTierResponse, error)
}

type NetworkService interface {
	// Get network details for an account
	GetNetwork(context.Context, *pb.GetNetworkRequest) (*pb.GetNetworkResponse, error)
	// Get network statistics
	GetNetworkStats(context.Context, *pb.GetNetworkStatsRequest) (*pb.GetNetworkStatsResponse, error)
	// Get IP allocation statistics for an account
	GetAccountIPStatistics(context.Context, *pb.GetAccountIPStatisticsRequest) (*pb.GetAccountIPStatisticsResponse, error)
}

type PeerService interface {
	// Add a new peer to the network
	AddPeer(context.Context, *pb.AddPeerRequest) (*pb.AddPeerResponse, error)
	// Get peer details
	GetPeer(context.Context, *pb.GetPeerRequest) (*pb.GetPeerResponse, error)
	// List all peers in an account
	ListPeers(context.Context, *pb.ListPeersRequest) (*pb.ListPeersResponse, error)
	// Update notes for a specific peer
	UpdatePeerNotes(context.Context, *pb.UpdatePeerNotesRequest) (*pb.UpdatePeerNotesResponse, error)
	// Remove a peer from the network
	RemovePeer(context.Context, *pb.RemovePeerRequest) (*pb.RemovePeerResponse, error)
	// Generate WireGuard configuration for a peer
	GetPeerConfig(context.Context, *pb.GetPeerConfigRequest) (*pb.GetPeerConfigResponse, error)
	// Get peer statistics
	GetPeerStats(context.Context, *pb.GetPeerStatsRequest) (*pb.GetPeerStatsResponse, error)
	// Ping a peer and collect statistics (for userspace mode)
	PingPeer(context.Context, *pb.PingPeerRequest) (*pb.PingPeerResponse, error)
	// StreamPing sends ICMP pings and streams each result as it arrives.
	// The portal receives real-time updates instead of waiting for all pings.
	StreamPing(*pb.StreamPingRequest, ServerStream[*pb.PingEvent]) error
	// Winbox multiplexer management
	SetWinboxCredentials(context.Context, *pb.SetWinboxCredentialsRequest) (*pb.SetWinboxCredentialsResponse, error)
	GetWinboxStatus(context.Context, *pb.GetWinboxStatusRequest) (*pb.GetWinboxStatusResponse, error)
	ClearWinboxCredentials(context.Context, *pb.ClearWinboxCredentialsRequest) (*pb.ClearWinboxCredentialsResponse, error)
	// Winbox session management (multiple sessions per peer)
	CreateWinboxSession(context.Context, *pb.CreateWinboxSessionRequest) (*pb.CreateWinboxSessionResponse, error)
	UpdateWinboxSession(context.Context, *pb.UpdateWinboxSessionRequest) (*pb.UpdateWinboxSessionResponse, error)
	DeleteWinboxSession(context.Context, *pb.DeleteWinboxSessionRequest) (*pb.DeleteWinboxSessionResponse, error)
	ListWinboxSessions(context.Context, *pb.ListWinboxSessionsRequest) (*pb.ListWinboxSessionsResponse, error)
	GetWinboxSession(context.Context, *pb.GetWinboxSessionRequest) (*pb.GetWinboxSessionResponse, error)
	// Port Scanning Control
	StartPortScan(context.Context, *pb.StartPortScanRequest) (*pb.StartPortScanResponse, error)
	StopPortScan(context.Context, *pb.StopPortScanRequest) (*pb.StopPortScanResponse, error)
	PausePortScan(context.Context, *pb.PausePortScanRequest) (*pb.PausePortScanResponse, error)
	ResumePortScan(context.Context, *pb.ResumePortScanRequest) (*pb.ResumePortScanResponse, error)
	StreamPortScanStatus(*pb.StreamPortScanStatusRequest, ServerStream[*pb.PortScanStatusUpdate]) error
}

type AdminService interface {
	// Get global statistics
	GetGlobalStats(context.Context, *pb.GetGlobalStatsRequest) (*pb.GetGlobalStatsResponse, error)
	// Health check
	HealthCheck(context.Context, *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error)
	// Get network topology for visualization
	// If account_id is empty, returns global topology (all tenants)
	// If account_id is set, returns topology for that tenant only
	GetTopology(context.Context, *pb.GetTopologyRequest) (*pb.GetTopologyResponse, error)
	// Check if admin setup is required (no admin exists yet)
	CheckAdminSetup(context.Context, *pb.CheckAdminSetupRequest) (*pb.CheckAdminSetupResponse, error)
	// Create the first admin user (only works if no admin exists)
	CreateFirstAdmin(context.Context, *pb.CreateFirstAdminRequest) (*pb.CreateFirstAdminResponse, error)
	// Admin login (returns session token)
	AdminLogin(context.Context, *pb.AdminLoginRequest) (*pb.AdminLoginResponse, error)
	// Validate admin session
	ValidateAdminSession(context.Context, *pb.ValidateAdminSessionRequest) (*pb.ValidateAdminSessionResponse, error)
	// Logout admin (invalidate session)
	AdminLogout(context.Context, *pb.AdminLogoutRequest) (*pb.AdminLogoutResponse, error)
	// Get admin profile
	GetAdminProfile(context.Context, *pb.GetAdminProfileRequest) (*pb.GetAdminProfileResponse, error)
	// Update admin settings (password, TOTP)
	UpdateAdminSettings(context.Context, *pb.UpdateAdminSettingsRequest) (*pb.UpdateAdminSettingsResponse, error)
}

type AuthService interface {
	// Validate a session token
	ValidateSession(context.Context, *pb.ValidateSessionRequest) (*pb.ValidateSessionResponse, error)
	// RegisterDevice provisions a WireGuard peer for an authenticated agent.
	RegisterDevice(context.Context, *pb.RegisterDeviceRequest) (*pb.RegisterDeviceResponse, error)
	// RefreshToken refreshes an existing authentication token.
	RefreshToken(context.Context, *pb.DeviceRefreshTokenRequest) (*pb.DeviceRefreshTokenResponse, error)
	// GetConfiguration retrieves the current WireGuard configuration for a device.
	GetConfiguration(context.Context, *pb.GetDeviceConfigurationRequest) (*pb.GetDeviceConfigurationResponse, error)
	// StartDeviceFlow initiates an OAuth2 Device Authorization Grant (RFC 8628).
	StartDeviceFlow(context.Context, *pb.StartDeviceFlowRequest) (*pb.StartDeviceFlowResponse, error)
	// PollDeviceFlow polls for completion of a device authorization flow.
	PollDeviceFlow(context.Context, *pb.PollDeviceFlowRequest) (*pb.PollDeviceFlowResponse, error)
}

type WebSSHService interface {
	// Create a WebSSH session with SSH credentials
	CreateWebSSHSession(context.Context, *pb.CreateWebSSHSessionRequest) (*pb.CreateWebSSHSessionResponse, error)
	// Bidirectional streaming for SSH terminal data
	// Client sends SSHInput (keystrokes, resize), server sends SSHOutput (terminal output)
	StreamSSH(BidiStream[*pb.SSHStreamMessage, *pb.SSHStreamMessage]) error
	// Get WebSSH session information
	GetWebSSHSession(context.Context, *pb.GetWebSSHSessionRequest) (*pb.GetWebSSHSessionResponse, error)
	// List all active WebSSH sessions for a tenant
	ListWebSSHSessions(context.Context, *pb.ListWebSSHSessionsRequest) (*pb.ListWebSSHSessionsResponse, error)
	// Disconnect a WebSSH session
	DisconnectWebSSHSession(context.Context, *pb.DisconnectWebSSHSessionRequest) (*pb.DisconnectWebSSHSessionResponse, error)
}

type WebProxyService interface {
	// Create a web proxy session for a peer's HTTP/HTTPS port
	CreateWebProxySession(context.Context, *pb.CreateWebProxySessionRequest) (*pb.CreateWebProxySessionResponse, error)
	// Bidirectional streaming for HTTP request/response proxying.
	// All HTTP requests and proxied WebSockets multiplex over a single
	// StreamHTTP per browser session, demuxed by request_id (one virtual
	// stream per logical request). See internal/webproxy/wpmux.
	StreamHTTP(BidiStream[*pb.WebProxyStreamMessage, *pb.WebProxyStreamMessage]) error
	// Get web proxy session information
	GetWebProxySession(context.Context, *pb.GetWebProxySessionRequest) (*pb.GetWebProxySessionResponse, error)
	// List all active web proxy sessions for a tenant
	ListWebProxySessions(context.Context, *pb.ListWebProxySessionsRequest) (*pb.ListWebProxySessionsResponse, error)
	// Close a web proxy session
	CloseWebProxySession(context.Context, *pb.CloseWebProxySessionRequest) (*pb.CloseWebProxySessionResponse, error)
}

type ACLService interface {
	// Create a new peer group
	CreatePeerGroup(context.Context, *pb.CreatePeerGroupRequest) (*pb.CreatePeerGroupResponse, error)
	// Delete a peer group
	DeletePeerGroup(context.Context, *pb.DeletePeerGroupRequest) (*pb.DeletePeerGroupResponse, error)
	// Get all peer groups for an account
	ListPeerGroups(context.Context, *pb.ListPeerGroupsRequest) (*pb.ListPeerGroupsResponse, error)
	// Add a peer to a group
	AddPeerToGroup(context.Context, *pb.AddPeerToGroupRequest) (*pb.AddPeerToGroupResponse, error)
	// Remove a peer from a group
	RemovePeerFromGroup(context.Context, *pb.RemovePeerFromGroupRequest) (*pb.RemovePeerFromGroupResponse, error)
	// Create a link between two groups
	CreateGroupLink(context.Context, *pb.CreateGroupLinkRequest) (*pb.CreateGroupLinkResponse, error)
	// Delete a group link
	DeleteGroupLink(context.Context, *pb.DeleteGroupLinkRequest) (*pb.DeleteGroupLinkResponse, error)
	// Get all group links for an account
	ListGroupLinks(context.Context, *pb.ListGroupLinksRequest) (*pb.ListGroupLinksResponse, error)
	// Compile peer groups and links into ACL rules
	CompileGroups(context.Context, *pb.CompileGroupsRequest) (*pb.CompileGroupsResponse, error)
	// Get compilation statistics
	GetCompilationStats(context.Context, *pb.GetCompilationStatsRequest) (*pb.GetCompilationStatsResponse, error)
	// Add an access control rule
	AddACLRule(context.Context, *pb.AddACLRuleRequest) (*pb.AddACLRuleResponse, error)
	// Remove an access control rule
	RemoveACLRule(context.Context, *pb.RemoveACLRuleRequest) (*pb.RemoveACLRuleResponse, error)
	// Get all ACL rules for an account
	GetACLRules(context.Context, *pb.GetACLRulesRequest) (*pb.GetACLRulesResponse, error)
	// Check if access is allowed (for testing/debugging)
	CheckAccess(context.Context, *pb.CheckAccessRequest) (*pb.CheckAccessResponse, error)
}

type TenantRegistrationService interface {
	// Get payment configuration status - check if Stripe is configured
	GetPaymentStatus(context.Context, *pb.GetPaymentStatusRequest) (*pb.GetPaymentStatusResponse, error)
	// Get available subscription plans (no auth required - for registration)
	GetAvailablePlans(context.Context, *pb.GetAvailablePlansRequest) (*pb.GetAvailablePlansResponse, error)
	// Get allowed phone regions for registration (returns country codes with dial codes)
	GetAllowedPhoneRegions(context.Context, *pb.GetAllowedPhoneRegionsRequest) (*pb.GetAllowedPhoneRegionsResponse, error)
	// Start registration - validates email, creates session, sends phone verification and CAPTCHA
	StartRegistration(context.Context, *pb.StartRegistrationRequest) (*pb.StartRegistrationResponse, error)
	// Verify CAPTCHA answer for registration (called before completing registration)
	VerifyCaptcha(context.Context, *pb.CaptchaVerifyRequest) (*pb.CaptchaVerifyResponse, error)
	// Verify phone number with code
	VerifyPhone(context.Context, *pb.VerifyPhoneRequest) (*pb.VerifyPhoneResponse, error)
	// Complete registration with password and optional payment
	CompleteRegistration(context.Context, *pb.CompleteRegistrationRequest) (*pb.CompleteRegistrationResponse, error)
	// Get Stripe checkout session for paid tier
	CreateCheckoutSession(context.Context, *pb.CreateCheckoutSessionRequest) (*pb.CreateCheckoutSessionResponse, error)
	// Create Stripe SetupIntent for adding payment method (during registration)
	CreateSetupIntent(context.Context, *pb.CreateSetupIntentRequest) (*pb.CreateSetupIntentResponse, error)
	// Check registration status
	GetRegistrationStatus(context.Context, *pb.GetRegistrationStatusRequest) (*pb.GetRegistrationStatusResponse, error)
	// Resend phone verification code
	ResendPhoneVerification(context.Context, *pb.ResendPhoneVerificationRequest) (*pb.ResendPhoneVerificationResponse, error)
	// Process Stripe webhook event (forwarded from tenant proxy)
	ProcessStripeWebhook(context.Context, *pb.ProcessStripeWebhookRequest) (*pb.ProcessStripeWebhookResponse, error)
	// Process Twilio SMS webhook event (forwarded from tenant proxy)
	ProcessTwilioWebhook(context.Context, *pb.ProcessTwilioWebhookRequest) (*pb.ProcessTwilioWebhookResponse, error)
}

type TenantBillingService interface {
	// Get current subscription status
	GetSubscriptionStatus(context.Context, *pb.GetSubscriptionStatusRequest) (*pb.GetSubscriptionStatusResponse, error)
	// Change subscription tier
	ChangeTier(context.Context, *pb.ChangeTierRequest) (*pb.ChangeTierResponse, error)
	// Get billing portal URL (Stripe customer portal)
	GetBillingPortal(context.Context, *pb.GetBillingPortalRequest) (*pb.GetBillingPortalResponse, error)
	// Cancel subscription
	CancelSubscription(context.Context, *pb.CancelSubscriptionRequest) (*pb.CancelSubscriptionResponse, error)
	// Get billing history
	GetBillingHistory(context.Context, *pb.GetBillingHistoryRequest) (*pb.GetBillingHistoryResponse, error)
	// Create SetupIntent for adding payment method (authenticated)
	CreateSetupIntent(context.Context, *pb.CreateBillingSetupIntentRequest) (*pb.CreateBillingSetupIntentResponse, error)
	// Contact Sales for Enterprise tier
	ContactSales(context.Context, *pb.ContactSalesRequest) (*pb.ContactSalesResponse, error)
}

type TenantDataService interface {
	// Request database backup download
	RequestBackup(context.Context, *pb.RequestBackupRequest) (*pb.RequestBackupResponse, error)
	// List all backups for tenant
	ListBackups(context.Context, *pb.ListBackupsRequest) (*pb.ListBackupsResponse, error)
	// Get backup download URL
	GetBackupDownloadURL(context.Context, *pb.GetBackupDownloadURLRequest) (*pb.GetBackupDownloadURLResponse, error)
	// Delete a backup by ID
	DeleteBackup(context.Context, *pb.DeleteBackupRequest) (*pb.DeleteBackupResponse, error)
	// Restore tenant data from an existing backup by ID
	RestoreBackup(context.Context, *pb.RestoreBackupRequest) (*pb.RestoreBackupResponse, error)
	// Initiate database restore from backup upload
	RestoreFromBackup(context.Context, *pb.RestoreFromBackupRequest) (*pb.RestoreFromBackupResponse, error)
	// Get restore status
	GetRestoreStatus(context.Context, *pb.GetRestoreStatusRequest) (*pb.GetRestoreStatusResponse, error)
}

type TenantPortalService interface {
	// Tenant login
	TenantLogin(context.Context, *pb.TenantLoginRequest) (*pb.TenantLoginResponse, error)
	// Verify CAPTCHA answer for login (called before completing login if CAPTCHA required)
	VerifyCaptcha(context.Context, *pb.CaptchaVerifyRequest) (*pb.CaptchaVerifyResponse, error)
	// Tenant logout (invalidate session)
	TenantLogout(context.Context, *pb.TenantLogoutRequest) (*pb.TenantLogoutResponse, error)
	// Get tenant dashboard data
	GetTenantDashboard(context.Context, *pb.GetTenantDashboardRequest) (*pb.GetTenantDashboardResponse, error)
	// Update tenant profile
	UpdateTenantProfile(context.Context, *pb.UpdateTenantProfileRequest) (*pb.UpdateTenantProfileResponse, error)
	// Delete tenant account (soft delete - sets status to "deleted")
	DeleteTenantAccount(context.Context, *pb.DeleteTenantAccountRequest) (*pb.DeleteTenantAccountResponse, error)
	// Get tenant's own account info
	GetTenantAccount(context.Context, *pb.GetTenantAccountRequest) (*pb.GetTenantAccountResponse, error)
	// 2FA Management
	GetTwoFASettings(context.Context, *pb.GetTwoFASettingsRequest) (*pb.GetTwoFASettingsResponse, error)
	SetTwoFAMethod(context.Context, *pb.SetTwoFAMethodRequest) (*pb.SetTwoFAMethodResponse, error)
	Send2FACode(context.Context, *pb.Send2FACodeRequest) (*pb.Send2FACodeResponse, error)
	// Change Password (authenticated)
	ChangePassword(context.Context, *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error)
	// Security Alert Handler (for "It wasn't me" flow)
	HandleSecurityAlert(context.Context, *pb.HandleSecurityAlertRequest) (*pb.HandleSecurityAlertResponse, error)
	// Password Recovery (public - no auth required)
	RequestPasswordReset(context.Context, *pb.RequestPasswordResetRequest) (*pb.RequestPasswordResetResponse, error)
	VerifyResetCode(context.Context, *pb.VerifyResetCodeRequest) (*pb.VerifyResetCodeResponse, error)
	ResetPassword(context.Context, *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error)
	// List tenant's own peers
	ListTenantPeers(context.Context, *pb.ListTenantPeersRequest) (*pb.ListTenantPeersResponse, error)
	// Add a new peer to tenant's network
	AddTenantPeer(context.Context, *pb.AddTenantPeerRequest) (*pb.AddTenantPeerResponse, error)
	// Remove a peer from tenant's network
	RemoveTenantPeer(context.Context, *pb.RemoveTenantPeerRequest) (*pb.RemoveTenantPeerResponse, error)
	// Update peer settings
	UpdateTenantPeer(context.Context, *pb.UpdateTenantPeerRequest) (*pb.UpdateTenantPeerResponse, error)
	// Batch update peer settings
	BatchUpdateTenantPeers(context.Context, *pb.BatchUpdatePeersRequest) (*pb.BatchUpdatePeersResponse, error)
	// Get peer configuration (WireGuard config + QR code)
	GetTenantPeerConfig(context.Context, *pb.GetTenantPeerConfigRequest) (*pb.GetTenantPeerConfigResponse, error)
	// Get tenant's network topology
	GetTenantTopology(context.Context, *pb.GetTenantTopologyRequest) (*pb.GetTenantTopologyResponse, error)
	// Assign an exit node for a peer (P2P mode)
	AssignExitNode(context.Context, *pb.AssignExitNodeRequest) (*pb.AssignExitNodeResponse, error)
	// Get single peer details
	GetTenantPeer(context.Context, *pb.GetTenantPeerRequest) (*pb.GetTenantPeerResponse, error)
	// Get peer statistics
	GetTenantPeerStats(context.Context, *pb.GetTenantPeerStatsRequest) (*pb.GetTenantPeerStatsResponse, error)
	// Ping a peer
	PingTenantPeer(context.Context, *pb.PingTenantPeerRequest) (*pb.PingTenantPeerResponse, error)
	// StreamPingTenantPeer sends ICMP pings and streams each result in real-time.
	StreamPingTenantPeer(*pb.PingTenantPeerRequest, ServerStream[*pb.PingEvent]) error
	// Set peer notification settings (enable/disable offline alerts)
	SetPeerNotification(context.Context, *pb.SetPeerNotificationRequest) (*pb.SetPeerNotificationResponse, error)
	// Disable all peer notifications for a tenant (used by unsubscribe links)
	DisableAllPeerNotifications(context.Context, *pb.DisableAllPeerNotificationsRequest) (*pb.DisableAllPeerNotificationsResponse, error)
	// List enrollment tokens for automated device registration
	ListEnrollmentTokens(context.Context, *pb.ListEnrollmentTokensRequest) (*pb.ListEnrollmentTokensResponse, error)
	// Create a new enrollment token
	CreateEnrollmentToken(context.Context, *pb.CreateEnrollmentTokenRequest) (*pb.CreateEnrollmentTokenResponse, error)
	// Delete an enrollment token
	DeleteEnrollmentToken(context.Context, *pb.DeleteEnrollmentTokenRequest) (*pb.DeleteEnrollmentTokenResponse, error)
	// Device Authorization Flow (for wantasticd interactive login)
	ConfirmDevice(context.Context, *pb.ConfirmDeviceRequest) (*pb.ConfirmDeviceResponse, error)
	// Clear Winbox credentials
	ClearTenantWinboxCredentials(context.Context, *pb.ClearTenantWinboxCredentialsRequest) (*pb.ClearTenantWinboxCredentialsResponse, error)
	// Create Winbox session
	CreateTenantWinboxSession(context.Context, *pb.CreateTenantWinboxSessionRequest) (*pb.CreateTenantWinboxSessionResponse, error)
	// Update Winbox session
	UpdateTenantWinboxSession(context.Context, *pb.UpdateTenantWinboxSessionRequest) (*pb.UpdateTenantWinboxSessionResponse, error)
	// Delete Winbox session
	DeleteTenantWinboxSession(context.Context, *pb.DeleteTenantWinboxSessionRequest) (*pb.DeleteTenantWinboxSessionResponse, error)
	// List Winbox sessions
	ListTenantWinboxSessions(context.Context, *pb.ListTenantWinboxSessionsRequest) (*pb.ListTenantWinboxSessionsResponse, error)
	// Get Winbox session
	GetTenantWinboxSession(context.Context, *pb.GetTenantWinboxSessionRequest) (*pb.GetTenantWinboxSessionResponse, error)
	// ========== TENANT ACL MANAGEMENT ==========
	// Peer Groups
	CreateTenantPeerGroup(context.Context, *pb.CreateTenantPeerGroupRequest) (*pb.CreateTenantPeerGroupResponse, error)
	DeleteTenantPeerGroup(context.Context, *pb.DeleteTenantPeerGroupRequest) (*pb.DeleteTenantPeerGroupResponse, error)
	ListTenantPeerGroups(context.Context, *pb.ListTenantPeerGroupsRequest) (*pb.ListTenantPeerGroupsResponse, error)
	AddTenantPeerToGroup(context.Context, *pb.AddTenantPeerToGroupRequest) (*pb.AddTenantPeerToGroupResponse, error)
	RemoveTenantPeerFromGroup(context.Context, *pb.RemoveTenantPeerFromGroupRequest) (*pb.RemoveTenantPeerFromGroupResponse, error)
	// Group Links
	CreateTenantGroupLink(context.Context, *pb.CreateTenantGroupLinkRequest) (*pb.CreateTenantGroupLinkResponse, error)
	DeleteTenantGroupLink(context.Context, *pb.DeleteTenantGroupLinkRequest) (*pb.DeleteTenantGroupLinkResponse, error)
	ListTenantGroupLinks(context.Context, *pb.ListTenantGroupLinksRequest) (*pb.ListTenantGroupLinksResponse, error)
	// ACL Compilation
	CompileTenantGroups(context.Context, *pb.CompileTenantGroupsRequest) (*pb.CompileTenantGroupsResponse, error)
	GetTenantCompilationStats(context.Context, *pb.GetTenantCompilationStatsRequest) (*pb.GetTenantCompilationStatsResponse, error)
	// ACL Rules
	AddTenantACLRule(context.Context, *pb.AddTenantACLRuleRequest) (*pb.AddTenantACLRuleResponse, error)
	RemoveTenantACLRule(context.Context, *pb.RemoveTenantACLRuleRequest) (*pb.RemoveTenantACLRuleResponse, error)
	GetTenantACLRules(context.Context, *pb.GetTenantACLRulesRequest) (*pb.GetTenantACLRulesResponse, error)
	CheckTenantAccess(context.Context, *pb.CheckTenantAccessRequest) (*pb.CheckTenantAccessResponse, error)
	// ========== TENANT WEBSSH MANAGEMENT ==========
	CreateTenantWebSSHSession(context.Context, *pb.CreateTenantWebSSHSessionRequest) (*pb.CreateTenantWebSSHSessionResponse, error)
	GetTenantWebSSHSession(context.Context, *pb.GetTenantWebSSHSessionRequest) (*pb.GetTenantWebSSHSessionResponse, error)
	ListTenantWebSSHSessions(context.Context, *pb.ListTenantWebSSHSessionsRequest) (*pb.ListTenantWebSSHSessionsResponse, error)
	DisconnectTenantWebSSHSession(context.Context, *pb.DisconnectTenantWebSSHSessionRequest) (*pb.DisconnectTenantWebSSHSessionResponse, error)
	// ========== CONFIGURATION ==========
	// Get centralized endpoint configuration
	GetEndpointsConfig(context.Context, *pb.GetEndpointsConfigRequest) (*pb.GetEndpointsConfigResponse, error)
	// ========== SESSION MANAGEMENT ==========
	// List all active sessions for the tenant
	ListTenantSessions(context.Context, *pb.ListTenantSessionsRequest) (*pb.ListTenantSessionsResponse, error)
	// Delete a specific session (logout from that device)
	DeleteTenantSession(context.Context, *pb.DeleteTenantSessionRequest) (*pb.DeleteTenantSessionResponse, error)
	// Batch Operations
	BatchUpdatePeers(context.Context, *pb.BatchUpdatePeersRequest) (*pb.BatchUpdatePeersResponse, error)
	// ========== TEAM / ACCESS SHARING ==========
	// List teammates invited by the current tenant (as owner)
	ListAccessShares(context.Context, *pb.ListAccessSharesRequest) (*pb.ListAccessSharesResponse, error)
	// Invite a teammate with optional tag filter and permissions
	CreateAccessShare(context.Context, *pb.CreateAccessShareRequest) (*pb.CreateAccessShareResponse, error)
	// Revoke a teammate's access (owner only)
	DeleteAccessShare(context.Context, *pb.DeleteAccessShareRequest) (*pb.DeleteAccessShareResponse, error)
	// Resend invite email (rate limited: once per 30 min)
	ResendAccessShareInvite(context.Context, *pb.ResendAccessShareInviteRequest) (*pb.ResendAccessShareInviteResponse, error)
	// List pending invites for the current tenant's email (as invitee)
	GetPendingShares(context.Context, *pb.GetPendingSharesRequest) (*pb.GetPendingSharesResponse, error)
	// Accept an invite (invitee)
	AcceptAccessShare(context.Context, *pb.AcceptAccessShareRequest) (*pb.AcceptAccessShareResponse, error)
	// Reject/decline an invite (invitee)
	RejectAccessShare(context.Context, *pb.RejectAccessShareRequest) (*pb.RejectAccessShareResponse, error)
	// List accounts the current tenant has been given access to (accepted shares)
	ListAccessibleAccounts(context.Context, *pb.ListAccessibleAccountsRequest) (*pb.ListAccessibleAccountsResponse, error)
	// Get invite details by token (public — no auth required)
	GetAccessShareByToken(context.Context, *pb.GetAccessShareByTokenRequest) (*pb.GetAccessShareByTokenResponse, error)
}

type WUSPServiceHandler interface {
	// GetDeviceState returns the last persisted device model snapshot for a peer.
	GetDeviceState(context.Context, *pb.GetWUSPDeviceStateRequest) (*pb.GetWUSPDeviceStateResponse, error)
	// ListDeviceStates lists all device state snapshots for a tenant.
	ListDeviceStates(context.Context, *pb.ListWUSPDeviceStatesRequest) (*pb.ListWUSPDeviceStatesResponse, error)
	// SyncDeviceState triggers a live Get round-trip to the peer and persists the result.
	SyncDeviceState(context.Context, *pb.SyncWUSPDeviceStateRequest) (*pb.SyncWUSPDeviceStateResponse, error)
	// SendGet sends a USP Get to the peer and returns the parameter values.
	SendGet(context.Context, *pb.WUSPGetRequest) (*pb.WUSPGetResponse, error)
	// SendSet sends a USP Set to the peer and returns success/error.
	SendSet(context.Context, *pb.WUSPSetRequest) (*pb.WUSPSetResponse, error)
	// SendOperate sends a USP Operate command to the peer.
	SendOperate(context.Context, *pb.WUSPOperateRequest) (*pb.WUSPOperateResponse, error)
	// SendAdd creates a new object instance on the peer device.
	SendAdd(context.Context, *pb.WUSPAddRequest) (*pb.WUSPAddResponse, error)
	// SendDelete removes object instances or parameters from the peer device.
	SendDelete(context.Context, *pb.WUSPDeleteRequest) (*pb.WUSPDeleteResponse, error)
	// GetSupportedProtocol queries the peer's WUSP transport capabilities.
	GetSupportedProtocol(context.Context, *pb.WUSPSupportedProtocolRequest) (*pb.WUSPSupportedProtocolResponse, error)
	// GetSupportedDM queries the peer's supported TR-181 data model tree.
	GetSupportedDM(context.Context, *pb.WUSPSupportedDMRequest) (*pb.WUSPSupportedDMResponse, error)
	// CreateSnapshot saves the current live state of a peer as a named snapshot.
	CreateSnapshot(context.Context, *pb.CreateDeviceSnapshotRequest) (*pb.CreateDeviceSnapshotResponse, error)
	// ListSnapshots returns all snapshots for the tenant (optionally filtered by protocol).
	ListSnapshots(context.Context, *pb.ListDeviceSnapshotsRequest) (*pb.ListDeviceSnapshotsResponse, error)
	// GetSnapshot returns a single snapshot by ID.
	GetSnapshot(context.Context, *pb.GetDeviceSnapshotRequest) (*pb.GetDeviceSnapshotResponse, error)
	// UpdateSnapshot updates the name or snapshot data of an existing snapshot.
	UpdateSnapshot(context.Context, *pb.UpdateDeviceSnapshotRequest) (*pb.UpdateDeviceSnapshotResponse, error)
	// DeleteSnapshot removes a snapshot.
	DeleteSnapshot(context.Context, *pb.DeleteDeviceSnapshotRequest) (*pb.DeleteDeviceSnapshotResponse, error)
	// ProvisionDevice applies a snapshot's parameters to a live peer.
	ProvisionDevice(context.Context, *pb.ProvisionDeviceRequest) (*pb.ProvisionDeviceResponse, error)
	// GetSnapshotBackup returns the binary backup file attached to a snapshot.
	GetSnapshotBackup(context.Context, *pb.GetSnapshotBackupRequest) (*pb.GetSnapshotBackupResponse, error)
	// UploadSnapshotBackup stores a binary backup file via upload token.
	UploadSnapshotBackup(context.Context, *pb.UploadSnapshotBackupRequest) (*pb.UploadSnapshotBackupResponse, error)
	// GenerateUploadToken creates a rotating upload token for a snapshot.
	GenerateUploadToken(context.Context, *pb.GenerateUploadTokenRequest) (*pb.GenerateUploadTokenResponse, error)
	// GenerateBackupToken creates a one-time backup upload token for a peer.
	// Called when the Device Config dialog opens. Token is invalidated after use.
	GenerateBackupToken(context.Context, *pb.GenerateBackupTokenRequest) (*pb.GenerateBackupTokenResponse, error)
	// EnsureWUSPSubscription registers (or refreshes) the canonical dashboard
	// Subscribe on the agent so it pushes ValueChange / OperationComplete /
	// ObjectCreation / ObjectDeletion Notify events back. Idempotent — calling
	// it twice in a row replaces the existing registration in place. Called by
	// the WS proxy when the first dashboard session subscribes to a peer's
	// live feed.
	EnsureWUSPSubscription(context.Context, *pb.EnsureWUSPSubscriptionRequest) (*pb.EnsureWUSPSubscriptionResponse, error)
	// CancelWUSPSubscription removes the canonical dashboard subscription.
	// Called by the WS proxy after the last dashboard session for a peer goes
	// away (debounced ~30 s to absorb tab-switching).
	CancelWUSPSubscription(context.Context, *pb.CancelWUSPSubscriptionRequest) (*pb.CancelWUSPSubscriptionResponse, error)
}

type RouterOSServiceHandler interface {
	// StreamDashboard opens a long-lived RouterOS dashboard session so the portal
	// can drive the device over one websocket->gRPC data plane instead of
	// scattering unary refresh calls across the UI.
	StreamDashboard(BidiStream[*pb.StreamRouterOSDashboardRequest, *pb.StreamRouterOSDashboardEvent]) error
	// GetOverview returns the verified RouterOS capability + system identity for a peer.
	GetOverview(context.Context, *pb.GetRouterOSOverviewRequest) (*pb.GetRouterOSOverviewResponse, error)
	// ConfigureAccess verifies and persists RouterOS API credentials for a peer.
	ConfigureAccess(context.Context, *pb.ConfigureRouterOSAccessRequest) (*pb.ConfigureRouterOSAccessResponse, error)
	// ListResource prints one RouterOS resource area.
	ListResource(context.Context, *pb.ListRouterOSResourceRequest) (*pb.ListRouterOSResourceResponse, error)
	// AddResource creates a new RouterOS record for resources that support add.
	AddResource(context.Context, *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)
	// UpdateResource updates an existing RouterOS record by .id.
	UpdateResource(context.Context, *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)
	// DeleteResource removes a RouterOS record by .id.
	DeleteResource(context.Context, *pb.DeleteRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)
}

