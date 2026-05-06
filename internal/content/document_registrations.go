package content

import (
	"context"
	"fmt"

	"metapus/internal/core/numerator"
	"metapus/internal/domain"
	"metapus/internal/domain/audit"
	"metapus/internal/domain/catalogs/wallet"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/goods_issue"
	"metapus/internal/domain/documents/goods_receipt"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/http/v1/handlers"
	"metapus/internal/infrastructure/storage/postgres/catalog_repo"
	"metapus/internal/infrastructure/storage/postgres/document_repo"
	"metapus/internal/metadata"
)

func init() {
	// CryptoInvoice Status
	metadata.RegisterEnum[crypto_invoice.InvoiceStatus]([]metadata.EnumValue{
		{Value: "created", Label: "Создан"},
		{Value: "partially_paid", Label: "Частично оплачен"},
		{Value: "paid", Label: "Оплачен"},
		{Value: "confirmed", Label: "Подтверждён"},
		{Value: "expired", Label: "Истёк"},
		{Value: "cancelled", Label: "Отменён"},
	})

	// CryptoPayment Status
	metadata.RegisterEnum[crypto_payment.PaymentStatus]([]metadata.EnumValue{
		{Value: "detected", Label: "Обнаружен"},
		{Value: "confirming", Label: "Подтверждается"},
		{Value: "confirmed", Label: "Подтверждён"},
		{Value: "settled", Label: "Рассчитан"},
		{Value: "reorged", Label: "Реорганизация"},
	})

	// CryptoWithdrawal Status
	metadata.RegisterEnum[crypto_withdrawal.WithdrawalStatus]([]metadata.EnumValue{
		{Value: "created", Label: "Создан"},
		{Value: "signed", Label: "Подписан"},
		{Value: "broadcast", Label: "Отправлен"},
		{Value: "confirmed", Label: "Подтверждён"},
		{Value: "failed", Label: "Ошибка"},
	})

	// CryptoSweep Status
	metadata.RegisterEnum[crypto_sweep.SweepStatus]([]metadata.EnumValue{
		{Value: "created", Label: "Создан"},
		{Value: "signed", Label: "Подписан"},
		{Value: "broadcast", Label: "Отправлен"},
		{Value: "confirmed", Label: "Подтверждён"},
		{Value: "partial_failed", Label: "Частичная ошибка"},
	})
}

// ---------------------------------------------------------------------------
// GoodsReceipt
// ---------------------------------------------------------------------------

type GoodsReceiptRegistration struct{}

func (r *GoodsReceiptRegistration) RoutePrefix() string { return "goods-receipt" }
func (r *GoodsReceiptRegistration) Permission() string  { return "document:goods_receipt" }
func (r *GoodsReceiptRegistration) EntityName() string  { return "GoodsReceipt" }
func (r *GoodsReceiptRegistration) EntityLabel() string { return "Поступление товаров" }
func (r *GoodsReceiptRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Поступление товаров",
		Plural:   "Поступления товаров",
		NewLabel: "Новое поступление",
		Genitive: "поступления товаров",
	}
}
func (r *GoodsReceiptRegistration) EntityStruct() interface{} { return goods_receipt.GoodsReceipt{} }
func (r *GoodsReceiptRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}

func (r *GoodsReceiptRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewGoodsReceiptRepo()
	service := goods_receipt.NewService(repo, deps.PostingEngine, deps.Numerator, nil, deps.CurrencyResolver)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *goods_receipt.GoodsReceipt) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*goods_receipt.GoodsReceipt](
		domain.WithLogging[*goods_receipt.GoodsReceipt]("goods-receipt"),
		domain.WithEventLog[*goods_receipt.GoodsReceipt]("goods_receipt", deps.EventWriter),
		domain.WithOutboxEvents[*goods_receipt.GoodsReceipt]("goods_receipt", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewGoodsReceiptHandler(deps.BaseHandler, decorated, deps.PrintRegistry, deps.PrintRenderer, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// GoodsIssue
// ---------------------------------------------------------------------------

type GoodsIssueRegistration struct{}

func (r *GoodsIssueRegistration) RoutePrefix() string { return "goods-issue" }
func (r *GoodsIssueRegistration) Permission() string  { return "document:goods_issue" }
func (r *GoodsIssueRegistration) EntityName() string  { return "GoodsIssue" }
func (r *GoodsIssueRegistration) EntityLabel() string { return "Реализация товаров" }
func (r *GoodsIssueRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Реализация товаров",
		Plural:   "Реализации товаров",
		NewLabel: "Новая реализация",
		Genitive: "реализации товаров",
	}
}
func (r *GoodsIssueRegistration) EntityStruct() interface{} { return goods_issue.GoodsIssue{} }
func (r *GoodsIssueRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}

func (r *GoodsIssueRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewGoodsIssueRepo()
	service := goods_issue.NewService(repo, deps.PostingEngine, deps.Numerator, nil, deps.CurrencyResolver)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *goods_issue.GoodsIssue) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *goods_issue.GoodsIssue) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*goods_issue.GoodsIssue](
		domain.WithLogging[*goods_issue.GoodsIssue]("goods-issue"),
		domain.WithEventLog[*goods_issue.GoodsIssue]("goods_issue", deps.EventWriter),
		domain.WithOutboxEvents[*goods_issue.GoodsIssue]("goods_issue", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewGoodsIssueHandler(deps.BaseHandler, decorated, deps.PrintRegistry, deps.PrintRenderer, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoInvoice
// ---------------------------------------------------------------------------

type CryptoInvoiceRegistration struct{}

func (r *CryptoInvoiceRegistration) RoutePrefix() string { return "crypto-invoice" }
func (r *CryptoInvoiceRegistration) Permission() string  { return "document:crypto_invoice" }
func (r *CryptoInvoiceRegistration) EntityName() string  { return "CryptoInvoice" }
func (r *CryptoInvoiceRegistration) EntityLabel() string { return "Крипто-инвойс" }
func (r *CryptoInvoiceRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-инвойс",
		Plural:   "Крипто-инвойсы",
		NewLabel: "Новый крипто-инвойс",
		Genitive: "крипто-инвойса",
	}
}
func (r *CryptoInvoiceRegistration) EntityStruct() interface{} {
	return crypto_invoice.CryptoInvoice{}
}
func (r *CryptoInvoiceRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}

func (r *CryptoInvoiceRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewCryptoInvoiceRepo()
	service := crypto_invoice.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	// Pre-allocate repos outside hook closure (single allocation, safe for concurrent use).
	tokenRepo := catalog_repo.NewTokenRepo()
	walletRepo := catalog_repo.NewWalletRepo()
	walletSvc := wallet.NewService(walletRepo, numerator.Noop())

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)

		// Auto-lease a pool wallet for receiving payment.
		// Resolve token → network, then lease a free pool wallet.
		tok, err := tokenRepo.GetByID(ctx, doc.TokenID)
		if err != nil {
			return fmt.Errorf("resolve token for wallet lease: %w", err)
		}

		w, err := walletSvc.LeaseForInvoice(ctx, doc.ID, tok.NetworkID)
		if err != nil {
			return fmt.Errorf("lease wallet for invoice: %w", err)
		}

		doc.WalletID = &w.ID
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*crypto_invoice.CryptoInvoice](
		domain.WithLogging[*crypto_invoice.CryptoInvoice]("crypto-invoice"),
		domain.WithEventLog[*crypto_invoice.CryptoInvoice]("crypto_invoice", deps.EventWriter),
		domain.WithOutboxEvents[*crypto_invoice.CryptoInvoice]("crypto_invoice", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewCryptoInvoiceHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoPayment
// ---------------------------------------------------------------------------

type CryptoPaymentRegistration struct{}

func (r *CryptoPaymentRegistration) RoutePrefix() string { return "crypto-payment" }
func (r *CryptoPaymentRegistration) Permission() string  { return "document:crypto_payment" }
func (r *CryptoPaymentRegistration) EntityName() string  { return "CryptoPayment" }
func (r *CryptoPaymentRegistration) EntityLabel() string { return "Крипто-платёж" }
func (r *CryptoPaymentRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-платёж",
		Plural:   "Крипто-платежи",
		NewLabel: "Новый крипто-платёж",
		Genitive: "крипто-платежа",
	}
}
func (r *CryptoPaymentRegistration) EntityStruct() interface{} {
	return crypto_payment.CryptoPayment{}
}
func (r *CryptoPaymentRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id", "merchant": "merchant_id"}
}

func (r *CryptoPaymentRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewCryptoPaymentRepo()
	service := crypto_payment.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_payment.CryptoPayment) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *crypto_payment.CryptoPayment) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*crypto_payment.CryptoPayment](
		domain.WithLogging[*crypto_payment.CryptoPayment]("crypto-payment"),
		domain.WithEventLog[*crypto_payment.CryptoPayment]("crypto_payment", deps.EventWriter),
		domain.WithOutboxEvents[*crypto_payment.CryptoPayment]("crypto_payment", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewCryptoPaymentHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoWithdrawal
// ---------------------------------------------------------------------------

type CryptoWithdrawalRegistration struct{}

func (r *CryptoWithdrawalRegistration) RoutePrefix() string { return "crypto-withdrawal" }
func (r *CryptoWithdrawalRegistration) Permission() string  { return "document:crypto_withdrawal" }
func (r *CryptoWithdrawalRegistration) EntityName() string  { return "CryptoWithdrawal" }
func (r *CryptoWithdrawalRegistration) EntityLabel() string { return "Крипто-вывод" }
func (r *CryptoWithdrawalRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-вывод",
		Plural:   "Крипто-выводы",
		NewLabel: "Новый крипто-вывод",
		Genitive: "крипто-вывода",
	}
}
func (r *CryptoWithdrawalRegistration) EntityStruct() interface{} {
	return crypto_withdrawal.CryptoWithdrawal{}
}
func (r *CryptoWithdrawalRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id", "merchant": "merchant_id"}
}

func (r *CryptoWithdrawalRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewCryptoWithdrawalRepo()
	service := crypto_withdrawal.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_withdrawal.CryptoWithdrawal) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *crypto_withdrawal.CryptoWithdrawal) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*crypto_withdrawal.CryptoWithdrawal](
		domain.WithLogging[*crypto_withdrawal.CryptoWithdrawal]("crypto-withdrawal"),
		domain.WithEventLog[*crypto_withdrawal.CryptoWithdrawal]("crypto_withdrawal", deps.EventWriter),
		domain.WithOutboxEvents[*crypto_withdrawal.CryptoWithdrawal]("crypto_withdrawal", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewCryptoWithdrawalHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoSweep
// ---------------------------------------------------------------------------

type CryptoSweepRegistration struct{}

func (r *CryptoSweepRegistration) RoutePrefix() string { return "crypto-sweep" }
func (r *CryptoSweepRegistration) Permission() string  { return "document:crypto_sweep" }
func (r *CryptoSweepRegistration) EntityName() string  { return "CryptoSweep" }
func (r *CryptoSweepRegistration) EntityLabel() string { return "Крипто-свип" }
func (r *CryptoSweepRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-свип",
		Plural:   "Крипто-свипы",
		NewLabel: "Новый крипто-свип",
		Genitive: "крипто-свипа",
	}
}
func (r *CryptoSweepRegistration) EntityStruct() interface{} {
	return crypto_sweep.CryptoSweep{}
}
func (r *CryptoSweepRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}

func (r *CryptoSweepRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := document_repo.NewCryptoSweepRepo()
	service := crypto_sweep.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_sweep.CryptoSweep) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *crypto_sweep.CryptoSweep) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*crypto_sweep.CryptoSweep](
		domain.WithLogging[*crypto_sweep.CryptoSweep]("crypto-sweep"),
		domain.WithEventLog[*crypto_sweep.CryptoSweep]("crypto_sweep", deps.EventWriter),
		domain.WithOutboxEvents[*crypto_sweep.CryptoSweep]("crypto_sweep", deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return handlers.NewCryptoSweepHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}
