package cryptoapp

import (
	"context"
	"errors"
	"strings"

	"metapus/internal/application/command"
	"metapus/internal/core/apperror"
	appctx "metapus/internal/core/context"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_merchant_credit"
)

// MerchantCreditResult is the stored outcome of an operator command.
type MerchantCreditResult struct {
	ID               id.ID                         `json:"id"`
	Number           string                        `json:"number"`
	ReceiptID        id.ID                         `json:"receiptId"`
	MerchantID       id.ID                         `json:"merchantId"`
	Status           crypto_merchant_credit.Status `json:"status"`
	Version          int                           `json:"version"`
	ApprovalRequired bool                          `json:"approvalRequired"`
	Amount           types.CryptoAmount            `json:"amount"`
	NetAmount        types.CryptoAmount            `json:"netAmount"`
	ProcessingFee    types.CryptoAmount            `json:"processingFee"`
}

// MerchantCreditCommands exposes merchant credits as idempotent operator
// commands: a repeated Idempotency-Key returns the stored result.
type MerchantCreditCommands struct {
	service  *MerchantCreditService
	commands *command.Executor
}

func NewMerchantCreditCommands(service *MerchantCreditService, commands *command.Executor) *MerchantCreditCommands {
	if service == nil || commands == nil {
		panic("merchant credit commands require the service and the command executor")
	}
	return &MerchantCreditCommands{service: service, commands: commands}
}

type prepareMerchantCreditRequest struct {
	ReceiptID  id.ID  `json:"receiptId"`
	MerchantID id.ID  `json:"merchantId"`
	InvoiceID  *id.ID `json:"invoiceId,omitempty"`
}

type decideMerchantCreditRequest struct {
	CreditID id.ID  `json:"creditId"`
	Version  int    `json:"version"`
	Reason   string `json:"reason,omitempty"`
}

func (c *MerchantCreditCommands) Prepare(ctx context.Context, key string, receiptID, merchantID id.ID, invoiceID *id.ID) (command.Execution, error) {
	if id.IsNil(receiptID) || id.IsNil(merchantID) {
		return command.Execution{}, apperror.NewValidation("receipt and merchant are required")
	}
	request := prepareMerchantCreditRequest{ReceiptID: receiptID, MerchantID: merchantID, InvoiceID: invoiceID}
	return c.execute(ctx, "prepare", key, request, func(ctx context.Context, actor id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
		return c.service.Prepare(ctx, PrepareMerchantCreditCommand{ReceiptID: receiptID, MerchantID: merchantID, InvoiceID: invoiceID, Actor: actor})
	})
}

func (c *MerchantCreditCommands) Approve(ctx context.Context, key string, creditID id.ID, version int) (command.Execution, error) {
	if id.IsNil(creditID) || version < 1 {
		return command.Execution{}, apperror.NewValidation("credit and its version are required")
	}
	return c.execute(ctx, "approve", key, decideMerchantCreditRequest{CreditID: creditID, Version: version},
		func(ctx context.Context, actor id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
			return c.service.Approve(ctx, DecideMerchantCreditCommand{CreditID: creditID, ExpectedVersion: version, Actor: actor})
		})
}

func (c *MerchantCreditCommands) Reject(ctx context.Context, key string, creditID id.ID, version int, reason string) (command.Execution, error) {
	reason = strings.TrimSpace(reason)
	if id.IsNil(creditID) || version < 1 || reason == "" || len(reason) > 2000 {
		return command.Execution{}, apperror.NewValidation("credit, its version and a reason (up to 2000 bytes) are required")
	}
	return c.execute(ctx, "reject", key, decideMerchantCreditRequest{CreditID: creditID, Version: version, Reason: reason},
		func(ctx context.Context, actor id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error) {
			return c.service.Reject(ctx, DecideMerchantCreditCommand{CreditID: creditID, ExpectedVersion: version, Actor: actor, Reason: reason})
		})
}

func (c *MerchantCreditCommands) execute(ctx context.Context, action, key string, request any,
	fn func(context.Context, id.ID) (*crypto_merchant_credit.CryptoMerchantCredit, error)) (command.Execution, error) {
	return runOperatorCommand(ctx, c.commands, "crypto.receipt.credit."+action, key, request,
		func(txCtx context.Context, actor id.ID) (command.Result, error) {
			doc, err := fn(txCtx, actor)
			if err != nil {
				return command.Result{}, err
			}
			result := MerchantCreditResult{ID: doc.ID, Number: doc.Number, ReceiptID: doc.ReceiptID, MerchantID: doc.MerchantID,
				Status: doc.Status, Version: doc.Version, ApprovalRequired: doc.ApprovalRequired,
				Amount: doc.Amount, NetAmount: doc.NetAmount, ProcessingFee: doc.ProcessingFee}
			return command.Result{Outcome: string(doc.Status), ResourceType: doc.GetDocumentType(), ResourceID: doc.ID.String(),
				ResourceVersion: int64(doc.Version), Payload: command.MustPayload(result)}, nil
		})
}

// runOperatorCommand runs a money command of an authenticated operator once
// per Idempotency-Key; a repeated key returns the stored result.
func runOperatorCommand(ctx context.Context, commands *command.Executor, operation, key string, request any,
	run func(context.Context, id.ID) (command.Result, error)) (command.Execution, error) {
	if err := security.GetDataScope(ctx).CanMutate(); err != nil {
		return command.Execution{}, err
	}
	user := appctx.GetUser(ctx)
	if user == nil {
		return command.Execution{}, apperror.NewUnauthorized("authenticated operator is required")
	}
	actor, err := id.Parse(user.UserID)
	if err != nil || id.IsNil(actor) {
		return command.Execution{}, apperror.NewUnauthorized("invalid user identity")
	}
	if strings.TrimSpace(key) == "" {
		return command.Execution{}, apperror.NewIdempotencyKeyRequired()
	}
	if len(key) > 200 {
		return command.Execution{}, apperror.NewValidation("idempotency key is too long")
	}
	tenantID, err := requiredTenantID(ctx)
	if err != nil {
		return command.Execution{}, err
	}
	fingerprint, err := command.Fingerprint(request)
	if err != nil {
		return command.Execution{}, apperror.NewInternal(err)
	}
	execution, err := commands.Execute(ctx, command.Metadata{
		ScopeType: "user", ScopeID: tenantID + ":" + actor.String(), Operation: operation,
		IdempotencyKey: key, RequestFingerprint: fingerprint,
	}, func(txCtx context.Context) (command.Result, error) { return run(txCtx, actor) })
	if errors.Is(err, command.ErrKeyReused) {
		err = apperror.NewIdempotencyMismatch(key)
	}
	if errors.Is(err, command.ErrInProgress) {
		err = apperror.NewIdempotencyConflict(key).WithDetail("retryAfterSeconds", 3)
	}
	return execution, err
}
