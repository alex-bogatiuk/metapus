package rpcpolicy

import (
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"time"
)

var (
	ErrResponseTooLarge   = errors.New("RPC response exceeds configured limit")
	ErrUnsupported        = errors.New("RPC method unsupported")
	ErrQueryLimit         = errors.New("RPC query range unavailable on current plan")
	ErrCredentialsExpired = errors.New("integration credentials expired")
	ErrAccessDenied       = errors.New("integration provider denied access")
	ErrArchiveUnavailable = errors.New("RPC archive access unavailable on current plan")
	ErrAdmissionBusy      = errors.New("RPC admission budget exhausted")
	ErrRateLimited        = errors.New("RPC rate limited")
	ErrInvalidResponse    = errors.New("missing or invalid RPC response")
	ErrInvalidRequest     = errors.New("invalid RPC request")
)

type ThrottleError struct{ Until time.Time }

func (*ThrottleError) Error() string { return ErrRateLimited.Error() }
func (*ThrottleError) Unwrap() error { return ErrRateLimited }

func RetryAfter(value string, now time.Time) time.Time {
	until := now.Add(30 * time.Second)
	if seconds, err := strconv.Atoi(value); err == nil {
		until = now.Add(time.Duration(max(1, min(seconds, 300))) * time.Second)
	} else if date, err := http.ParseTime(value); err == nil {
		until = minTime(maxTime(date, now.Add(time.Second)), now.Add(5*time.Minute))
	}
	return until.Add(time.Duration(rand.IntN(1000)) * time.Millisecond)
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// LimitResponse catches both declared and chunked oversize bodies. The caller
// owns closing the response body on success; on rejection it is closed here.
func LimitResponse(resp *http.Response, limit int64, onClose func(int64)) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		_ = resp.Body.Close()
		return &ThrottleError{Until: RetryAfter(resp.Header.Get("Retry-After"), time.Now())}
	}
	if resp.ContentLength > limit {
		_ = resp.Body.Close()
		return ErrResponseTooLarge
	}
	resp.Body = &boundedBody{ReadCloser: resp.Body, remaining: limit, onClose: onClose}
	return nil
}

type boundedBody struct {
	io.ReadCloser
	remaining, read int64
	onClose         func(int64)
}

func (b *boundedBody) Read(p []byte) (int, error) {
	if b.remaining <= 0 {
		var extra [1]byte
		n, err := b.ReadCloser.Read(extra[:])
		if n > 0 {
			return 0, ErrResponseTooLarge
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:b.remaining]
	}
	n, err := b.ReadCloser.Read(p)
	b.remaining -= int64(n)
	b.read += int64(n)
	return n, err
}
func (b *boundedBody) Close() error {
	if b.onClose != nil {
		b.onClose(b.read)
		b.onClose = nil
	}
	return b.ReadCloser.Close()
}
