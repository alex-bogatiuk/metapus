package crypto

import (
	"context"

	"metapus/internal/core/apperror"
)

// AMLSigningSchemaV1 includes acceptance-bound intent V2, exact source-line
// membership and durable consumption of each line across approval changes.
// AML withdrawal lines permit attempt/generation 1 only.
const AMLSigningSchemaV1 = "aml_signing.v1"

type AMLSigningContract struct {
	Schema          string `json:"aml_signing_schema"`
	IntentSchema    string `json:"aml_intent_schema"`
	SourceLineBasis string `json:"aml_source_line_basis"`
}

func (c AMLSigningContract) Validate() error {
	if c.Schema != AMLSigningSchemaV1 || c.IntentSchema != MoneyMovementIntentV2 || c.SourceLineBasis != "CryptoAMLWithdrawalLine" {
		return apperror.NewConflict("family signer does not support the required AML signing contract")
	}
	return nil
}

// AMLSigner is separate from custody support: an older custody plugin must not
// receive new AML intents merely because its independent owner registry works.
type AMLSigner interface {
	CheckAMLReady(context.Context, NetworkRef) error
}
