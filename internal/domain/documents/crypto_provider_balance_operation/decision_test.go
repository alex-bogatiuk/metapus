package crypto_provider_balance_operation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestProviderOpeningDecisionPolicyMatrix(t *testing.T) {
	maker, checker := id.New(), id.New()
	for _, admin := range []bool{false, true} {
		for _, permitted := range []bool{false, true} {
			for _, self := range []bool{false, true} {
				actor := checker
				if self {
					actor = maker
				}
				auth, err := AuthorizeDecision(maker, actor, admin, permitted)
				if (!admin && self) || (!admin && !permitted) {
					require.Error(t, err)
					continue
				}
				require.NoError(t, err)
				require.Equal(t, 1, auth.PolicyVersion)
				if self {
					require.Equal(t, DecisionAdminSelf, auth.Mode)
				} else {
					require.Equal(t, DecisionIndependent, auth.Mode)
				}
			}
		}
	}
	_, err := AuthorizeDecision(id.Nil(), checker, true, true)
	require.Error(t, err)
	_, err = AuthorizeDecision(maker, id.Nil(), true, true)
	require.Error(t, err)
}

func testOpening() *CryptoProviderBalanceOperation {
	observation, incident := id.New(), id.New()
	doc := &CryptoProviderBalanceOperation{Document: entity.NewDocument(), ProviderAccountID: id.New(), TokenID: id.New(),
		ProviderObservationID: &observation, OperationType: OperationTypeOpening, RecordType: entity.RecordTypeReceipt,
		Amount: types.NewRawAmountFromInt64(1), ExternalRef: "opening-observation:" + observation.String(), Status: StatusPendingApproval}
	doc.BasisID, doc.BasisType, doc.CreatedBy, doc.Description = &incident, "CryptoProviderBalanceIncident", id.New(), "verified observation"
	return doc
}

func TestProviderOpeningAuthorizedDecisionAndCanPost(t *testing.T) {
	for _, self := range []bool{true, false} {
		for _, approve := range []bool{true, false} {
			doc := testOpening()
			actor := id.New()
			if self {
				actor = doc.CreatedBy
			}
			auth, err := AuthorizeDecision(doc.CreatedBy, actor, true, true)
			require.NoError(t, err)
			forged := auth
			if self {
				forged.Mode = DecisionIndependent
			} else {
				forged.Mode = DecisionAdminSelf
			}
			require.Error(t, doc.CheckDecision(forged, 1, "checked"))
			require.Error(t, doc.CheckDecision(auth, 2, "checked"))
			require.Error(t, doc.CheckDecision(auth, 1, strings.Repeat("я", 1001)))
			require.Error(t, doc.CheckDecision(auth, 1, "  "))
			if approve {
				require.NoError(t, doc.Approve(auth, 1, " checked ", time.Now()))
				require.NoError(t, doc.CanPost(context.Background()))
				doc.DecisionMode = forged.Mode
				require.Error(t, doc.CanPost(context.Background()))
				doc.DecisionMode = auth.Mode
				doc.MarkPosted()
				require.Error(t, doc.CanPost(context.Background()))
			} else {
				require.NoError(t, doc.Reject(auth, 1, " checked ", time.Now()))
				require.Error(t, doc.CanPost(context.Background()))
			}
			require.Equal(t, auth.Mode, doc.DecisionMode)
			require.Equal(t, 1, doc.DecisionPolicyVersion)
			require.Error(t, doc.CheckDecision(auth, 1, "again"))
		}
	}
}
