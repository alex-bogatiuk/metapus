package security_profile

import (
	"context"
	"sync"
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/core/security"
)

// mockRepo implements Repository for testing.
type mockRepo struct {
	mu        sync.Mutex
	profiles  map[id.ID]*SecurityProfile // profileID → profile
	userMap   map[id.ID]id.ID            // userID → profileID
	callCount int
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		profiles: make(map[id.ID]*SecurityProfile),
		userMap:  make(map[id.ID]id.ID),
	}
}

func (r *mockRepo) GetByID(_ context.Context, profileID id.ID) (*SecurityProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount++
	if p, ok := r.profiles[profileID]; ok {
		return p, nil
	}
	return nil, nil
}

func (r *mockRepo) GetByUserID(_ context.Context, userID id.ID) (*SecurityProfile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callCount++
	if pid, ok := r.userMap[userID]; ok {
		if p, ok := r.profiles[pid]; ok {
			return p, nil
		}
	}
	return nil, nil
}

func (r *mockRepo) List(_ context.Context) ([]*SecurityProfile, error) { return nil, nil }
func (r *mockRepo) Create(_ context.Context, _ *SecurityProfile) error { return nil }
func (r *mockRepo) Update(_ context.Context, _ *SecurityProfile) error { return nil }
func (r *mockRepo) Delete(_ context.Context, _ id.ID) error            { return nil }
func (r *mockRepo) AssignToUser(_ context.Context, _, _ id.ID) error   { return nil }
func (r *mockRepo) RemoveFromUser(_ context.Context, _, _ id.ID) error { return nil }

func TestCachedProfileProvider_CacheHit(t *testing.T) {
	repo := newMockRepo()
	userID := id.New()
	profileID := id.New()

	profile := &SecurityProfile{
		ID:   profileID,
		Code: "test",
		Name: "Test Profile",
		Dimensions: map[string][]string{
			"organization": {"org-1", "org-2"},
		},
	}
	repo.profiles[profileID] = profile
	repo.userMap[userID] = profileID

	provider := NewCachedProfileProvider(repo, 5*time.Minute)
	ctx := context.Background()

	// First call — cache miss, hits repo
	p1, err := provider.GetUserProfile(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p1 == nil || p1.Code != "test" {
		t.Fatal("expected profile on first call")
	}
	if repo.callCount != 1 {
		t.Fatalf("expected 1 repo call, got %d", repo.callCount)
	}

	// Second call — cache hit, no repo call
	p2, err := provider.GetUserProfile(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p2 == nil || p2.Code != "test" {
		t.Fatal("expected profile on second call")
	}
	if repo.callCount != 1 {
		t.Fatalf("expected 1 repo call (cached), got %d", repo.callCount)
	}
}

func TestCachedProfileProvider_Invalidate(t *testing.T) {
	repo := newMockRepo()
	userID := id.New()
	profileID := id.New()

	profile := &SecurityProfile{
		ID:   profileID,
		Code: "test",
		Name: "Test Profile",
	}
	repo.profiles[profileID] = profile
	repo.userMap[userID] = profileID

	provider := NewCachedProfileProvider(repo, 5*time.Minute)
	ctx := context.Background()

	// Warm cache
	_, _ = provider.GetUserProfile(ctx, userID)
	if repo.callCount != 1 {
		t.Fatalf("expected 1 call, got %d", repo.callCount)
	}

	// Invalidate
	provider.Invalidate(userID)

	// Next call hits repo again
	_, _ = provider.GetUserProfile(ctx, userID)
	if repo.callCount != 2 {
		t.Fatalf("expected 2 calls after invalidate, got %d", repo.callCount)
	}
}

func TestCachedProfileProvider_TTLExpiry(t *testing.T) {
	repo := newMockRepo()
	userID := id.New()
	profileID := id.New()

	profile := &SecurityProfile{
		ID:   profileID,
		Code: "test",
		Name: "Test Profile",
	}
	repo.profiles[profileID] = profile
	repo.userMap[userID] = profileID

	// Very short TTL
	provider := NewCachedProfileProvider(repo, 10*time.Millisecond)
	ctx := context.Background()

	_, _ = provider.GetUserProfile(ctx, userID)
	if repo.callCount != 1 {
		t.Fatalf("expected 1 call, got %d", repo.callCount)
	}

	// Wait for TTL to expire
	time.Sleep(20 * time.Millisecond)

	_, _ = provider.GetUserProfile(ctx, userID)
	if repo.callCount != 2 {
		t.Fatalf("expected 2 calls after TTL expiry, got %d", repo.callCount)
	}
}

func TestCachedProfileProvider_NoProfile(t *testing.T) {
	repo := newMockRepo()
	userID := id.New()

	provider := NewCachedProfileProvider(repo, 5*time.Minute)
	ctx := context.Background()

	p, err := provider.GetUserProfile(ctx, userID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p != nil {
		t.Fatal("expected nil profile for unassigned user")
	}
}

func TestBuildSecurityContext_Admin(t *testing.T) {
	sc := BuildSecurityContext(nil, []string{"org-1"}, true)

	if !sc.DataScope.IsAdmin {
		t.Fatal("expected admin scope")
	}
	if sc.FieldPolicies != nil {
		t.Fatal("expected nil policies for admin")
	}
	if len(sc.PolicyRules) != 0 {
		t.Fatal("expected no policy rules for admin")
	}
}

func TestBuildSecurityContext_NoProfile(t *testing.T) {
	jwtOrgs := []string{"org-1", "org-2"}
	sc := BuildSecurityContext(nil, jwtOrgs, false)

	if sc.DataScope.IsAdmin {
		t.Fatal("should not be admin")
	}
	orgs := sc.DataScope.Dimensions[security.DimOrganization]
	if len(orgs) != 2 || orgs[0] != "org-1" {
		t.Fatalf("expected JWT orgs in scope, got %v", orgs)
	}
	if sc.FieldPolicies != nil {
		t.Fatal("expected nil policies when no profile")
	}
	if len(sc.PolicyRules) != 0 {
		t.Fatal("expected no policy rules when no profile")
	}
}

func TestBuildSecurityContext_WithProfile(t *testing.T) {
	profile := &SecurityProfile{
		ID:   id.New(),
		Code: "sales",
		Name: "Sales Manager",
		Dimensions: map[string][]string{
			"organization": {"org-2", "org-3"},
			"counterparty": {"cp-1"},
		},
		FieldPolicies: map[string]*security.FieldPolicy{
			"goods_receipt:read": {
				EntityName:    "goods_receipt",
				Action:        "read",
				AllowedFields: []string{"*", "-unit_price"},
			},
		},
	}
	jwtOrgs := []string{"org-1", "org-2"}

	sc := BuildSecurityContext(profile, jwtOrgs, false)

	// Organization: intersection of JWT {org-1, org-2} and profile {org-2, org-3} = {org-2}
	orgs := sc.DataScope.Dimensions[security.DimOrganization]
	if len(orgs) != 1 || orgs[0] != "org-2" {
		t.Fatalf("expected intersected orgs [org-2], got %v", orgs)
	}

	// Counterparty from profile
	cps := sc.DataScope.Dimensions["counterparty"]
	if len(cps) != 1 || cps[0] != "cp-1" {
		t.Fatalf("expected counterparty [cp-1], got %v", cps)
	}

	// FieldPolicies present
	if sc.FieldPolicies == nil {
		t.Fatal("expected field policies")
	}
	if sc.FieldPolicies["goods_receipt:read"] == nil {
		t.Fatal("expected goods_receipt:read policy")
	}
}

func TestSecurityProfile_BuildDataScope_NoProfileOrgs(t *testing.T) {
	// Profile has no organization dimension → JWT orgs used directly
	profile := &SecurityProfile{
		ID:         id.New(),
		Dimensions: map[string][]string{},
	}
	jwtOrgs := []string{"org-1", "org-2"}

	scope := profile.BuildDataScope(jwtOrgs, false)

	orgs := scope.Dimensions[security.DimOrganization]
	if len(orgs) != 2 {
		t.Fatalf("expected 2 JWT orgs, got %v", orgs)
	}
}
