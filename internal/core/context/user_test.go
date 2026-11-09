package context

import "testing"

func TestIsSystemAdminExcludesSecurityAdmin(t *testing.T) {
	tests := []struct {
		name string
		user *UserContext
		want bool
	}{
		{name: "nil", user: nil, want: false},
		{name: "regular", user: &UserContext{Roles: []string{"operator"}}, want: false},
		{name: "security admin", user: &UserContext{Roles: []string{"security_admin"}}, want: false},
		{name: "admin role", user: &UserContext{Roles: []string{"admin"}}, want: true},
		{name: "admin flag", user: &UserContext{IsAdmin: true}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSystemAdmin(tt.user); got != tt.want {
				t.Fatalf("IsSystemAdmin() = %v, want %v", got, tt.want)
			}
		})
	}
}
