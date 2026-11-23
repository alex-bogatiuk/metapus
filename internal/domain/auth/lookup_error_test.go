package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/core/tenant"
	"github.com/alex-bogatiuk/metapus/pkg/logger"
)

type failingLookupUserRepo struct {
	UserRepository
	err error
}

func (r failingLookupUserRepo) GetByEmail(context.Context, string) (*User, error) {
	return nil, r.err
}

func (r failingLookupUserRepo) GetByID(context.Context, id.ID) (*User, error) {
	return nil, r.err
}

type failingLookupRoleRepo struct {
	RoleRepository
	err error
}

func (r failingLookupRoleRepo) GetByID(context.Context, id.ID) (*Role, error) {
	return nil, r.err
}

type failingLookupTokenRepo struct {
	TokenRepository
	err error
}

func (r failingLookupTokenRepo) GetRefreshToken(context.Context, string) (*RefreshToken, error) {
	return nil, r.err
}

func TestServiceLookupsDistinguishMissingRecordsFromStorageFailures(t *testing.T) {
	userID := id.New()
	storageErr := errors.New("database connection lost")
	tests := []struct {
		name string
		call func(*Service, context.Context) error
		want int
	}{
		{"login", func(s *Service, ctx context.Context) error {
			_, err := s.Login(ctx, Credentials{Email: "missing@example.com"}, SessionInfo{})
			return err
		}, http.StatusUnauthorized},
		{"refresh", func(s *Service, ctx context.Context) error {
			_, err := s.RefreshToken(ctx, "invalid-refresh", SessionInfo{})
			return err
		}, http.StatusUnauthorized},
		{"get user", func(s *Service, ctx context.Context) error {
			_, err := s.GetUserByID(ctx, userID)
			return err
		}, http.StatusNotFound},
		{"assign role", func(s *Service, ctx context.Context) error {
			return s.AssignRole(ctx, userID, "admin")
		}, http.StatusNotFound},
		{"get role", func(s *Service, ctx context.Context) error {
			_, _, _, err := s.GetRole(ctx, userID)
			return err
		}, http.StatusNotFound},
		{"role permissions", func(s *Service, ctx context.Context) error {
			_, err := s.ListRolePermissions(ctx, userID)
			return err
		}, http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, give := range []struct {
				name string
				err  error
				want int
			}{
				{"missing", fmt.Errorf("query: %w", apperror.NewNotFound("record", userID)), tt.want},
				{"storage failure", storageErr, http.StatusInternalServerError},
			} {
				t.Run(give.name, func(t *testing.T) {
					core, logs := observer.New(zap.ErrorLevel)
					ctx := tenant.WithTenant(context.Background(), &tenant.Tenant{ID: "tenant-a"})
					ctx = logger.WithLogger(ctx, &logger.Logger{SugaredLogger: zap.New(core).Sugar()})
					s := &Service{
						userRepo:  failingLookupUserRepo{err: give.err},
						roleRepo:  failingLookupRoleRepo{err: give.err},
						tokenRepo: failingLookupTokenRepo{err: give.err},
						txManager: testTxManager{},
					}
					err := tt.call(s, ctx)
					appErr, ok := apperror.AsAppError(err)
					require.True(t, ok)
					require.Equal(t, give.want, appErr.HTTPStatus)
					require.Zero(t, logs.Len(), "the HTTP boundary owns error logging")
					if give.want == http.StatusInternalServerError {
						require.ErrorIs(t, err, storageErr)
						require.NotContains(t, appErr.Message, storageErr.Error())
					} else {
						require.Nil(t, appErr.Err, "expected misses must not be logged as internal failures")
					}
				})
			}
		})
	}
}
