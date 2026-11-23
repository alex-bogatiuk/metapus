package auth

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/core/id"
)

type listUsersRepo struct {
	UserRepository
	users             []User
	roles             map[id.ID][]Role
	listErr, rolesErr error
	batchCalls        int
}

func (r *listUsersRepo) List(context.Context, UserFilter) ([]User, int, error) {
	return append([]User(nil), r.users...), len(r.users), r.listErr
}

func (r *listUsersRepo) LoadRolesForUsers(_ context.Context, ids []id.ID) (map[id.ID][]Role, error) {
	r.batchCalls++
	return r.roles, r.rolesErr
}

type listMFARepo struct {
	MFARepository
	statuses map[id.ID]MFAStatus
	err      error
	calls    int
}

func (r *listMFARepo) LoadMFAStatuses(context.Context, []id.ID) (map[id.ID]MFAStatus, error) {
	r.calls++
	return r.statuses, r.err
}

func TestListUsersBatchesRelationsAndPreservesOrder(t *testing.T) {
	for _, n := range []int{1, 100} {
		t.Run(strconv.Itoa(n), func(t *testing.T) {
			u := &listUsersRepo{users: make([]User, n), roles: make(map[id.ID][]Role)}
			for i := range u.users {
				u.users[i].ID = id.New()
			}
			first := u.users[0].ID
			u.roles[first] = []Role{{Code: "security_admin"}}
			grace := time.Now().Add(time.Hour)
			m := &listMFARepo{statuses: map[id.ID]MFAStatus{first: {Enabled: true, RequiredAfter: &grace}}}
			s := &Service{userRepo: u, mfaRepo: m, config: ServiceConfig{AdminMFAMode: MFAModeRequired}}
			users, total, err := s.ListUsers(context.Background(), UserFilter{})
			require.NoError(t, err)
			require.Equal(t, n, total)
			require.Equal(t, 1, u.batchCalls)
			require.Equal(t, 1, m.calls)
			for i, user := range users {
				require.Equal(t, u.users[i].ID, user.ID)
			}
			require.True(t, users[0].MFAEnabled)
			require.True(t, users[0].MFARequired)
			require.Equal(t, &grace, users[0].MFAGraceEndsAt)
			if n > 1 {
				require.Empty(t, users[1].Roles)
				require.False(t, users[1].MFAEnabled)
				require.False(t, users[1].MFARequired)
			}
		})
	}
}

func TestListUsersEmptyPageDoesNotLoadRelations(t *testing.T) {
	u, m := &listUsersRepo{}, &listMFARepo{}
	s := &Service{userRepo: u, mfaRepo: m}
	users, total, err := s.ListUsers(context.Background(), UserFilter{})
	require.NoError(t, err)
	require.Empty(t, users)
	require.Zero(t, total)
	require.Zero(t, u.batchCalls)
	require.Zero(t, m.calls)
}

func TestListUsersPropagatesRelationFailures(t *testing.T) {
	sentinel := errors.New("database unavailable")
	for _, stage := range []string{"list", "roles", "mfa"} {
		t.Run(stage, func(t *testing.T) {
			u := &listUsersRepo{users: []User{{ID: id.New()}}}
			m := &listMFARepo{}
			switch stage {
			case "list":
				u.listErr = sentinel
			case "roles":
				u.rolesErr = sentinel
			case "mfa":
				m.err = sentinel
			}
			s := &Service{userRepo: u, mfaRepo: m}
			users, _, err := s.ListUsers(context.Background(), UserFilter{})
			require.ErrorIs(t, err, sentinel)
			require.Nil(t, users)
		})
	}
}

func TestMFAStatusMissingEnrollmentIsDistinctFromReadFailure(t *testing.T) {
	user := &User{ID: id.New(), IsAdmin: true, MFAEnabled: true}
	m := &listMFARepo{}
	s := &Service{mfaRepo: m, config: ServiceConfig{AdminMFAMode: MFAModeRequired}}
	require.NoError(t, s.enrichMFAStatus(context.Background(), user))
	require.False(t, user.MFAEnabled)
	require.True(t, user.MFARequired)
	m.err = errors.New("database unavailable")
	require.ErrorIs(t, s.enrichMFAStatus(context.Background(), user), m.err)
}
