package outbound_repo

import (
	"os"
	"strings"
	"testing"
)

func TestMarkWalletsFreeAfterSweepSQLDoesNotClearCooldownUntil(t *testing.T) {
	src, err := os.ReadFile("outbound_repo_sweeps.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func (r *OutboundRepo) MarkWalletsFreeAfterSweep")
	if start < 0 {
		t.Fatal("MarkWalletsFreeAfterSweep not found")
	}
	rest := text[start:]
	end := strings.Index(rest[1:], "\nfunc ")
	if end < 0 {
		end = len(rest) - 1
	}
	body := rest[:end+1]
	if strings.Contains(body, "cooldown_until") {
		t.Fatal("MarkWalletsFreeAfterSweep must not write cooldown_until")
	}
}
