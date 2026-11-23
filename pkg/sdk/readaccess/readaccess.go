// Package readaccess projects records through the read policy of the
// request's security profile.
package readaccess

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/internal/application/readaccess"
	"github.com/alex-bogatiuk/metapus/internal/core/security"
)

type (
	Definition = core.Definition
	Field      = core.Field
	Projection = core.Projection
	Query      = core.Query
	Record     = core.Record
	Service    = core.Service
)

func FromContext(ctx context.Context) (*core.Service, bool) { return core.FromContext(ctx) }

func New(reg core.Registry, repo core.Repository, policies *security.PolicyEngine) *core.Service {
	return core.New(reg, repo, policies)
}

func WithService(ctx context.Context, s *core.Service) context.Context {
	return core.WithService(ctx, s)
}
