// Package errs provides typed application errors that replace the
// google.golang.org/grpc/status + codes packages. Each error carries a
// semantic Code that the WebSocket bridge maps to an HTTP-friendly
// response shape; the wire format stays JSON, no gRPC runtime needed.
//
// Construction helpers mirror the most common status.Error / status.Errorf
// patterns so the conversion sweep is a one-to-one rename:
//
//	status.Error(codes.NotFound, "tenant")         →   errs.NotFound("tenant")
//	status.Errorf(codes.Internal, "fmt %v", x)     →   errs.Internalf("fmt %v", x)
package errs

import (
	"errors"
	"fmt"
)

// Code is the semantic category of an error. Mirrors the gRPC codes we
// actually used in the codebase (Unimplemented, NotFound, Internal,
// InvalidArgument, PermissionDenied, Unauthenticated, FailedPrecondition,
// AlreadyExists, Unavailable). Anything outside this set maps to Unknown.
type Code int

const (
	Unknown Code = iota
	InvalidArgument
	NotFound
	AlreadyExists
	PermissionDenied
	Unauthenticated
	FailedPrecondition
	Unavailable
	Unimplemented
	Internal
)

// String returns the lowercase identifier for the code — used by the WS
// bridge when serializing an error to the client.
func (c Code) String() string {
	switch c {
	case InvalidArgument:
		return "invalid_argument"
	case NotFound:
		return "not_found"
	case AlreadyExists:
		return "already_exists"
	case PermissionDenied:
		return "permission_denied"
	case Unauthenticated:
		return "unauthenticated"
	case FailedPrecondition:
		return "failed_precondition"
	case Unavailable:
		return "unavailable"
	case Unimplemented:
		return "unimplemented"
	case Internal:
		return "internal"
	default:
		return "unknown"
	}
}

// HTTPStatus returns the most sensible HTTP status to surface for this code.
// Used when projecting these errors over a JSON-over-WebSocket transport.
func (c Code) HTTPStatus() int {
	switch c {
	case InvalidArgument, FailedPrecondition:
		return 400
	case Unauthenticated:
		return 401
	case PermissionDenied:
		return 403
	case NotFound:
		return 404
	case AlreadyExists:
		return 409
	case Unimplemented:
		return 501
	case Unavailable:
		return 503
	case Internal, Unknown:
		fallthrough
	default:
		return 500
	}
}

// Error is the concrete type returned by all constructors. It satisfies
// `error` and supports `errors.Unwrap` so wrapped causes propagate.
type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// CodeOf returns the semantic code for an error. Walks Unwrap chains so
// wrapped Errors are recognized. Returns Unknown for non-Error values.
func CodeOf(err error) Code {
	if err == nil {
		return Unknown
	}
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return Unknown
}

// IsCode reports whether err carries (or wraps) the given code.
func IsCode(err error, c Code) bool {
	return CodeOf(err) == c
}

// ─────────────────────────────────────────────────────────────────────
// Constructors — one pair per Code (plain + format)
// ─────────────────────────────────────────────────────────────────────

func newE(c Code, msg string) error          { return &Error{Code: c, Message: msg} }
func newEf(c Code, f string, a ...any) error { return &Error{Code: c, Message: fmt.Sprintf(f, a...)} }

// Wrap returns an error with the given code that wraps cause. The wrapped
// error's text is appended to the message: "cannot create tenant: <cause>".
func Wrap(c Code, msg string, cause error) error {
	if cause == nil {
		return &Error{Code: c, Message: msg}
	}
	return &Error{Code: c, Message: fmt.Sprintf("%s: %v", msg, cause), Cause: cause}
}

// InvalidArgumentE / etc. are the public constructors. Each pair mirrors
// the gRPC pattern: plain string and printf-style.

func InvalidArgumentE(msg string) error             { return newE(InvalidArgument, msg) }
func InvalidArgumentf(f string, a ...any) error     { return newEf(InvalidArgument, f, a...) }
func NotFoundE(msg string) error                    { return newE(NotFound, msg) }
func NotFoundf(f string, a ...any) error            { return newEf(NotFound, f, a...) }
func AlreadyExistsE(msg string) error               { return newE(AlreadyExists, msg) }
func AlreadyExistsf(f string, a ...any) error       { return newEf(AlreadyExists, f, a...) }
func PermissionDeniedE(msg string) error            { return newE(PermissionDenied, msg) }
func PermissionDeniedf(f string, a ...any) error    { return newEf(PermissionDenied, f, a...) }
func UnauthenticatedE(msg string) error             { return newE(Unauthenticated, msg) }
func Unauthenticatedf(f string, a ...any) error     { return newEf(Unauthenticated, f, a...) }
func FailedPreconditionE(msg string) error          { return newE(FailedPrecondition, msg) }
func FailedPreconditionf(f string, a ...any) error  { return newEf(FailedPrecondition, f, a...) }
func UnavailableE(msg string) error                 { return newE(Unavailable, msg) }
func Unavailablef(f string, a ...any) error         { return newEf(Unavailable, f, a...) }
func UnimplementedE(msg string) error               { return newE(Unimplemented, msg) }
func Unimplementedf(f string, a ...any) error       { return newEf(Unimplemented, f, a...) }
func InternalE(msg string) error                    { return newE(Internal, msg) }
func Internalf(f string, a ...any) error            { return newEf(Internal, f, a...) }
