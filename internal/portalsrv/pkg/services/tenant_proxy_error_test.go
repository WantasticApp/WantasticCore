package services

import (
	"WantasticCore/internal/errs"
	"errors"
	"testing"
)

func TestSanitizeClientErrorMapsFailoverToGenericSentence(t *testing.T) {
	err := errors.New(`all failover attempts failed for /overlay.v1.TenantPortalService/ListTenantSessions: rpc error: code = Unavailable desc = connection error: desc = "transport: Error while dialing: dial tcp [fd07:b51a:cc66:d000::3]:50052: connect: connection refused"`)

	got := sanitizeClientError(err)
	want := "Service is temporarily unavailable. Please try again later."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSanitizeClientErrorMapsGrpcInvalidArgument(t *testing.T) {
	err := errs.InvalidArgumentE("invalid request: json: cannot unmarshal")

	got := sanitizeClientError(err)
	want := "Invalid request. Please check your input and try again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSanitizeClientErrorKeepsSessionExpiredGeneric(t *testing.T) {
	err := errs.UnauthenticatedE("token expired")

	got := sanitizeClientError(err)
	want := "Your session has expired. Please sign in again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestSanitizeClientErrorMessageForSSHStreamRawError(t *testing.T) {
	got := sanitizeClientErrorMessage(
		"Failed to start SSH stream: rpc error: code = Unavailable desc = transport: Error while dialing",
		"fallback",
	)
	want := "Failed to start SSH stream. Please try again."
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
