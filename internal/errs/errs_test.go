package errs

import (
	"errors"
	"testing"
)

func TestConstructorsCarryCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want Code
	}{
		{"InvalidArgumentE", InvalidArgumentE("x"), InvalidArgument},
		{"NotFoundE", NotFoundE("x"), NotFound},
		{"AlreadyExistsE", AlreadyExistsE("x"), AlreadyExists},
		{"PermissionDeniedE", PermissionDeniedE("x"), PermissionDenied},
		{"UnauthenticatedE", UnauthenticatedE("x"), Unauthenticated},
		{"FailedPreconditionE", FailedPreconditionE("x"), FailedPrecondition},
		{"UnavailableE", UnavailableE("x"), Unavailable},
		{"UnimplementedE", UnimplementedE("x"), Unimplemented},
		{"InternalE", InternalE("x"), Internal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CodeOf(c.err); got != c.want {
				t.Errorf("CodeOf = %v, want %v", got, c.want)
			}
		})
	}
}

func TestFmtVariantSubstitutes(t *testing.T) {
	err := NotFoundf("tenant %q", "abc")
	if got, want := err.Error(), `tenant "abc"`; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !IsCode(err, NotFound) {
		t.Errorf("IsCode(err, NotFound) = false, want true")
	}
}

func TestWrapPropagatesCause(t *testing.T) {
	cause := errors.New("disk full")
	err := Wrap(Internal, "save tenant", cause)
	if !errors.Is(err, cause) {
		t.Errorf("errors.Is(err, cause) = false")
	}
	if got := CodeOf(err); got != Internal {
		t.Errorf("CodeOf = %v, want %v", got, Internal)
	}
	if got, want := err.Error(), "save tenant: disk full"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := map[Code]int{
		InvalidArgument:    400,
		Unauthenticated:    401,
		PermissionDenied:   403,
		NotFound:           404,
		AlreadyExists:      409,
		FailedPrecondition: 400,
		Unimplemented:      501,
		Unavailable:        503,
		Internal:           500,
		Unknown:            500,
	}
	for c, want := range cases {
		if got := c.HTTPStatus(); got != want {
			t.Errorf("%v.HTTPStatus() = %d, want %d", c, got, want)
		}
	}
}

func TestCodeOfPlainError(t *testing.T) {
	if got := CodeOf(errors.New("plain")); got != Unknown {
		t.Errorf("CodeOf(plain) = %v, want Unknown", got)
	}
	if got := CodeOf(nil); got != Unknown {
		t.Errorf("CodeOf(nil) = %v, want Unknown", got)
	}
}
