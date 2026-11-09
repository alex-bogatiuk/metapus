package crypto_merchant_credit

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
)

func TestPrepareFixesTariffAndBalancesGross(t *testing.T) {
	receipt, merchant, token, actor := id.New(), id.New(), id.New(), id.New()
	c, err := Prepare(receipt, merchant, token, nil, types.MustCryptoAmount("1000"),
		Fee{Fixed: types.MustCryptoAmount("5"), PercentBP: 100, Min: types.ZeroCryptoAmount(), Max: types.ZeroCryptoAmount()}, true, actor, time.Now())
	require.NoError(t, err)
	require.Equal(t, "15", c.ProcessingFee.String())
	require.Equal(t, "985", c.NetAmount.String())
	require.Equal(t, StatusPrepared, c.Status)
	require.Equal(t, receipt, *c.BasisID)
}

func TestApprovalRequiresAnotherOperator(t *testing.T) {
	actor := id.New()
	c, err := Prepare(id.New(), id.New(), id.New(), nil, types.MustCryptoAmount("10"), Fee{}, true, actor, time.Now())
	require.NoError(t, err)
	require.Error(t, c.Approve(actor, time.Now()))
	require.NoError(t, c.Approve(id.New(), time.Now()))
	require.Equal(t, StatusPosted, c.Status)
	require.Error(t, c.Reject(id.New(), "late", time.Now()), "a decided credit cannot change")

	single, err := Prepare(id.New(), id.New(), id.New(), nil, types.MustCryptoAmount("10"), Fee{}, false, actor, time.Now())
	require.NoError(t, err)
	require.NoError(t, single.Approve(actor, time.Now()), "below the threshold one operator posts the credit")
}

func TestFeeAboveGrossIsRejected(t *testing.T) {
	_, err := Prepare(id.New(), id.New(), id.New(), nil, types.MustCryptoAmount("3"),
		Fee{Fixed: types.MustCryptoAmount("5")}, false, id.New(), time.Now())
	require.Error(t, err)
}
