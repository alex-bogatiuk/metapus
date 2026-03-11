package security

import "testing"

// testDimensionable is a simple test struct implementing RLSDimensionable.
type testDimensionable struct {
	OrgID    string
	SupplierID string
}

func (t *testDimensionable) GetRLSDimensions() map[string]string {
	return map[string]string{
		"organization": t.OrgID,
		"counterparty": t.SupplierID,
	}
}

func TestRLSDimensionable_WithDataScope(t *testing.T) {
	entity := &testDimensionable{
		OrgID:      "org-1",
		SupplierID: "cp-5",
	}

	// Admin scope — always allowed
	adminScope := &DataScope{IsAdmin: true}
	if !adminScope.CanAccessRecord(entity.GetRLSDimensions()) {
		t.Fatal("admin should access any record")
	}

	// Matching scope
	matchScope := &DataScope{
		Dimensions: map[string][]string{
			"organization": {"org-1", "org-2"},
			"counterparty": {"cp-5"},
		},
	}
	if !matchScope.CanAccessRecord(entity.GetRLSDimensions()) {
		t.Fatal("matching scope should access record")
	}

	// Non-matching scope (wrong org)
	wrongOrgScope := &DataScope{
		Dimensions: map[string][]string{
			"organization": {"org-3"},
			"counterparty": {"cp-5"},
		},
	}
	if wrongOrgScope.CanAccessRecord(entity.GetRLSDimensions()) {
		t.Fatal("wrong org scope should NOT access record")
	}

	// Scope with only org dimension (no counterparty restriction)
	orgOnlyScope := &DataScope{
		Dimensions: map[string][]string{
			"organization": {"org-1"},
		},
	}
	if !orgOnlyScope.CanAccessRecord(entity.GetRLSDimensions()) {
		t.Fatal("scope without counterparty restriction should still allow access")
	}
}
