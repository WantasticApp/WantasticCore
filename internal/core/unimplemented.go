// Code generated. DO NOT EDIT manually.
// Provides forward-compatible default implementations for each Service
// interface. Tests embed these helpers to mock a single method without
// having to stub every other method on the interface.

package core

import (
	"context"

	pb "WantasticCore/internal/types"
	"WantasticCore/internal/errs"
)

var _ context.Context
var _ = pb.CreateAccountRequest{}
var _ = errs.UnimplementedE

// UnimplementedAccountService returns errs.UnimplementedE from every method of AccountService.
type UnimplementedAccountService struct{}

func (UnimplementedAccountService) CreateAccount(context.Context, *pb.CreateAccountRequest) (*pb.CreateAccountResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.CreateAccount")
}

func (UnimplementedAccountService) GetAccount(context.Context, *pb.GetAccountRequest) (*pb.GetAccountResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.GetAccount")
}

func (UnimplementedAccountService) ListAccounts(context.Context, *pb.ListAccountsRequest) (*pb.ListAccountsResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.ListAccounts")
}

func (UnimplementedAccountService) DeleteAccount(context.Context, *pb.DeleteAccountRequest) (*pb.DeleteAccountResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.DeleteAccount")
}

func (UnimplementedAccountService) UpdateAccountQuotas(context.Context, *pb.UpdateAccountQuotasRequest) (*pb.UpdateAccountQuotasResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.UpdateAccountQuotas")
}

func (UnimplementedAccountService) UpdateAccountTier(context.Context, *pb.UpdateAccountTierRequest) (*pb.UpdateAccountTierResponse, error)  {
	return nil, errs.UnimplementedE("AccountService.UpdateAccountTier")
}

// UnimplementedNetworkService returns errs.UnimplementedE from every method of NetworkService.
type UnimplementedNetworkService struct{}

func (UnimplementedNetworkService) GetNetwork(context.Context, *pb.GetNetworkRequest) (*pb.GetNetworkResponse, error)  {
	return nil, errs.UnimplementedE("NetworkService.GetNetwork")
}

func (UnimplementedNetworkService) GetNetworkStats(context.Context, *pb.GetNetworkStatsRequest) (*pb.GetNetworkStatsResponse, error)  {
	return nil, errs.UnimplementedE("NetworkService.GetNetworkStats")
}

func (UnimplementedNetworkService) GetAccountIPStatistics(context.Context, *pb.GetAccountIPStatisticsRequest) (*pb.GetAccountIPStatisticsResponse, error)  {
	return nil, errs.UnimplementedE("NetworkService.GetAccountIPStatistics")
}

// UnimplementedPeerService returns errs.UnimplementedE from every method of PeerService.
type UnimplementedPeerService struct{}

func (UnimplementedPeerService) AddPeer(context.Context, *pb.AddPeerRequest) (*pb.AddPeerResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.AddPeer")
}

func (UnimplementedPeerService) GetPeer(context.Context, *pb.GetPeerRequest) (*pb.GetPeerResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.GetPeer")
}

func (UnimplementedPeerService) ListPeers(context.Context, *pb.ListPeersRequest) (*pb.ListPeersResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.ListPeers")
}

func (UnimplementedPeerService) UpdatePeerNotes(context.Context, *pb.UpdatePeerNotesRequest) (*pb.UpdatePeerNotesResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.UpdatePeerNotes")
}

func (UnimplementedPeerService) RemovePeer(context.Context, *pb.RemovePeerRequest) (*pb.RemovePeerResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.RemovePeer")
}

func (UnimplementedPeerService) GetPeerConfig(context.Context, *pb.GetPeerConfigRequest) (*pb.GetPeerConfigResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.GetPeerConfig")
}

func (UnimplementedPeerService) GetPeerStats(context.Context, *pb.GetPeerStatsRequest) (*pb.GetPeerStatsResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.GetPeerStats")
}

func (UnimplementedPeerService) PingPeer(context.Context, *pb.PingPeerRequest) (*pb.PingPeerResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.PingPeer")
}

func (UnimplementedPeerService) StreamPing(*pb.StreamPingRequest, ServerStream[*pb.PingEvent]) error {
	return errs.UnimplementedE("PeerService.StreamPing")
}

func (UnimplementedPeerService) SetWinboxCredentials(context.Context, *pb.SetWinboxCredentialsRequest) (*pb.SetWinboxCredentialsResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.SetWinboxCredentials")
}

func (UnimplementedPeerService) GetWinboxStatus(context.Context, *pb.GetWinboxStatusRequest) (*pb.GetWinboxStatusResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.GetWinboxStatus")
}

func (UnimplementedPeerService) ClearWinboxCredentials(context.Context, *pb.ClearWinboxCredentialsRequest) (*pb.ClearWinboxCredentialsResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.ClearWinboxCredentials")
}

func (UnimplementedPeerService) CreateWinboxSession(context.Context, *pb.CreateWinboxSessionRequest) (*pb.CreateWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.CreateWinboxSession")
}

func (UnimplementedPeerService) UpdateWinboxSession(context.Context, *pb.UpdateWinboxSessionRequest) (*pb.UpdateWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.UpdateWinboxSession")
}

func (UnimplementedPeerService) DeleteWinboxSession(context.Context, *pb.DeleteWinboxSessionRequest) (*pb.DeleteWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.DeleteWinboxSession")
}

func (UnimplementedPeerService) ListWinboxSessions(context.Context, *pb.ListWinboxSessionsRequest) (*pb.ListWinboxSessionsResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.ListWinboxSessions")
}

func (UnimplementedPeerService) GetWinboxSession(context.Context, *pb.GetWinboxSessionRequest) (*pb.GetWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.GetWinboxSession")
}

func (UnimplementedPeerService) StartPortScan(context.Context, *pb.StartPortScanRequest) (*pb.StartPortScanResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.StartPortScan")
}

func (UnimplementedPeerService) StopPortScan(context.Context, *pb.StopPortScanRequest) (*pb.StopPortScanResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.StopPortScan")
}

func (UnimplementedPeerService) PausePortScan(context.Context, *pb.PausePortScanRequest) (*pb.PausePortScanResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.PausePortScan")
}

func (UnimplementedPeerService) ResumePortScan(context.Context, *pb.ResumePortScanRequest) (*pb.ResumePortScanResponse, error)  {
	return nil, errs.UnimplementedE("PeerService.ResumePortScan")
}

func (UnimplementedPeerService) StreamPortScanStatus(*pb.StreamPortScanStatusRequest, ServerStream[*pb.PortScanStatusUpdate]) error {
	return errs.UnimplementedE("PeerService.StreamPortScanStatus")
}

// UnimplementedAdminService returns errs.UnimplementedE from every method of AdminService.
type UnimplementedAdminService struct{}

func (UnimplementedAdminService) GetGlobalStats(context.Context, *pb.GetGlobalStatsRequest) (*pb.GetGlobalStatsResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.GetGlobalStats")
}

func (UnimplementedAdminService) HealthCheck(context.Context, *pb.HealthCheckRequest) (*pb.HealthCheckResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.HealthCheck")
}

func (UnimplementedAdminService) GetTopology(context.Context, *pb.GetTopologyRequest) (*pb.GetTopologyResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.GetTopology")
}

func (UnimplementedAdminService) CheckAdminSetup(context.Context, *pb.CheckAdminSetupRequest) (*pb.CheckAdminSetupResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.CheckAdminSetup")
}

func (UnimplementedAdminService) CreateFirstAdmin(context.Context, *pb.CreateFirstAdminRequest) (*pb.CreateFirstAdminResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.CreateFirstAdmin")
}

func (UnimplementedAdminService) AdminLogin(context.Context, *pb.AdminLoginRequest) (*pb.AdminLoginResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.AdminLogin")
}

func (UnimplementedAdminService) ValidateAdminSession(context.Context, *pb.ValidateAdminSessionRequest) (*pb.ValidateAdminSessionResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.ValidateAdminSession")
}

func (UnimplementedAdminService) AdminLogout(context.Context, *pb.AdminLogoutRequest) (*pb.AdminLogoutResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.AdminLogout")
}

func (UnimplementedAdminService) GetAdminProfile(context.Context, *pb.GetAdminProfileRequest) (*pb.GetAdminProfileResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.GetAdminProfile")
}

func (UnimplementedAdminService) UpdateAdminSettings(context.Context, *pb.UpdateAdminSettingsRequest) (*pb.UpdateAdminSettingsResponse, error)  {
	return nil, errs.UnimplementedE("AdminService.UpdateAdminSettings")
}

// UnimplementedAuthService returns errs.UnimplementedE from every method of AuthService.
type UnimplementedAuthService struct{}

func (UnimplementedAuthService) ValidateSession(context.Context, *pb.ValidateSessionRequest) (*pb.ValidateSessionResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.ValidateSession")
}

func (UnimplementedAuthService) RegisterDevice(context.Context, *pb.RegisterDeviceRequest) (*pb.RegisterDeviceResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.RegisterDevice")
}

func (UnimplementedAuthService) RefreshToken(context.Context, *pb.DeviceRefreshTokenRequest) (*pb.DeviceRefreshTokenResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.RefreshToken")
}

func (UnimplementedAuthService) GetConfiguration(context.Context, *pb.GetDeviceConfigurationRequest) (*pb.GetDeviceConfigurationResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.GetConfiguration")
}

func (UnimplementedAuthService) StartDeviceFlow(context.Context, *pb.StartDeviceFlowRequest) (*pb.StartDeviceFlowResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.StartDeviceFlow")
}

func (UnimplementedAuthService) PollDeviceFlow(context.Context, *pb.PollDeviceFlowRequest) (*pb.PollDeviceFlowResponse, error)  {
	return nil, errs.UnimplementedE("AuthService.PollDeviceFlow")
}

// UnimplementedWebSSHService returns errs.UnimplementedE from every method of WebSSHService.
type UnimplementedWebSSHService struct{}

func (UnimplementedWebSSHService) CreateWebSSHSession(context.Context, *pb.CreateWebSSHSessionRequest) (*pb.CreateWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("WebSSHService.CreateWebSSHSession")
}

func (UnimplementedWebSSHService) StreamSSH(BidiStream[*pb.SSHStreamMessage, *pb.SSHStreamMessage]) error {
	return errs.UnimplementedE("WebSSHService.StreamSSH")
}

func (UnimplementedWebSSHService) GetWebSSHSession(context.Context, *pb.GetWebSSHSessionRequest) (*pb.GetWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("WebSSHService.GetWebSSHSession")
}

func (UnimplementedWebSSHService) ListWebSSHSessions(context.Context, *pb.ListWebSSHSessionsRequest) (*pb.ListWebSSHSessionsResponse, error)  {
	return nil, errs.UnimplementedE("WebSSHService.ListWebSSHSessions")
}

func (UnimplementedWebSSHService) DisconnectWebSSHSession(context.Context, *pb.DisconnectWebSSHSessionRequest) (*pb.DisconnectWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("WebSSHService.DisconnectWebSSHSession")
}

// UnimplementedWebProxyService returns errs.UnimplementedE from every method of WebProxyService.
type UnimplementedWebProxyService struct{}

func (UnimplementedWebProxyService) CreateWebProxySession(context.Context, *pb.CreateWebProxySessionRequest) (*pb.CreateWebProxySessionResponse, error)  {
	return nil, errs.UnimplementedE("WebProxyService.CreateWebProxySession")
}

func (UnimplementedWebProxyService) StreamHTTP(BidiStream[*pb.WebProxyStreamMessage, *pb.WebProxyStreamMessage]) error {
	return errs.UnimplementedE("WebProxyService.StreamHTTP")
}

func (UnimplementedWebProxyService) GetWebProxySession(context.Context, *pb.GetWebProxySessionRequest) (*pb.GetWebProxySessionResponse, error)  {
	return nil, errs.UnimplementedE("WebProxyService.GetWebProxySession")
}

func (UnimplementedWebProxyService) ListWebProxySessions(context.Context, *pb.ListWebProxySessionsRequest) (*pb.ListWebProxySessionsResponse, error)  {
	return nil, errs.UnimplementedE("WebProxyService.ListWebProxySessions")
}

func (UnimplementedWebProxyService) CloseWebProxySession(context.Context, *pb.CloseWebProxySessionRequest) (*pb.CloseWebProxySessionResponse, error)  {
	return nil, errs.UnimplementedE("WebProxyService.CloseWebProxySession")
}

// UnimplementedACLService returns errs.UnimplementedE from every method of ACLService.
type UnimplementedACLService struct{}

func (UnimplementedACLService) CreatePeerGroup(context.Context, *pb.CreatePeerGroupRequest) (*pb.CreatePeerGroupResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.CreatePeerGroup")
}

func (UnimplementedACLService) DeletePeerGroup(context.Context, *pb.DeletePeerGroupRequest) (*pb.DeletePeerGroupResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.DeletePeerGroup")
}

func (UnimplementedACLService) ListPeerGroups(context.Context, *pb.ListPeerGroupsRequest) (*pb.ListPeerGroupsResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.ListPeerGroups")
}

func (UnimplementedACLService) AddPeerToGroup(context.Context, *pb.AddPeerToGroupRequest) (*pb.AddPeerToGroupResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.AddPeerToGroup")
}

func (UnimplementedACLService) RemovePeerFromGroup(context.Context, *pb.RemovePeerFromGroupRequest) (*pb.RemovePeerFromGroupResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.RemovePeerFromGroup")
}

func (UnimplementedACLService) CreateGroupLink(context.Context, *pb.CreateGroupLinkRequest) (*pb.CreateGroupLinkResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.CreateGroupLink")
}

func (UnimplementedACLService) DeleteGroupLink(context.Context, *pb.DeleteGroupLinkRequest) (*pb.DeleteGroupLinkResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.DeleteGroupLink")
}

func (UnimplementedACLService) ListGroupLinks(context.Context, *pb.ListGroupLinksRequest) (*pb.ListGroupLinksResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.ListGroupLinks")
}

func (UnimplementedACLService) CompileGroups(context.Context, *pb.CompileGroupsRequest) (*pb.CompileGroupsResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.CompileGroups")
}

func (UnimplementedACLService) GetCompilationStats(context.Context, *pb.GetCompilationStatsRequest) (*pb.GetCompilationStatsResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.GetCompilationStats")
}

func (UnimplementedACLService) AddACLRule(context.Context, *pb.AddACLRuleRequest) (*pb.AddACLRuleResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.AddACLRule")
}

func (UnimplementedACLService) RemoveACLRule(context.Context, *pb.RemoveACLRuleRequest) (*pb.RemoveACLRuleResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.RemoveACLRule")
}

func (UnimplementedACLService) GetACLRules(context.Context, *pb.GetACLRulesRequest) (*pb.GetACLRulesResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.GetACLRules")
}

func (UnimplementedACLService) CheckAccess(context.Context, *pb.CheckAccessRequest) (*pb.CheckAccessResponse, error)  {
	return nil, errs.UnimplementedE("ACLService.CheckAccess")
}

// UnimplementedTenantRegistrationService returns errs.UnimplementedE from every method of TenantRegistrationService.
type UnimplementedTenantRegistrationService struct{}

func (UnimplementedTenantRegistrationService) GetPaymentStatus(context.Context, *pb.GetPaymentStatusRequest) (*pb.GetPaymentStatusResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.GetPaymentStatus")
}

func (UnimplementedTenantRegistrationService) GetAvailablePlans(context.Context, *pb.GetAvailablePlansRequest) (*pb.GetAvailablePlansResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.GetAvailablePlans")
}

func (UnimplementedTenantRegistrationService) GetAllowedPhoneRegions(context.Context, *pb.GetAllowedPhoneRegionsRequest) (*pb.GetAllowedPhoneRegionsResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.GetAllowedPhoneRegions")
}

func (UnimplementedTenantRegistrationService) StartRegistration(context.Context, *pb.StartRegistrationRequest) (*pb.StartRegistrationResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.StartRegistration")
}

func (UnimplementedTenantRegistrationService) VerifyCaptcha(context.Context, *pb.CaptchaVerifyRequest) (*pb.CaptchaVerifyResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.VerifyCaptcha")
}

func (UnimplementedTenantRegistrationService) VerifyPhone(context.Context, *pb.VerifyPhoneRequest) (*pb.VerifyPhoneResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.VerifyPhone")
}

func (UnimplementedTenantRegistrationService) CompleteRegistration(context.Context, *pb.CompleteRegistrationRequest) (*pb.CompleteRegistrationResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.CompleteRegistration")
}

func (UnimplementedTenantRegistrationService) CreateCheckoutSession(context.Context, *pb.CreateCheckoutSessionRequest) (*pb.CreateCheckoutSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.CreateCheckoutSession")
}

func (UnimplementedTenantRegistrationService) CreateSetupIntent(context.Context, *pb.CreateSetupIntentRequest) (*pb.CreateSetupIntentResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.CreateSetupIntent")
}

func (UnimplementedTenantRegistrationService) GetRegistrationStatus(context.Context, *pb.GetRegistrationStatusRequest) (*pb.GetRegistrationStatusResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.GetRegistrationStatus")
}

func (UnimplementedTenantRegistrationService) ResendPhoneVerification(context.Context, *pb.ResendPhoneVerificationRequest) (*pb.ResendPhoneVerificationResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.ResendPhoneVerification")
}

func (UnimplementedTenantRegistrationService) ProcessStripeWebhook(context.Context, *pb.ProcessStripeWebhookRequest) (*pb.ProcessStripeWebhookResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.ProcessStripeWebhook")
}

func (UnimplementedTenantRegistrationService) ProcessTwilioWebhook(context.Context, *pb.ProcessTwilioWebhookRequest) (*pb.ProcessTwilioWebhookResponse, error)  {
	return nil, errs.UnimplementedE("TenantRegistrationService.ProcessTwilioWebhook")
}

// UnimplementedTenantBillingService returns errs.UnimplementedE from every method of TenantBillingService.
type UnimplementedTenantBillingService struct{}

func (UnimplementedTenantBillingService) GetSubscriptionStatus(context.Context, *pb.GetSubscriptionStatusRequest) (*pb.GetSubscriptionStatusResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.GetSubscriptionStatus")
}

func (UnimplementedTenantBillingService) ChangeTier(context.Context, *pb.ChangeTierRequest) (*pb.ChangeTierResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.ChangeTier")
}

func (UnimplementedTenantBillingService) GetBillingPortal(context.Context, *pb.GetBillingPortalRequest) (*pb.GetBillingPortalResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.GetBillingPortal")
}

func (UnimplementedTenantBillingService) CancelSubscription(context.Context, *pb.CancelSubscriptionRequest) (*pb.CancelSubscriptionResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.CancelSubscription")
}

func (UnimplementedTenantBillingService) GetBillingHistory(context.Context, *pb.GetBillingHistoryRequest) (*pb.GetBillingHistoryResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.GetBillingHistory")
}

func (UnimplementedTenantBillingService) CreateSetupIntent(context.Context, *pb.CreateBillingSetupIntentRequest) (*pb.CreateBillingSetupIntentResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.CreateSetupIntent")
}

func (UnimplementedTenantBillingService) ContactSales(context.Context, *pb.ContactSalesRequest) (*pb.ContactSalesResponse, error)  {
	return nil, errs.UnimplementedE("TenantBillingService.ContactSales")
}

// UnimplementedTenantDataService returns errs.UnimplementedE from every method of TenantDataService.
type UnimplementedTenantDataService struct{}

func (UnimplementedTenantDataService) RequestBackup(context.Context, *pb.RequestBackupRequest) (*pb.RequestBackupResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.RequestBackup")
}

func (UnimplementedTenantDataService) ListBackups(context.Context, *pb.ListBackupsRequest) (*pb.ListBackupsResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.ListBackups")
}

func (UnimplementedTenantDataService) GetBackupDownloadURL(context.Context, *pb.GetBackupDownloadURLRequest) (*pb.GetBackupDownloadURLResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.GetBackupDownloadURL")
}

func (UnimplementedTenantDataService) DeleteBackup(context.Context, *pb.DeleteBackupRequest) (*pb.DeleteBackupResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.DeleteBackup")
}

func (UnimplementedTenantDataService) RestoreBackup(context.Context, *pb.RestoreBackupRequest) (*pb.RestoreBackupResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.RestoreBackup")
}

func (UnimplementedTenantDataService) RestoreFromBackup(context.Context, *pb.RestoreFromBackupRequest) (*pb.RestoreFromBackupResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.RestoreFromBackup")
}

func (UnimplementedTenantDataService) GetRestoreStatus(context.Context, *pb.GetRestoreStatusRequest) (*pb.GetRestoreStatusResponse, error)  {
	return nil, errs.UnimplementedE("TenantDataService.GetRestoreStatus")
}

// UnimplementedTenantPortalService returns errs.UnimplementedE from every method of TenantPortalService.
type UnimplementedTenantPortalService struct{}

func (UnimplementedTenantPortalService) TenantLogin(context.Context, *pb.TenantLoginRequest) (*pb.TenantLoginResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.TenantLogin")
}

func (UnimplementedTenantPortalService) VerifyCaptcha(context.Context, *pb.CaptchaVerifyRequest) (*pb.CaptchaVerifyResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.VerifyCaptcha")
}

func (UnimplementedTenantPortalService) TenantLogout(context.Context, *pb.TenantLogoutRequest) (*pb.TenantLogoutResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.TenantLogout")
}

func (UnimplementedTenantPortalService) GetTenantDashboard(context.Context, *pb.GetTenantDashboardRequest) (*pb.GetTenantDashboardResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantDashboard")
}

func (UnimplementedTenantPortalService) UpdateTenantProfile(context.Context, *pb.UpdateTenantProfileRequest) (*pb.UpdateTenantProfileResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.UpdateTenantProfile")
}

func (UnimplementedTenantPortalService) DeleteTenantAccount(context.Context, *pb.DeleteTenantAccountRequest) (*pb.DeleteTenantAccountResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteTenantAccount")
}

func (UnimplementedTenantPortalService) GetTenantAccount(context.Context, *pb.GetTenantAccountRequest) (*pb.GetTenantAccountResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantAccount")
}

func (UnimplementedTenantPortalService) GetTwoFASettings(context.Context, *pb.GetTwoFASettingsRequest) (*pb.GetTwoFASettingsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTwoFASettings")
}

func (UnimplementedTenantPortalService) SetTwoFAMethod(context.Context, *pb.SetTwoFAMethodRequest) (*pb.SetTwoFAMethodResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.SetTwoFAMethod")
}

func (UnimplementedTenantPortalService) Send2FACode(context.Context, *pb.Send2FACodeRequest) (*pb.Send2FACodeResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.Send2FACode")
}

func (UnimplementedTenantPortalService) ChangePassword(context.Context, *pb.ChangePasswordRequest) (*pb.ChangePasswordResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ChangePassword")
}

func (UnimplementedTenantPortalService) HandleSecurityAlert(context.Context, *pb.HandleSecurityAlertRequest) (*pb.HandleSecurityAlertResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.HandleSecurityAlert")
}

func (UnimplementedTenantPortalService) RequestPasswordReset(context.Context, *pb.RequestPasswordResetRequest) (*pb.RequestPasswordResetResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.RequestPasswordReset")
}

func (UnimplementedTenantPortalService) VerifyResetCode(context.Context, *pb.VerifyResetCodeRequest) (*pb.VerifyResetCodeResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.VerifyResetCode")
}

func (UnimplementedTenantPortalService) ResetPassword(context.Context, *pb.ResetPasswordRequest) (*pb.ResetPasswordResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ResetPassword")
}

func (UnimplementedTenantPortalService) ListTenantPeers(context.Context, *pb.ListTenantPeersRequest) (*pb.ListTenantPeersResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantPeers")
}

func (UnimplementedTenantPortalService) AddTenantPeer(context.Context, *pb.AddTenantPeerRequest) (*pb.AddTenantPeerResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.AddTenantPeer")
}

func (UnimplementedTenantPortalService) RemoveTenantPeer(context.Context, *pb.RemoveTenantPeerRequest) (*pb.RemoveTenantPeerResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.RemoveTenantPeer")
}

func (UnimplementedTenantPortalService) UpdateTenantPeer(context.Context, *pb.UpdateTenantPeerRequest) (*pb.UpdateTenantPeerResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.UpdateTenantPeer")
}

func (UnimplementedTenantPortalService) BatchUpdateTenantPeers(context.Context, *pb.BatchUpdatePeersRequest) (*pb.BatchUpdatePeersResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.BatchUpdateTenantPeers")
}

func (UnimplementedTenantPortalService) GetTenantPeerConfig(context.Context, *pb.GetTenantPeerConfigRequest) (*pb.GetTenantPeerConfigResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantPeerConfig")
}

func (UnimplementedTenantPortalService) GetTenantTopology(context.Context, *pb.GetTenantTopologyRequest) (*pb.GetTenantTopologyResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantTopology")
}

func (UnimplementedTenantPortalService) AssignExitNode(context.Context, *pb.AssignExitNodeRequest) (*pb.AssignExitNodeResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.AssignExitNode")
}

func (UnimplementedTenantPortalService) GetTenantPeer(context.Context, *pb.GetTenantPeerRequest) (*pb.GetTenantPeerResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantPeer")
}

func (UnimplementedTenantPortalService) GetTenantPeerStats(context.Context, *pb.GetTenantPeerStatsRequest) (*pb.GetTenantPeerStatsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantPeerStats")
}

func (UnimplementedTenantPortalService) PingTenantPeer(context.Context, *pb.PingTenantPeerRequest) (*pb.PingTenantPeerResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.PingTenantPeer")
}

func (UnimplementedTenantPortalService) StreamPingTenantPeer(*pb.PingTenantPeerRequest, ServerStream[*pb.PingEvent]) error {
	return errs.UnimplementedE("TenantPortalService.StreamPingTenantPeer")
}

func (UnimplementedTenantPortalService) SetPeerNotification(context.Context, *pb.SetPeerNotificationRequest) (*pb.SetPeerNotificationResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.SetPeerNotification")
}

func (UnimplementedTenantPortalService) DisableAllPeerNotifications(context.Context, *pb.DisableAllPeerNotificationsRequest) (*pb.DisableAllPeerNotificationsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DisableAllPeerNotifications")
}

func (UnimplementedTenantPortalService) ListEnrollmentTokens(context.Context, *pb.ListEnrollmentTokensRequest) (*pb.ListEnrollmentTokensResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListEnrollmentTokens")
}

func (UnimplementedTenantPortalService) CreateEnrollmentToken(context.Context, *pb.CreateEnrollmentTokenRequest) (*pb.CreateEnrollmentTokenResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateEnrollmentToken")
}

func (UnimplementedTenantPortalService) DeleteEnrollmentToken(context.Context, *pb.DeleteEnrollmentTokenRequest) (*pb.DeleteEnrollmentTokenResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteEnrollmentToken")
}

func (UnimplementedTenantPortalService) ConfirmDevice(context.Context, *pb.ConfirmDeviceRequest) (*pb.ConfirmDeviceResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ConfirmDevice")
}

func (UnimplementedTenantPortalService) ClearTenantWinboxCredentials(context.Context, *pb.ClearTenantWinboxCredentialsRequest) (*pb.ClearTenantWinboxCredentialsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ClearTenantWinboxCredentials")
}

func (UnimplementedTenantPortalService) CreateTenantWinboxSession(context.Context, *pb.CreateTenantWinboxSessionRequest) (*pb.CreateTenantWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateTenantWinboxSession")
}

func (UnimplementedTenantPortalService) UpdateTenantWinboxSession(context.Context, *pb.UpdateTenantWinboxSessionRequest) (*pb.UpdateTenantWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.UpdateTenantWinboxSession")
}

func (UnimplementedTenantPortalService) DeleteTenantWinboxSession(context.Context, *pb.DeleteTenantWinboxSessionRequest) (*pb.DeleteTenantWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteTenantWinboxSession")
}

func (UnimplementedTenantPortalService) ListTenantWinboxSessions(context.Context, *pb.ListTenantWinboxSessionsRequest) (*pb.ListTenantWinboxSessionsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantWinboxSessions")
}

func (UnimplementedTenantPortalService) GetTenantWinboxSession(context.Context, *pb.GetTenantWinboxSessionRequest) (*pb.GetTenantWinboxSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantWinboxSession")
}

func (UnimplementedTenantPortalService) CreateTenantPeerGroup(context.Context, *pb.CreateTenantPeerGroupRequest) (*pb.CreateTenantPeerGroupResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateTenantPeerGroup")
}

func (UnimplementedTenantPortalService) DeleteTenantPeerGroup(context.Context, *pb.DeleteTenantPeerGroupRequest) (*pb.DeleteTenantPeerGroupResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteTenantPeerGroup")
}

func (UnimplementedTenantPortalService) ListTenantPeerGroups(context.Context, *pb.ListTenantPeerGroupsRequest) (*pb.ListTenantPeerGroupsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantPeerGroups")
}

func (UnimplementedTenantPortalService) AddTenantPeerToGroup(context.Context, *pb.AddTenantPeerToGroupRequest) (*pb.AddTenantPeerToGroupResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.AddTenantPeerToGroup")
}

func (UnimplementedTenantPortalService) RemoveTenantPeerFromGroup(context.Context, *pb.RemoveTenantPeerFromGroupRequest) (*pb.RemoveTenantPeerFromGroupResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.RemoveTenantPeerFromGroup")
}

func (UnimplementedTenantPortalService) CreateTenantGroupLink(context.Context, *pb.CreateTenantGroupLinkRequest) (*pb.CreateTenantGroupLinkResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateTenantGroupLink")
}

func (UnimplementedTenantPortalService) DeleteTenantGroupLink(context.Context, *pb.DeleteTenantGroupLinkRequest) (*pb.DeleteTenantGroupLinkResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteTenantGroupLink")
}

func (UnimplementedTenantPortalService) ListTenantGroupLinks(context.Context, *pb.ListTenantGroupLinksRequest) (*pb.ListTenantGroupLinksResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantGroupLinks")
}

func (UnimplementedTenantPortalService) CompileTenantGroups(context.Context, *pb.CompileTenantGroupsRequest) (*pb.CompileTenantGroupsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CompileTenantGroups")
}

func (UnimplementedTenantPortalService) GetTenantCompilationStats(context.Context, *pb.GetTenantCompilationStatsRequest) (*pb.GetTenantCompilationStatsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantCompilationStats")
}

func (UnimplementedTenantPortalService) AddTenantACLRule(context.Context, *pb.AddTenantACLRuleRequest) (*pb.AddTenantACLRuleResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.AddTenantACLRule")
}

func (UnimplementedTenantPortalService) RemoveTenantACLRule(context.Context, *pb.RemoveTenantACLRuleRequest) (*pb.RemoveTenantACLRuleResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.RemoveTenantACLRule")
}

func (UnimplementedTenantPortalService) GetTenantACLRules(context.Context, *pb.GetTenantACLRulesRequest) (*pb.GetTenantACLRulesResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantACLRules")
}

func (UnimplementedTenantPortalService) CheckTenantAccess(context.Context, *pb.CheckTenantAccessRequest) (*pb.CheckTenantAccessResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CheckTenantAccess")
}

func (UnimplementedTenantPortalService) CreateTenantWebSSHSession(context.Context, *pb.CreateTenantWebSSHSessionRequest) (*pb.CreateTenantWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateTenantWebSSHSession")
}

func (UnimplementedTenantPortalService) GetTenantWebSSHSession(context.Context, *pb.GetTenantWebSSHSessionRequest) (*pb.GetTenantWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetTenantWebSSHSession")
}

func (UnimplementedTenantPortalService) ListTenantWebSSHSessions(context.Context, *pb.ListTenantWebSSHSessionsRequest) (*pb.ListTenantWebSSHSessionsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantWebSSHSessions")
}

func (UnimplementedTenantPortalService) DisconnectTenantWebSSHSession(context.Context, *pb.DisconnectTenantWebSSHSessionRequest) (*pb.DisconnectTenantWebSSHSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DisconnectTenantWebSSHSession")
}

func (UnimplementedTenantPortalService) GetEndpointsConfig(context.Context, *pb.GetEndpointsConfigRequest) (*pb.GetEndpointsConfigResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetEndpointsConfig")
}

func (UnimplementedTenantPortalService) ListTenantSessions(context.Context, *pb.ListTenantSessionsRequest) (*pb.ListTenantSessionsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListTenantSessions")
}

func (UnimplementedTenantPortalService) DeleteTenantSession(context.Context, *pb.DeleteTenantSessionRequest) (*pb.DeleteTenantSessionResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteTenantSession")
}

func (UnimplementedTenantPortalService) BatchUpdatePeers(context.Context, *pb.BatchUpdatePeersRequest) (*pb.BatchUpdatePeersResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.BatchUpdatePeers")
}

func (UnimplementedTenantPortalService) ListAccessShares(context.Context, *pb.ListAccessSharesRequest) (*pb.ListAccessSharesResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListAccessShares")
}

func (UnimplementedTenantPortalService) CreateAccessShare(context.Context, *pb.CreateAccessShareRequest) (*pb.CreateAccessShareResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.CreateAccessShare")
}

func (UnimplementedTenantPortalService) DeleteAccessShare(context.Context, *pb.DeleteAccessShareRequest) (*pb.DeleteAccessShareResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.DeleteAccessShare")
}

func (UnimplementedTenantPortalService) ResendAccessShareInvite(context.Context, *pb.ResendAccessShareInviteRequest) (*pb.ResendAccessShareInviteResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ResendAccessShareInvite")
}

func (UnimplementedTenantPortalService) GetPendingShares(context.Context, *pb.GetPendingSharesRequest) (*pb.GetPendingSharesResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetPendingShares")
}

func (UnimplementedTenantPortalService) AcceptAccessShare(context.Context, *pb.AcceptAccessShareRequest) (*pb.AcceptAccessShareResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.AcceptAccessShare")
}

func (UnimplementedTenantPortalService) RejectAccessShare(context.Context, *pb.RejectAccessShareRequest) (*pb.RejectAccessShareResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.RejectAccessShare")
}

func (UnimplementedTenantPortalService) ListAccessibleAccounts(context.Context, *pb.ListAccessibleAccountsRequest) (*pb.ListAccessibleAccountsResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.ListAccessibleAccounts")
}

func (UnimplementedTenantPortalService) GetAccessShareByToken(context.Context, *pb.GetAccessShareByTokenRequest) (*pb.GetAccessShareByTokenResponse, error)  {
	return nil, errs.UnimplementedE("TenantPortalService.GetAccessShareByToken")
}

// UnimplementedWUSPServiceHandler returns errs.UnimplementedE from every method of WUSPServiceHandler.
type UnimplementedWUSPServiceHandler struct{}

func (UnimplementedWUSPServiceHandler) GetDeviceState(context.Context, *pb.GetWUSPDeviceStateRequest) (*pb.GetWUSPDeviceStateResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GetDeviceState")
}

func (UnimplementedWUSPServiceHandler) ListDeviceStates(context.Context, *pb.ListWUSPDeviceStatesRequest) (*pb.ListWUSPDeviceStatesResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.ListDeviceStates")
}

func (UnimplementedWUSPServiceHandler) SyncDeviceState(context.Context, *pb.SyncWUSPDeviceStateRequest) (*pb.SyncWUSPDeviceStateResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SyncDeviceState")
}

func (UnimplementedWUSPServiceHandler) SendGet(context.Context, *pb.WUSPGetRequest) (*pb.WUSPGetResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SendGet")
}

func (UnimplementedWUSPServiceHandler) SendSet(context.Context, *pb.WUSPSetRequest) (*pb.WUSPSetResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SendSet")
}

func (UnimplementedWUSPServiceHandler) SendOperate(context.Context, *pb.WUSPOperateRequest) (*pb.WUSPOperateResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SendOperate")
}

func (UnimplementedWUSPServiceHandler) SendAdd(context.Context, *pb.WUSPAddRequest) (*pb.WUSPAddResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SendAdd")
}

func (UnimplementedWUSPServiceHandler) SendDelete(context.Context, *pb.WUSPDeleteRequest) (*pb.WUSPDeleteResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.SendDelete")
}

func (UnimplementedWUSPServiceHandler) GetSupportedProtocol(context.Context, *pb.WUSPSupportedProtocolRequest) (*pb.WUSPSupportedProtocolResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GetSupportedProtocol")
}

func (UnimplementedWUSPServiceHandler) GetSupportedDM(context.Context, *pb.WUSPSupportedDMRequest) (*pb.WUSPSupportedDMResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GetSupportedDM")
}

func (UnimplementedWUSPServiceHandler) CreateSnapshot(context.Context, *pb.CreateDeviceSnapshotRequest) (*pb.CreateDeviceSnapshotResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.CreateSnapshot")
}

func (UnimplementedWUSPServiceHandler) ListSnapshots(context.Context, *pb.ListDeviceSnapshotsRequest) (*pb.ListDeviceSnapshotsResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.ListSnapshots")
}

func (UnimplementedWUSPServiceHandler) GetSnapshot(context.Context, *pb.GetDeviceSnapshotRequest) (*pb.GetDeviceSnapshotResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GetSnapshot")
}

func (UnimplementedWUSPServiceHandler) UpdateSnapshot(context.Context, *pb.UpdateDeviceSnapshotRequest) (*pb.UpdateDeviceSnapshotResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.UpdateSnapshot")
}

func (UnimplementedWUSPServiceHandler) DeleteSnapshot(context.Context, *pb.DeleteDeviceSnapshotRequest) (*pb.DeleteDeviceSnapshotResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.DeleteSnapshot")
}

func (UnimplementedWUSPServiceHandler) ProvisionDevice(context.Context, *pb.ProvisionDeviceRequest) (*pb.ProvisionDeviceResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.ProvisionDevice")
}

func (UnimplementedWUSPServiceHandler) GetSnapshotBackup(context.Context, *pb.GetSnapshotBackupRequest) (*pb.GetSnapshotBackupResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GetSnapshotBackup")
}

func (UnimplementedWUSPServiceHandler) UploadSnapshotBackup(context.Context, *pb.UploadSnapshotBackupRequest) (*pb.UploadSnapshotBackupResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.UploadSnapshotBackup")
}

func (UnimplementedWUSPServiceHandler) GenerateUploadToken(context.Context, *pb.GenerateUploadTokenRequest) (*pb.GenerateUploadTokenResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GenerateUploadToken")
}

func (UnimplementedWUSPServiceHandler) GenerateBackupToken(context.Context, *pb.GenerateBackupTokenRequest) (*pb.GenerateBackupTokenResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.GenerateBackupToken")
}

func (UnimplementedWUSPServiceHandler) EnsureWUSPSubscription(context.Context, *pb.EnsureWUSPSubscriptionRequest) (*pb.EnsureWUSPSubscriptionResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.EnsureWUSPSubscription")
}

func (UnimplementedWUSPServiceHandler) CancelWUSPSubscription(context.Context, *pb.CancelWUSPSubscriptionRequest) (*pb.CancelWUSPSubscriptionResponse, error)  {
	return nil, errs.UnimplementedE("WUSPServiceHandler.CancelWUSPSubscription")
}

// UnimplementedRouterOSServiceHandler returns errs.UnimplementedE from every method of RouterOSServiceHandler.
type UnimplementedRouterOSServiceHandler struct{}

func (UnimplementedRouterOSServiceHandler) StreamDashboard(BidiStream[*pb.StreamRouterOSDashboardRequest, *pb.StreamRouterOSDashboardEvent]) error {
	return errs.UnimplementedE("RouterOSServiceHandler.StreamDashboard")
}

func (UnimplementedRouterOSServiceHandler) GetOverview(context.Context, *pb.GetRouterOSOverviewRequest) (*pb.GetRouterOSOverviewResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.GetOverview")
}

func (UnimplementedRouterOSServiceHandler) ConfigureAccess(context.Context, *pb.ConfigureRouterOSAccessRequest) (*pb.ConfigureRouterOSAccessResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.ConfigureAccess")
}

func (UnimplementedRouterOSServiceHandler) ListResource(context.Context, *pb.ListRouterOSResourceRequest) (*pb.ListRouterOSResourceResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.ListResource")
}

func (UnimplementedRouterOSServiceHandler) AddResource(context.Context, *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.AddResource")
}

func (UnimplementedRouterOSServiceHandler) UpdateResource(context.Context, *pb.MutateRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.UpdateResource")
}

func (UnimplementedRouterOSServiceHandler) DeleteResource(context.Context, *pb.DeleteRouterOSResourceRequest) (*pb.MutateRouterOSResourceResponse, error)  {
	return nil, errs.UnimplementedE("RouterOSServiceHandler.DeleteResource")
}

