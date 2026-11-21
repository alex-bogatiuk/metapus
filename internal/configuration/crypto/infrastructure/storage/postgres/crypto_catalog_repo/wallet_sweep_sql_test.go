package crypto_catalog_repo

import (
	"os"
	"strings"
	"testing"
)

func TestBeginSweepSQLDoesNotTouchCooldownUntil(t *testing.T) {
	src, err := os.ReadFile("wallet.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func (r *WalletRepo) BeginSweep")
	if start < 0 {
		t.Fatal("BeginSweep not found")
	}
	body := text[start:]
	if end := strings.Index(body[1:], "\nfunc "); end > 0 {
		body = body[:end+1]
	}
	if strings.Contains(body, "cooldown_until") {
		t.Fatal("BeginSweep must update status only")
	}
	if !strings.Contains(body, "sweep_pending") && !strings.Contains(body, "WalletStatusSweepPending") {
		t.Fatal("BeginSweep must set sweep_pending")
	}
}
