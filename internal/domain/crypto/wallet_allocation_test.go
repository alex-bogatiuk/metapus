package crypto

import (
	"testing"
	"time"

	"metapus/internal/core/id"
	"metapus/internal/domain/documents/crypto_invoice"
)

func TestWalletAllocationCovers(t *testing.T) {
	leasedAt := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	cooldown := leasedAt.Add(10 * time.Minute)
	closed := leasedAt.Add(5 * time.Minute)
	open := WalletAllocation{LeasedAt: leasedAt}
	closedAlloc := WalletAllocation{
		LeasedAt:      leasedAt,
		ClosedAt:      &closed,
		CooldownUntil: &cooldown,
	}

	if !open.Covers(leasedAt) {
		t.Fatal("open allocation must cover leased_at")
	}
	if open.Covers(leasedAt.Add(-time.Second)) {
		t.Fatal("open allocation must not cover ts < leased_at")
	}
	if !open.Covers(leasedAt.Add(time.Hour)) {
		t.Fatal("open allocation must cover ts after leased_at")
	}
	if !closedAlloc.Covers(cooldown) {
		t.Fatal("closed allocation must include cooldown_until")
	}
	if closedAlloc.Covers(cooldown.Add(time.Nanosecond)) {
		t.Fatal("closed allocation must not cover after cooldown_until")
	}
	if closedAlloc.Covers(leasedAt.Add(-time.Second)) {
		t.Fatal("closed allocation must not cover before leased_at")
	}
}

func TestDecideMatch(t *testing.T) {
	tokenA := id.New()
	tokenB := id.New()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	closedAt := now.Add(time.Minute)
	cooldown := now.Add(2 * time.Minute)
	allocA := WalletAllocation{
		WalletID:      id.New(),
		InvoiceID:     id.New(),
		TokenID:       tokenA,
		LeasedAt:      now,
		ClosedAt:      &closedAt,
		CooldownUntil: &cooldown,
	}
	openB := WalletAllocation{
		WalletID:  allocA.WalletID,
		InvoiceID: id.New(),
		TokenID:   tokenA,
		LeasedAt:  cooldown.Add(time.Second),
	}

	matched := DecideMatch(nil, []WalletAllocation{allocA}, tokenA, now.Add(30*time.Second))
	if matched.Status != MatchMatched || matched.Allocation == nil || matched.Allocation.InvoiceID != allocA.InvoiceID {
		t.Fatalf("expected match to A, got %+v", matched)
	}

	wrongToken := DecideMatch(nil, []WalletAllocation{allocA}, tokenB, now.Add(30*time.Second))
	if wrongToken.Status != MatchUnmatched || wrongToken.Reason != MatchReasonWrongToken {
		t.Fatalf("expected wrong_token, got %+v", wrongToken)
	}

	overlap := DecideMatch(nil, []WalletAllocation{allocA, allocA}, tokenA, now.Add(30*time.Second))
	if overlap.Status != MatchNeedsReview || overlap.Reason != MatchReasonOverlappingWindows {
		t.Fatalf("expected overlapping_windows, got %+v", overlap)
	}

	beforeOpen := DecideMatch(&openB, nil, tokenA, openB.LeasedAt.Add(-time.Second))
	if beforeOpen.Status != MatchNeedsReview || beforeOpen.Reason != MatchReasonTimestampBeforeLease {
		t.Fatalf("expected timestamp_before_lease, got %+v", beforeOpen)
	}

	gap := DecideMatch(&openB, nil, tokenA, openB.LeasedAt.Add(time.Second))
	if gap.Status != MatchUnmatched || gap.Reason != MatchReasonNoAllocation {
		t.Fatalf("gap after A with covering empty and ts >= B.leased_at must be no_allocation, got %+v", gap)
	}

	// Current lease must not steal A's last second: B does not cover ts < B.leased_at.
	lastSecondA := cooldown
	covering := []WalletAllocation{allocA}
	last := DecideMatch(&openB, covering, tokenA, lastSecondA)
	if last.Status != MatchMatched || last.Allocation == nil || last.Allocation.InvoiceID != allocA.InvoiceID {
		t.Fatalf("ts in A's last second must credit A, got %+v", last)
	}
}

func TestInvoiceIsLateTerminal(t *testing.T) {
	if !InvoiceIsLateTerminal(crypto_invoice.InvoiceStatusExpired) {
		t.Fatal("expired is late-terminal")
	}
	if InvoiceIsLateTerminal(crypto_invoice.InvoiceStatusPaid) {
		t.Fatal("paid is not late-terminal")
	}
}
