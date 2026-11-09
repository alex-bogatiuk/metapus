package crypto_worker

import (
	"context"
	"errors"
	"time"

	"metapus/internal/domain/crypto"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
)

// Internal watchers can emit only a provisional observation. There is no
// invoice confirmation loop for these transfers, so final settlement triggers
// an independent proof read before their resource/principal inventory is used.
func (p *CryptoProcessor) verifyFinalAMLInternalTransfers(ctx context.Context) error {
	events, err := crypto_repo.NewWorkerRuntimeRepo().ListFinalAMLInternalTransfers(ctx)
	if err != nil {
		return err
	}
	for _, event := range events {
		driver, err := p.drivers.Resolve(event.NetworkID)
		if err != nil {
			return err
		}
		verifier, ok := driver.(crypto.TransferVerifier)
		if !ok || !verifier.VerificationRequired() {
			continue
		}
		checkCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		proof, err := verifier.VerifyTransfer(checkCtx, event)
		cancel()
		if err != nil {
			if errors.Is(err, crypto.ErrVerificationMismatch) {
				return errors.Join(err, crypto_repo.NewBlockIngestionRepo().ReportIngestionFailure(ctx, event.NetworkID, "internal-finality", event.BlockNumber, "transfer_mismatch", true))
			}
			p.log.Warnw("AML internal transfer proof deferred", "network_id", event.NetworkID, "tx_hash", event.TxHash, "error", err)
			continue
		}
		if _, err := crypto_repo.NewBlockIngestionRepo().SaveVerification(ctx, proof); err != nil {
			return err
		}
	}
	return nil
}
