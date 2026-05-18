package core

// Services bundles direct references to every service implementation so
// in-process callers (the merged portal and adminbot) can invoke handlers
// as plain Go method calls. Fields are typed as handwritten interfaces
// from iface.go (no gRPC dependency).
type Services struct {
	Account            AccountService
	Auth               AuthService
	Peer               PeerService
	RouterOS           RouterOSServiceHandler
	TenantBilling      TenantBillingService
	TenantData         TenantDataService
	TenantPortal       TenantPortalService
	TenantRegistration TenantRegistrationService
	WUSP               WUSPServiceHandler
	WebProxy           WebProxyService
	WebSSH             WebSSHService
}

// ServiceBundle returns the service implementations registered on this server so
// in-process callers can invoke them without a gRPC client.
func (s *GRPCServer) ServiceBundle() *Services {
	out := &Services{
		Account:            s.accountSvc,
		WebSSH:             s.websshSvc,
		TenantBilling:      s.tenantBillingSvc,
		TenantData:         s.tenantDataSvc,
		TenantPortal:       s.tenantPortalSvc,
		TenantRegistration: s.tenantRegSvc,
		Peer:               s.peerSvc,
		WUSP:               s.wuspSvc,
		RouterOS:           s.routerOSSvc,
		WebProxy:           s.webproxySvc,
	}
	if s.wantasticSvc != nil {
		out.Auth = s.wantasticSvc
	}
	return out
}
