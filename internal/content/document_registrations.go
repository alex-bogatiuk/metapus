package content

import (
	"context"

	"github.com/gin-gonic/gin"

	"metapus/internal/application/command"
	"metapus/internal/application/cryptoapp"
	"metapus/internal/application/portalapp"
	"metapus/internal/domain"
	"metapus/internal/domain/audit"
	"metapus/internal/domain/documents/crypto_invoice"
	"metapus/internal/domain/documents/crypto_network_operation"
	"metapus/internal/domain/documents/crypto_payment"
	"metapus/internal/domain/documents/crypto_provider_balance_operation"
	"metapus/internal/domain/documents/crypto_resource_rental"
	"metapus/internal/domain/documents/crypto_sweep"
	"metapus/internal/domain/documents/crypto_withdrawal"
	"metapus/internal/domain/documents/treasury_withdrawal"
	"metapus/internal/domain/documents/withdrawal_request"
	"metapus/internal/domain/printing"
	"metapus/internal/domain/registers/crypto_merchant_balance"
	"metapus/internal/domain/registers/exchange_rate"
	"metapus/internal/infrastructure/http/cryptohttp/cryptohandlers"
	v1 "metapus/internal/infrastructure/http/v1"
	"metapus/internal/infrastructure/storage/postgres"
	"metapus/internal/infrastructure/storage/postgres/crypto_document_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_register_repo"
	"metapus/internal/infrastructure/storage/postgres/crypto_repo"
	"metapus/internal/infrastructure/storage/postgres/money_movement_repo"
	"metapus/internal/infrastructure/storage/postgres/portal_repo"
	"metapus/internal/infrastructure/storage/postgres/register_repo"
	"metapus/internal/metadata"
)

func init() {
	metadata.RegisterEnum[crypto_network_operation.OperationType]([]metadata.EnumValue{
		{Value: string(crypto_network_operation.OperationTypeWalletActivation), Label: "Активация кошелька"},
	})
	metadata.RegisterEnum[crypto_network_operation.Status]([]metadata.EnumValue{
		{Value: string(crypto_network_operation.StatusBroadcast), Label: "Отправлена"},
		{Value: string(crypto_network_operation.StatusSucceeded), Label: "Выполнена"},
		{Value: string(crypto_network_operation.StatusReverted), Label: "Отменена сетью"},
	})
	metadata.RegisterEnum[crypto_resource_rental.Status]([]metadata.EnumValue{
		{Value: "created", Label: "Создана"}, {Value: "ordering", Label: "Заказывается"},
		{Value: "delegated", Label: "Делегирована"}, {Value: "active", Label: "Активна"},
		{Value: "reclaiming", Label: "Возвращается"}, {Value: "closed", Label: "Закрыта"},
		{Value: "failed", Label: "Ошибка"}, {Value: "held", Label: "Удержана"},
	})
	// CryptoInvoice Status
	metadata.RegisterEnum[crypto_invoice.InvoiceStatus]([]metadata.EnumValue{
		{Value: "created", Label: "Создан"},
		{Value: "partially_paid", Label: "Частично оплачен"},
		{Value: "paid", Label: "Оплачен"},
		{Value: "completed", Label: "Завершён"},
		{Value: "underpaid", Label: "Недоплата"},
		{Value: "reversed", Label: "Отозван"},
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
		{Value: string(crypto_sweep.SweepStatusCreated), Label: "Создан"},
		{Value: string(crypto_sweep.SweepStatusSigned), Label: "Подписан"},
		{Value: string(crypto_sweep.SweepStatusBroadcast), Label: "Отправлен"},
		{Value: string(crypto_sweep.SweepStatusConfirmed), Label: "Подтверждён"},
		{Value: string(crypto_sweep.SweepStatusPartialFailed), Label: "Частичная ошибка"},
		{Value: string(crypto_sweep.SweepStatusHeld), Label: "Требует сверки"},
	})

	metadata.RegisterEnum[treasury_withdrawal.Status]([]metadata.EnumValue{
		{Value: "pending_signature", Label: "Ожидает подписи"},
		{Value: "pending_approval", Label: "Ожидает утверждения"},
		{Value: "approved", Label: "Утверждён"},
		{Value: "signing", Label: "Подписывается"},
		{Value: "broadcast", Label: "Отправлен"},
		{Value: "confirmed", Label: "Подтверждён"},
		{Value: "held", Label: "Требует сверки"},
		{Value: "rejected", Label: "Отклонён"},
		{Value: "failed", Label: "Ошибка"},
	})

	metadata.RegisterEnum[withdrawal_request.Status]([]metadata.EnumValue{
		{Value: "pending_signature", Label: "Ожидает подписи"},
		{Value: "pending_approval", Label: "Ожидает утверждения"},
		{Value: "approved", Label: "Утверждена"},
		{Value: "signing", Label: "Подписывается"},
		{Value: "broadcast", Label: "Отправлена"},
		{Value: "confirmed", Label: "Подтверждена"},
		{Value: "held", Label: "Требует сверки"},
		{Value: "rejected", Label: "Отклонена"},
		{Value: "failed", Label: "Ошибка"},
	})
}

// ---------------------------------------------------------------------------
// CryptoNetworkOperation
// ---------------------------------------------------------------------------

type CryptoNetworkOperationRegistration struct {
	v1.EntityRegistration[crypto_network_operation.CryptoNetworkOperation]
}

func (r *CryptoNetworkOperationRegistration) EntityLabel() string {
	return "Сетевая криптооперация"
}
func (r *CryptoNetworkOperationRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Сетевая криптооперация", Plural: "Сетевые криптооперации",
		Genitive: "сетевой криптооперации",
	}
}
func (r *CryptoNetworkOperationRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}

// DocumentCapabilities: the activation workflow creates and posts network
// operations; users only read them.
func (r *CryptoNetworkOperationRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, Movements: true, RelatedDocuments: true}
}
func (r *CryptoNetworkOperationRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	service := crypto_network_operation.NewService(crypto_document_repo.NewCryptoNetworkOperationRepo(), deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)
	return cryptohandlers.NewCryptoNetworkOperationHandler(deps.BaseHandler, service,
		deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo, deps.RelatedDocFinder)
}

// ---------------------------------------------------------------------------
// CryptoProviderBalanceOperation
// ---------------------------------------------------------------------------

type CryptoProviderBalanceOperationRegistration struct {
	v1.EntityRegistration[crypto_provider_balance_operation.CryptoProviderBalanceOperation]
}

func (r *CryptoProviderBalanceOperationRegistration) EntityLabel() string {
	return "Операция баланса крипто-провайдера"
}
func (r *CryptoProviderBalanceOperationRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Операция баланса провайдера", Plural: "Операции баланса провайдера", NewLabel: "Новая операция", Genitive: "операции баланса провайдера"}
}
func (r *CryptoProviderBalanceOperationRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}
func (r *CryptoProviderBalanceOperationRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, Movements: true, ExportList: true}
}
func (r *CryptoProviderBalanceOperationRegistration) DocumentActions() []metadata.EntityActionDef {
	return []metadata.EntityActionDef{
		{Key: "approve", Label: "Одобрить", Method: "POST", Path: "/:id/approve", Scope: metadata.EntityActionScopeItem, Permission: r.Permission() + ":approve", Idempotent: true, AllowedStates: []string{crypto_provider_balance_operation.StatusPendingApproval},
			UI: &metadata.EntityActionUI{FormToolbar: true}, Input: &metadata.EntityActionInput{Kind: "reasonedDecision", SummaryFields: []string{"number", "externalRef", "amount", "tokenId"}, ReasonMaxBytes: 2000}},
		{Key: "reject", Label: "Отклонить", Method: "POST", Path: "/:id/reject", Scope: metadata.EntityActionScopeItem, Permission: r.Permission() + ":reject", Idempotent: true, Destructive: true, AllowedStates: []string{crypto_provider_balance_operation.StatusPendingApproval},
			UI: &metadata.EntityActionUI{FormToolbar: true}, Input: &metadata.EntityActionInput{Kind: "reasonedDecision", SummaryFields: []string{"number", "externalRef", "amount", "tokenId"}, ReasonMaxBytes: 2000}},
	}
}
func (r *CryptoProviderBalanceOperationRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoProviderBalanceOperationRepo()
	service := crypto_provider_balance_operation.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)
	workflow := cryptoapp.NewProviderBalanceWorkflowService(repo, crypto_repo.NewProviderBalanceWorkflowRepo(), deps.Numerator, deps.PostingEngine,
		command.NewExecutor(postgres.NewContextTxManager(), postgres.NewCommandRepository()), deps.StepUpChecker,
		crypto_repo.NewProviderOpeningAuthorityRepo(), postgres.NewEventLogRepo())
	return cryptohandlers.NewCryptoProviderBalanceOperationHandler(deps.BaseHandler, service, workflow, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoResourceRental
// ---------------------------------------------------------------------------

type CryptoResourceRentalRegistration struct {
	v1.EntityRegistration[crypto_resource_rental.CryptoResourceRental]
}

func (r *CryptoResourceRentalRegistration) EntityLabel() string {
	return "Аренда TRON-ресурсов"
}
func (r *CryptoResourceRentalRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{Singular: "Аренда TRON-ресурсов", Plural: "Аренды TRON-ресурсов", Genitive: "аренды TRON-ресурсов"}
}
func (r *CryptoResourceRentalRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}
func (r *CryptoResourceRentalRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, RelatedDocuments: true, Movements: true}
}
func (r *CryptoResourceRentalRegistration) DocumentActions() []metadata.EntityActionDef {
	permission := r.Permission() + ":post"
	return []metadata.EntityActionDef{
		{Key: "reclaim", Label: "Вернуть ресурс", Method: "POST", Path: "/:id/reclaim", Scope: metadata.EntityActionScopeItem, Permission: permission, Idempotent: true, AllowedStates: []string{"active"}},
		{Key: "retry", Label: "Повторить", Method: "POST", Path: "/:id/retry", Scope: metadata.EntityActionScopeItem, Permission: permission, Idempotent: true, AllowedStates: []string{"held"}},
	}
}
func (r *CryptoResourceRentalRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoResourceRentalRepo()
	service := crypto_resource_rental.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)
	workflow := cryptoapp.NewResourceRentalWorkflowService(crypto_repo.NewResourceRentalWorkflowRepo())
	return cryptohandlers.NewCryptoResourceRentalHandler(deps.BaseHandler, service, workflow, deps.RelatedDocFinder,
		deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoInvoice
// ---------------------------------------------------------------------------

type CryptoInvoiceRegistration struct {
	v1.EntityRegistration[crypto_invoice.CryptoInvoice]
	services Services
}

func (r *CryptoInvoiceRegistration) EntityLabel() string { return "Крипто-инвойс" }
func (r *CryptoInvoiceRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-инвойс",
		Plural:   "Крипто-инвойсы",
		NewLabel: "Новый крипто-инвойс",
		Genitive: "крипто-инвойса",
	}
}
func (r *CryptoInvoiceRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}

var _ v1.PrintFormsProvider = (*CryptoInvoiceRegistration)(nil)

func (r *CryptoInvoiceRegistration) PrintForms() []printing.PrintFormDef {
	return cryptohandlers.CryptoInvoicePrintForms()
}

func (r *CryptoInvoiceRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoInvoiceRepo()
	service := crypto_invoice.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	walletLease := NewInvoiceWalletLease()
	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return walletLease.Apply(ctx, doc)
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *crypto_invoice.CryptoInvoice) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*crypto_invoice.CryptoInvoice](
		domain.WithLogging[*crypto_invoice.CryptoInvoice]("crypto-invoice"),
		domain.WithEventLog[*crypto_invoice.CryptoInvoice](crypto_invoice.Definition.Key, deps.EventWriter),
		domain.WithOutboxEvents[*crypto_invoice.CryptoInvoice](crypto_invoice.Definition.Key, deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)
	if r.services.CheckoutRefs != nil {
		decorated = cryptoapp.NewCheckoutInvoiceService(decorated, r.services.CheckoutRefs)
	}

	return cryptohandlers.NewCryptoInvoiceHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo,
		cryptohandlers.DocumentPrintDeps{Registry: deps.PrintRegistry, Renderer: deps.PrintRenderer, PDFRenderer: deps.PDFRenderer})
}

// ---------------------------------------------------------------------------
// CryptoPayment
// ---------------------------------------------------------------------------

type CryptoPaymentRegistration struct {
	v1.EntityRegistration[crypto_payment.CryptoPayment]
}

func (r *CryptoPaymentRegistration) EntityLabel() string { return "Крипто-платёж" }
func (r *CryptoPaymentRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-платёж",
		Plural:   "Крипто-платежи",
		NewLabel: "Новый крипто-платёж",
		Genitive: "крипто-платежа",
	}
}
func (r *CryptoPaymentRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id", "merchant": "merchant_id"}
}
func (r *CryptoPaymentRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, RelatedDocuments: true, Movements: true}
}

func (r *CryptoPaymentRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoPaymentRepo()
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
		domain.WithEventLog[*crypto_payment.CryptoPayment](crypto_payment.Definition.Key, deps.EventWriter),
		domain.WithOutboxEvents[*crypto_payment.CryptoPayment](crypto_payment.Definition.Key, deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return cryptohandlers.NewCryptoPaymentHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoWithdrawal
// ---------------------------------------------------------------------------

type CryptoWithdrawalRegistration struct {
	v1.EntityRegistration[crypto_withdrawal.CryptoWithdrawal]
}

func (r *CryptoWithdrawalRegistration) EntityLabel() string { return "Крипто-вывод" }
func (r *CryptoWithdrawalRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-вывод",
		Plural:   "Крипто-выводы",
		NewLabel: "Новый крипто-вывод",
		Genitive: "крипто-вывода",
	}
}
func (r *CryptoWithdrawalRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id", "merchant": "merchant_id"}
}
func (r *CryptoWithdrawalRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, RelatedDocuments: true, Movements: true}
}

func (r *CryptoWithdrawalRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoWithdrawalRepo()
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
		domain.WithEventLog[*crypto_withdrawal.CryptoWithdrawal](crypto_withdrawal.Definition.Key, deps.EventWriter),
		domain.WithOutboxEvents[*crypto_withdrawal.CryptoWithdrawal](crypto_withdrawal.Definition.Key, deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return cryptohandlers.NewCryptoWithdrawalHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// CryptoSweep
// ---------------------------------------------------------------------------

type CryptoSweepRegistration struct {
	v1.EntityRegistration[crypto_sweep.CryptoSweep]
}

func (r *CryptoSweepRegistration) EntityLabel() string { return "Крипто-свип" }
func (r *CryptoSweepRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Крипто-свип",
		Plural:   "Крипто-свипы",
		NewLabel: "Новый крипто-свип",
		Genitive: "крипто-свипа",
	}
}
func (r *CryptoSweepRegistration) RLSDimensions() map[string]string {
	return map[string]string{"organization": "organization_id"}
}
func (r *CryptoSweepRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{List: true, Read: true, ExportList: true, RelatedDocuments: true, Movements: true}
}
func (r *CryptoSweepRegistration) DocumentActions() []metadata.EntityActionDef {
	permission := r.Permission() + ":post"
	return []metadata.EntityActionDef{
		{Key: "retry", Label: "Повторить свип", Method: "POST", Path: "/:id/retry", Scope: metadata.EntityActionScopeItem, Permission: permission, Idempotent: true, AllowedStates: []string{string(crypto_sweep.SweepStatusHeld)}},
	}
}

func (r *CryptoSweepRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewCryptoSweepRepo()
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
		domain.WithEventLog[*crypto_sweep.CryptoSweep](crypto_sweep.Definition.Key, deps.EventWriter),
		domain.WithOutboxEvents[*crypto_sweep.CryptoSweep](crypto_sweep.Definition.Key, deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return cryptohandlers.NewCryptoSweepHandler(deps.BaseHandler, decorated, service, repo, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo)
}

// ---------------------------------------------------------------------------
// TreasuryWithdrawal
// ---------------------------------------------------------------------------

type TreasuryWithdrawalRegistration struct {
	v1.EntityRegistration[treasury_withdrawal.TreasuryWithdrawal]
	services Services
}

func (r *TreasuryWithdrawalRegistration) EntityLabel() string {
	return "Казначейский вывод"
}
func (r *TreasuryWithdrawalRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Казначейский вывод",
		Plural:   "Казначейские выводы",
		NewLabel: "Новый казначейский вывод",
		Genitive: "казначейского вывода",
	}
}
func (r *TreasuryWithdrawalRegistration) RLSDimensions() map[string]string {
	return map[string]string{}
}

func (r *TreasuryWithdrawalRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{
		List:             true,
		Read:             true,
		Create:           true,
		Update:           true,
		Delete:           true,
		ExportList:       true,
		RelatedDocuments: true,
		Movements:        true,
	}
}

func (r *TreasuryWithdrawalRegistration) DocumentActions() []metadata.EntityActionDef {
	permission := r.Permission()
	return []metadata.EntityActionDef{
		{
			Key:           "approve",
			Label:         "Утвердить",
			Method:        "POST",
			Path:          "/:id/approve",
			Scope:         metadata.EntityActionScopeItem,
			Permission:    permission + ":post",
			Idempotent:    true,
			AllowedStates: []string{string(treasury_withdrawal.StatusPendingApproval)},
		},
		{
			Key:           "reject",
			Label:         "Отклонить",
			Method:        "POST",
			Path:          "/:id/reject",
			Scope:         metadata.EntityActionScopeItem,
			Permission:    permission + ":post",
			Idempotent:    true,
			Destructive:   true,
			AllowedStates: []string{string(treasury_withdrawal.StatusPendingSignature), string(treasury_withdrawal.StatusPendingApproval), string(treasury_withdrawal.StatusApproved)},
		},
		{
			Key: "retry_signing", Label: "Повторить подписание", Method: "POST", Path: "/:id/retry-signing",
			Scope: metadata.EntityActionScopeItem, Permission: permission + ":post",
			Idempotent: true, AllowedStates: []string{string(treasury_withdrawal.StatusPendingSignature)},
		},
		{
			Key:        "addresses",
			Label:      "Адреса казначейских выводов",
			Method:     "GET",
			Path:       "/addresses",
			Scope:      metadata.EntityActionScopeCollection,
			Permission: permission + ":read",
			Idempotent: true,
		},
		{
			Key:        "create_address",
			Label:      "Добавить адрес казначейского вывода",
			Method:     "POST",
			Path:       "/addresses",
			Scope:      metadata.EntityActionScopeCollection,
			Permission: permission + ":create",
			Idempotent: true,
		},
		{
			Key:        "availability",
			Label:      "Доступность средств",
			Method:     "GET",
			Path:       "/availability",
			Scope:      metadata.EntityActionScopeCollection,
			Permission: permission + ":read",
			Idempotent: true,
		},
		{
			Key:        "source_wallets",
			Label:      "Исходные кошельки",
			Method:     "GET",
			Path:       "/source-wallets",
			Scope:      metadata.EntityActionScopeCollection,
			Permission: permission + ":read",
			Idempotent: true,
		},
		{
			Key:        "reconciliation_outflows",
			Label:      "Исходящие операции сверки",
			Method:     "GET",
			Path:       "/reconciliation/outflows",
			Scope:      metadata.EntityActionScopeCollection,
			Permission: permission + ":read",
			Idempotent: true,
		},
	}
}

func (r *TreasuryWithdrawalRegistration) Build(deps v1.DocumentDeps) v1.DocumentRouteHandler {
	repo := crypto_document_repo.NewTreasuryWithdrawalRepo()
	service := treasury_withdrawal.NewService(repo, deps.PostingEngine, deps.Numerator, nil)
	service.SetPolicyEngine(deps.PolicyEngine)

	service.Hooks().OnBeforeCreate(func(ctx context.Context, doc *treasury_withdrawal.TreasuryWithdrawal) error {
		audit.EnrichCreatedByDirect(ctx, &doc.CreatedBy, &doc.UpdatedBy)
		return nil
	})
	service.Hooks().OnBeforeUpdate(func(ctx context.Context, doc *treasury_withdrawal.TreasuryWithdrawal) error {
		audit.EnrichUpdatedByDirect(ctx, &doc.UpdatedBy)
		return nil
	})

	decorated := domain.Chain[*treasury_withdrawal.TreasuryWithdrawal](
		domain.WithLogging[*treasury_withdrawal.TreasuryWithdrawal]("treasury-withdrawal"),
		domain.WithEventLog[*treasury_withdrawal.TreasuryWithdrawal](treasury_withdrawal.Definition.Key, deps.EventWriter),
		domain.WithOutboxEvents[*treasury_withdrawal.TreasuryWithdrawal](treasury_withdrawal.Definition.Key, deps.OutboxPublisher, deps.CurrencyMetadataResolver),
	)(service)

	return cryptohandlers.NewTreasuryWithdrawalHandler(deps.BaseHandler, decorated, deps.RelatedDocFinder, deps.MovementProviders, deps.MovementRefResolver, deps.SettingsRepo, money_movement_repo.New(), r.services.MoneyMovementSigners, deps.StepUpChecker, deps.PostingEngine, deps.StepUpGuard)
}

// ---------------------------------------------------------------------------
// WithdrawalRequest
// ---------------------------------------------------------------------------

type WithdrawalRequestRegistration struct {
	v1.EntityRegistration[withdrawal_request.WithdrawalRequest]
	services Services
}

func (r *WithdrawalRequestRegistration) EntityLabel() string {
	return "Заявка на вывод"
}
func (r *WithdrawalRequestRegistration) EntityPresentation() metadata.Presentation {
	return metadata.Presentation{
		Singular: "Заявка на вывод",
		Plural:   "Заявки на вывод",
		NewLabel: "Новая заявка на вывод",
		Genitive: "заявки на вывод",
	}
}
func (r *WithdrawalRequestRegistration) RLSDimensions() map[string]string {
	return map[string]string{"merchant": "merchant_id"}
}

func (r *WithdrawalRequestRegistration) DocumentCapabilities() metadata.DocumentCapabilities {
	return metadata.DocumentCapabilities{
		List:             true,
		Read:             true,
		ExportList:       true,
		RelatedDocuments: true,
		Movements:        true,
	}
}

func (r *WithdrawalRequestRegistration) DocumentActions() []metadata.EntityActionDef {
	return []metadata.EntityActionDef{
		{
			Key:           "approve",
			Label:         "Утвердить",
			Method:        "POST",
			Path:          "/:id/approve",
			Scope:         metadata.EntityActionScopeItem,
			Permission:    r.Permission() + ":post",
			Idempotent:    true,
			AllowedStates: []string{string(withdrawal_request.StatusPendingApproval)},
			UI:            &metadata.EntityActionUI{FormToolbar: true},
		},
		{
			Key: "reject", Label: "Отклонить", Method: "POST", Path: "/:id/reject",
			Scope: metadata.EntityActionScopeItem, Permission: r.Permission() + ":post",
			Idempotent: true, Destructive: true,
			AllowedStates: []string{string(withdrawal_request.StatusPendingSignature), string(withdrawal_request.StatusPendingApproval)},
		},
		{
			Key: "retry_signing", Label: "Повторить подписание", Method: "POST", Path: "/:id/retry-signing",
			Scope: metadata.EntityActionScopeItem, Permission: r.Permission() + ":post",
			Idempotent: true, AllowedStates: []string{string(withdrawal_request.StatusPendingSignature)},
		},
	}
}

func (r *WithdrawalRequestRegistration) RegisterRoutes(group *gin.RouterGroup, deps v1.DocumentDeps) {
	repo := portal_repo.NewDashboardRepo()
	moneyMovementApprovals := money_movement_repo.New()
	approvals := cryptoapp.NewMoneyMovementApprovalService(moneyMovementApprovals, r.services.MoneyMovementSigners, deps.StepUpChecker)
	handler := cryptohandlers.NewWithdrawalRequestWorkflowHandler(
		deps.BaseHandler,
		repo,
		deps.RelatedDocFinder,
		moneyMovementApprovals,
		cryptoapp.NewWithdrawalApprovalService(repo, moneyMovementApprovals, approvals),
		deps.StepUpChecker,
		cryptoapp.NewWithdrawalRejectionService(repo, crypto_merchant_balance.NewService(crypto_register_repo.NewCryptoMerchantBalanceRepo())),
		portalapp.NewWithdrawalValuationService(repo, exchange_rate.NewService(register_repo.NewExchangeRateRepo())),
		deps.MovementProviders,
		deps.MovementRefResolver,
	)
	handler.RegisterDocumentRoutes(group, r.Permission())
}
