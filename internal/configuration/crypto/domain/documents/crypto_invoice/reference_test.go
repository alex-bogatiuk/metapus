package crypto_invoice

import (
	"fmt"
	"strings"
	"testing"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/stretchr/testify/require"
)

func TestOrderReferenceValidationPreservesUnicodeAndCase(t *testing.T) {
	for _, value := range []string{"ORD-99881", "ord-99881", "00001", strings.Repeat("Я", 255), "SAP,\"45\"\n001"} {
		require.NoError(t, ValidateOrderReference(value))
	}
	for _, value := range []string{"", " ", " ORD-1", "ORD-1\n", "\u00a0ORD-1", strings.Repeat("Я", 256)} {
		require.Error(t, ValidateOrderReference(value))
	}
}

func TestReferenceRejectsNULWithValidationError(t *testing.T) {
	for name, validation := range map[string]func() error{
		"orderId":      func() error { return ValidateOrderReference("ORD\x00X") },
		"externalId":   func() error { return ValidateExternalReference("ERP\x00X") },
		"metadata key": func() error { return ValidateMetadata(map[string]string{"sap\x00doc": "45001"}) },
		"metadata value": func() error {
			return ValidateMetadata(map[string]string{"sap_doc": "45\x00001"})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var appErr *apperror.AppError
			require.ErrorAs(t, validation(), &appErr)
			require.Equal(t, 400, appErr.HTTPStatus)
			require.Equal(t, apperror.CodeValidation, appErr.Code)
		})
	}
}

func TestReferenceMetadataUsesByteLimits(t *testing.T) {
	require.NoError(t, ValidateMetadata(map[string]string{strings.Repeat("Я", 20): strings.Repeat("Я", 250)}))
	require.Error(t, ValidateMetadata(map[string]string{strings.Repeat("Я", 21): "value"}))
	require.Error(t, ValidateMetadata(map[string]string{"key": strings.Repeat("Я", 251)}))
	require.Error(t, ValidateMetadata(map[string]string{" ": "value"}))
	metadata := map[string]string{}
	for i := range 51 {
		metadata[fmt.Sprint(i)] = "value"
	}
	require.Error(t, ValidateMetadata(metadata))
}
