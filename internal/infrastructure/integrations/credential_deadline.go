package integrations

import (
	"context"
	"errors"
	"io"
	model "metapus/internal/domain/integrations"
	"metapus/internal/infrastructure/providerpolicy"
	"time"
)

func selectionExpired(s model.Selection, now time.Time) bool {
	return s.CredentialsExpiresAt != nil && !now.Before(*s.CredentialsExpiresAt)
}

func credentialContext(ctx context.Context, s model.Selection, now time.Time) (context.Context, context.CancelFunc) {
	if s.CredentialsExpiresAt == nil {
		return ctx, func() {}
	}
	return context.WithTimeoutCause(ctx, s.CredentialsExpiresAt.Sub(now), providerpolicy.ErrCredentialsExpired)
}

type credentialBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *credentialBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		if b.ctx != nil && errors.Is(context.Cause(b.ctx), providerpolicy.ErrCredentialsExpired) {
			err = providerpolicy.ErrCredentialsExpired
		}
		b.cancel()
	}
	return n, err
}
func (b *credentialBody) Close() error { defer b.cancel(); return b.ReadCloser.Close() }
