package security

// RLSDimensionable is implemented by domain entities that participate in
// row-level security checks. It returns a map of dimension name → entity's
// value for that dimension (e.g. {"organization": "org-uuid-1"}).
//
// Used by DataScope.CanAccessRecord for point-checks (GetByID, Update, Delete).
type RLSDimensionable interface {
	GetRLSDimensions() map[string]string
}
