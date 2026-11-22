package sdk

import "testing"

func TestCompatible(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		required string
		ok       bool
	}{
		{APIVersion, true},
		{"3.0.0", true},
		{"v3.0.7", true},
		{"3.1.0", true},
		{"3.2.0", false},
		{"2.9.0", false},
		{"4.0.0", false},
		{"3.0", false},
		{"three", false},
	} {
		if err := Compatible(tc.required); (err == nil) != tc.ok {
			t.Errorf("Compatible(%q) = %v, want ok=%v", tc.required, err, tc.ok)
		}
	}
}
