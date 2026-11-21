package tenant

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreateTenantInputValidateCanonicalSlug(t *testing.T) {
	input := CreateTenantInput{
		Configuration: "probe",
		Slug:          "  Acme-Europe  ",
		DisplayName:   " ACME Europe ",
	}

	require.NoError(t, input.Validate())
	require.Equal(t, "acme-europe", input.Slug)
	require.Equal(t, "ACME Europe", input.DisplayName)
	require.Equal(t, PlanStandard, input.Plan)
	require.Equal(t, "localhost", input.DBHost)
	require.Equal(t, 5432, input.DBPort)
}

func TestCreateTenantInputValidateRejectsUnsafeSlugs(t *testing.T) {
	for _, slug := range []string{
		"-acme",
		"acme-",
		"acme_eu",
		"acme eu",
		"acme;drop-database",
		strings.Repeat("a", 64),
	} {
		t.Run(slug, func(t *testing.T) {
			input := CreateTenantInput{Configuration: "probe", Slug: slug, DisplayName: "ACME"}
			require.Error(t, input.Validate())
		})
	}
}

func TestGenerateDatabaseNameUsesTenantID(t *testing.T) {
	name, err := GenerateDatabaseName("0195d5a8-f32a-7bc8-a901-1234567890ab")

	require.NoError(t, err)
	require.Equal(t, "mt_0195d5a8f32a7bc8a9011234567890ab", name)
	require.Len(t, name, 35)
}
