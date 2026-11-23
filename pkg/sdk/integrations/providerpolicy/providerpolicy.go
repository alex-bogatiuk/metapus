// Package providerpolicy admits calls to external providers within their
// capacity and classifies their failures.
package providerpolicy

import (
	"context"
	"net"
	"net/http"
	"time"

	core "github.com/alex-bogatiuk/metapus/internal/infrastructure/providerpolicy"
)

type (
	// Admission belongs to a process endpoint, shared across tenants and roles.
	// A smaller bulk semaphore leaves capacity for urgent state/receipt reads.
	Admission     = core.Admission
	Capacity      = core.Capacity
	ThrottleError = core.ThrottleError
)

var (
	ErrAccessDenied       = core.ErrAccessDenied
	ErrAdmissionBusy      = core.ErrAdmissionBusy
	ErrArchiveUnavailable = core.ErrArchiveUnavailable
	ErrCredentialsExpired = core.ErrCredentialsExpired
	ErrInvalidRequest     = core.ErrInvalidRequest
	ErrInvalidResponse    = core.ErrInvalidResponse
	ErrQueryLimit         = core.ErrQueryLimit
	ErrRateLimited        = core.ErrRateLimited
	ErrResponseTooLarge   = core.ErrResponseTooLarge
	ErrUnsupported        = core.ErrUnsupported
)

func Acquire(endpoint string, c core.Capacity) (*core.Admission, error) {
	return core.Acquire(endpoint, c)
}

// AcquireScoped keeps tenant pools with the same secret-free URL independent.
// Capacity is part of the generation identity so new settings can be prepared
// while the old generation drains; unchanged settings retain shared admission.
func AcquireScoped(scope string, endpoint string, c core.Capacity) (*core.Admission, error) {
	return core.AcquireScoped(scope, endpoint, c)
}

// IsBulk reports whether ctx carries the bulk mark of WithBulk.
func IsBulk(ctx context.Context) bool { return core.IsBulk(ctx) }

// LimitResponse catches both declared and chunked oversize bodies. The caller
// owns closing the response body on success; on rejection it is closed here.
func LimitResponse(resp *http.Response, limit int64, onClose func(int64)) error {
	return core.LimitResponse(resp, limit, onClose)
}

// PublicDialContext pins the validated destination IP. An optional DNS-over-TLS
// server avoids a local VPN's fake-IP DNS without permitting reserved RPC IPs.
// The resolver is an operator-controlled public IP:853 with a valid TLS identity.
func PublicDialContext(dnsTLSServer string) func(context.Context, string, string) (net.Conn, error) {
	return core.PublicDialContext(dnsTLSServer)
}

func Release(a *core.Admission) { core.Release(a) }

func RetryAfter(value string, now time.Time) time.Time { return core.RetryAfter(value, now) }

// WithBulk marks a request context as bulk work: history, full blocks, logs
// and receipts of whole blocks. Admission layers below the chain adapter,
// such as an integration pool, use the mark to keep urgent reads ahead.
func WithBulk(ctx context.Context, bulk bool) context.Context { return core.WithBulk(ctx, bulk) }
