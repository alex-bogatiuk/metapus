package crypto

import (
	"context"
	"errors"
	"testing"
	"time"

	"metapus/internal/configuration/crypto/domain/registers/crypto_platform_cost"
	"metapus/internal/configuration/crypto/domain/registers/cryptoposting"
	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/posting"
)

func validCustodyAdjustment() *CryptoCustodyAdjustment {
	return &CryptoCustodyAdjustment{
		ID:                id.New(),
		ExternalOutflowID: id.New(),
		NetworkID:         id.New(),
		WalletID:          id.New(),
		TokenID:           id.New(),
		Amount:            types.NewRawAmountFromInt64(1250),
		Status:            "pending_approval",
		MakerID:           id.New(),
	}
}

func TestCustodyLossV2UsesStableMovementIdentityAndOnePeriod(t *testing.T) {
	a := validCustodyAdjustment()
	if err := a.CanPost(context.Background()); err == nil {
		t.Fatal("v2 requires admitted period")
	}
	a.RecognitionPeriod = time.Now().UTC().Truncate(time.Microsecond)
	if err := a.CanPost(context.Background()); err != nil {
		t.Fatal(err)
	}
	first, err := a.GenerateCryptoBalanceMovements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.GenerateCryptoBalanceMovements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cost, err := a.GenerateCryptoPlatformCostMovements(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first[0].LineID != second[0].LineID || first[0].LineID != a.PrincipalLineID() || first[0].LineID == cost[0].LineID {
		t.Fatal("unstable principal/cost identities")
	}
	if !first[0].Period.Equal(a.RecognitionPeriod) || !cost[0].Period.Equal(a.RecognitionPeriod) {
		t.Fatal("principal/cost period differs from binding")
	}
}

func TestCryptoCustodyAdjustmentGeneratesOnlyCustodyLossMovements(t *testing.T) {
	adjustment := validCustodyAdjustment()
	adjustment.RecognitionPeriod = time.Now().UTC().Truncate(time.Microsecond)
	if err := adjustment.CanPost(context.Background()); err != nil {
		t.Fatalf("valid adjustment cannot post: %v", err)
	}

	balance, err := adjustment.GenerateCryptoBalanceMovements(context.Background())
	if err != nil {
		t.Fatalf("generate wallet balance movement: %v", err)
	}
	if len(balance) != 1 {
		t.Fatalf("expected one wallet movement, got %d", len(balance))
	}
	if balance[0].RecordType != entity.RecordTypeExpense || balance[0].WalletID != adjustment.WalletID ||
		balance[0].TokenID != adjustment.TokenID || !balance[0].Amount.Equal(adjustment.Amount) {
		t.Fatalf("unexpected wallet movement: %#v", balance[0])
	}
	if balance[0].RecorderType != CryptoCustodyAdjustmentDocumentType || balance[0].RecorderVersion != 1 {
		t.Fatalf("unexpected wallet movement identity: %#v", balance[0].MovementBase)
	}

	costs, err := adjustment.GenerateCryptoPlatformCostMovements(context.Background())
	if err != nil {
		t.Fatalf("generate platform cost movement: %v", err)
	}
	if len(costs) != 1 {
		t.Fatalf("expected one platform cost movement, got %d", len(costs))
	}
	cost := costs[0]
	if cost.RecordType != entity.RecordTypeExpense || cost.NetworkID != adjustment.NetworkID ||
		cost.CostTokenID != adjustment.TokenID || !cost.Amount.Equal(adjustment.Amount) ||
		cost.CostType != crypto_platform_cost.PlatformCostTypeCustodyLoss || cost.Purpose != crypto_platform_cost.PlatformCostPurposeCustodyLoss {
		t.Fatalf("unexpected custody-loss cost movement: %#v", cost)
	}
	if _, changesMerchantBalance := any(adjustment).(cryptoposting.CryptoMerchantBalanceMovementSource); changesMerchantBalance {
		t.Fatal("custody adjustment must not generate merchant-balance movements")
	}
}

func TestCryptoCustodyAdjustmentPostingStateIsImmutable(t *testing.T) {
	adjustment := validCustodyAdjustment()
	adjustment.MarkPosted()
	if !adjustment.IsPosted() || adjustment.PostedVersion != 1 {
		t.Fatalf("expected posted version 1, got status=%q version=%d", adjustment.Status, adjustment.PostedVersion)
	}
	if err := adjustment.CanPost(context.Background()); err == nil {
		t.Fatal("posted custody adjustment was accepted for reposting")
	}

	err := posting.NewEngine(nil).Unpost(context.Background(), adjustment, func(context.Context) error {
		t.Fatal("unpost callback must not execute")
		return nil
	})
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) || appErr.Code != apperror.CodeConflict {
		t.Fatalf("expected immutable adjustment conflict, got %T: %v", err, err)
	}
	if !adjustment.IsPosted() || adjustment.PostedVersion != 1 {
		t.Fatalf("unpost attempt changed adjustment: status=%q version=%d", adjustment.Status, adjustment.PostedVersion)
	}
}

func TestCryptoCustodyAdjustmentRejectsIncompleteOrNonPositiveDocument(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*CryptoCustodyAdjustment)
	}{
		{name: "missing wallet", mutate: func(a *CryptoCustodyAdjustment) { a.WalletID = id.Nil() }},
		{name: "zero amount", mutate: func(a *CryptoCustodyAdjustment) { a.Amount = types.ZeroRawAmount() }},
		{name: "rejected", mutate: func(a *CryptoCustodyAdjustment) { a.Status = "rejected" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adjustment := validCustodyAdjustment()
			tt.mutate(adjustment)
			if err := adjustment.CanPost(context.Background()); err == nil {
				t.Fatal("invalid custody adjustment was accepted for posting")
			}
		})
	}
}
