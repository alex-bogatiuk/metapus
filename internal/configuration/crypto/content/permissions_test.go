package content

import (
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"

	"metapus/internal/configuration"
)

// TestAssembledPermissionsFitTheTableAndCoverEveryEntity checks the list the
// tenant migration command syncs: every registered entity and dataset has its
// read permission, codes are unique, and each row fits auth_permissions
// (code VARCHAR(100), name VARCHAR(200), resource and action VARCHAR(50)).
func TestAssembledPermissionsFitTheTableAndCoverEveryEntity(t *testing.T) {
	permissions := configuration.Permissions(Crypto(Services{}))
	codes := map[string]bool{}
	for _, p := range permissions {
		require.Falsef(t, codes[p.Code], "duplicate permission %s", p.Code)
		codes[p.Code] = true
		require.LessOrEqual(t, utf8.RuneCountInString(p.Code), 100, p.Code)
		require.LessOrEqual(t, utf8.RuneCountInString(p.Name), 200, p.Code)
		require.LessOrEqual(t, utf8.RuneCountInString(p.Resource), 50, p.Code)
		require.LessOrEqual(t, utf8.RuneCountInString(p.Action), 50, p.Code)
		require.NotEmpty(t, p.Resource, p.Code)
		require.NotEmpty(t, p.Action, p.Code)
	}

	reg := configuration.Compose(Crypto(Services{}))
	var required []string
	for _, f := range reg.Catalogs() {
		required = append(required, f.Permission()+":read", f.Permission()+":create", f.Permission()+":update", f.Permission()+":delete")
	}
	for _, f := range reg.Documents() {
		required = append(required, f.Permission()+":read")
	}
	for _, f := range reg.CustomDocuments() {
		required = append(required, f.Permission()+":read")
	}
	for _, ds := range reg.Datasets() {
		required = append(required, ds.Permission)
	}
	for _, code := range required {
		require.Truef(t, codes[code], "registered permission %s is not synced", code)
	}
}
