package admin

import (
	"errors"
	"fmt"
)

// BootstrapAdmin ensures the system has at least one admin tenant. If no admin
// exists yet, it creates one with the supplied credentials. If one already
// exists, the call is a no-op and the existing admin is returned.
// Intended for the first-run setup wizard and idempotent re-runs.
func (s *Service) BootstrapAdmin(email, fullName, password string, maxPeers int) error {
	if email == "" || password == "" {
		return errors.New("bootstrap admin requires email and password")
	}

	all, err := s.registry.ListTenants()
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	for _, t := range all {
		if t.IsAdmin {
			return nil // an admin already exists; nothing to do
		}
	}

	_, err = s.CreateTenant(CreateTenantInput{
		Email:    email,
		FullName: fullName,
		Password: password,
		MaxPeers: maxPeers,
		IsAdmin:  true,
	})
	return err
}
