package crypto_document_repo

import (
	"os"
	"strings"
	"testing"
)

func TestExpirationOrchestratorLocksInvoiceBeforeWallet(t *testing.T) {
	src, err := os.ReadFile("../../../crypto_worker/processor.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	start := strings.Index(text, "func (p *CryptoProcessor) expireInvoicesWithCount")
	if start < 0 {
		t.Fatal("expireInvoicesWithCount not found")
	}
	body := text[start:]
	walletLock := strings.Index(body, "LockWalletForInvoice")
	invoiceLock := strings.Index(body, "ExpireInvoiceInTransaction")
	if walletLock < 0 || invoiceLock < 0 {
		t.Fatal("expire path must invoke the invoice lifecycle and lock the wallet")
	}
	if invoiceLock > walletLock {
		t.Fatal("invoice lifecycle lock must precede wallet lock")
	}
}
