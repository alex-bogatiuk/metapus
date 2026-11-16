package cryptoapp

import (
	"context"
	"strings"

	"metapus/internal/application/command"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/core/types"
	"metapus/internal/domain/documents/crypto_platform_funding"
)

// PlatformFundingResult is the stored outcome of a platform funding command.
type PlatformFundingResult struct {
	ID                       id.ID                          `json:"id"`
	Number                   string                         `json:"number"`
	ReceiptID                id.ID                          `json:"receiptId"`
	Status                   crypto_platform_funding.Status `json:"status"`
	Version                  int                            `json:"version"`
	Amount                   types.RawAmount                `json:"amount"`
	Reason                   string                         `json:"reason"`
	MerchantID               *id.ID                         `json:"merchantId,omitempty"`
	MerchantLink             string                         `json:"merchantLink,omitempty"`
	MerchantLinkAcknowledged bool                           `json:"merchantLinkAcknowledged"`
}

// PlatformFundingCommands exposes platform fundings as idempotent operator commands.
type PlatformFundingCommands struct {
	service  *PlatformFundingService
	commands *command.Executor
}

func NewPlatformFundingCommands(service *PlatformFundingService, commands *command.Executor) *PlatformFundingCommands {
	if service == nil || commands == nil {
		panic("platform funding commands require the service and the command executor")
	}
	return &PlatformFundingCommands{service: service, commands: commands}
}

type preparePlatformFundingRequest struct {
	ReceiptID              id.ID  `json:"receiptId"`
	Reason                 string `json:"reason"`
	AcknowledgeMerchantTie bool   `json:"acknowledgeMerchantTie"`
}

type decidePlatformFundingRequest struct {
	FundingID id.ID  `json:"fundingId"`
	Version   int    `json:"version"`
	Reason    string `json:"reason,omitempty"`
}

func (c *PlatformFundingCommands) Prepare(ctx context.Context, key string, receiptID id.ID, reason string, acknowledge bool) (command.Execution, error) {
	reason = strings.TrimSpace(reason)
	if id.IsNil(receiptID) || reason == "" || len(reason) > 2000 {
		return command.Execution{}, apperror.NewValidation("receipt and a reason (up to 2000 bytes) are required")
	}
	request := preparePlatformFundingRequest{ReceiptID: receiptID, Reason: reason, AcknowledgeMerchantTie: acknowledge}
	return c.execute(ctx, "prepare", key, request, func(ctx context.Context, actor id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error) {
		return c.service.Prepare(ctx, PreparePlatformFundingCommand{ReceiptID: receiptID, Reason: reason, AcknowledgeMerchantTie: acknowledge, Actor: actor})
	})
}

func (c *PlatformFundingCommands) Approve(ctx context.Context, key string, fundingID id.ID, version int) (command.Execution, error) {
	if id.IsNil(fundingID) || version < 1 {
		return command.Execution{}, apperror.NewValidation("platform funding and its version are required")
	}
	return c.execute(ctx, "approve", key, decidePlatformFundingRequest{FundingID: fundingID, Version: version},
		func(ctx context.Context, actor id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error) {
			return c.service.Approve(ctx, DecidePlatformFundingCommand{FundingID: fundingID, ExpectedVersion: version, Actor: actor})
		})
}

func (c *PlatformFundingCommands) Reject(ctx context.Context, key string, fundingID id.ID, version int, reason string) (command.Execution, error) {
	reason = strings.TrimSpace(reason)
	if id.IsNil(fundingID) || version < 1 || reason == "" || len(reason) > 2000 {
		return command.Execution{}, apperror.NewValidation("platform funding, its version and a reason (up to 2000 bytes) are required")
	}
	return c.execute(ctx, "reject", key, decidePlatformFundingRequest{FundingID: fundingID, Version: version, Reason: reason},
		func(ctx context.Context, actor id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error) {
			return c.service.Reject(ctx, DecidePlatformFundingCommand{FundingID: fundingID, ExpectedVersion: version, Actor: actor, Reason: reason})
		})
}

func (c *PlatformFundingCommands) execute(ctx context.Context, action, key string, request any,
	fn func(context.Context, id.ID) (*crypto_platform_funding.CryptoPlatformFunding, error)) (command.Execution, error) {
	return runOperatorCommand(ctx, c.commands, "crypto.receipt.platform_funding."+action, key, request,
		func(txCtx context.Context, actor id.ID) (command.Result, error) {
			doc, err := fn(txCtx, actor)
			if err != nil {
				return command.Result{}, err
			}
			result := PlatformFundingResult{ID: doc.ID, Number: doc.Number, ReceiptID: doc.ReceiptID, Status: doc.Status,
				Version: doc.Version, Amount: doc.Amount, Reason: doc.Reason, MerchantID: doc.MerchantID,
				MerchantLinkAcknowledged: doc.MerchantLinkAcknowledged}
			if doc.MerchantLink != nil {
				result.MerchantLink = string(*doc.MerchantLink)
			}
			return command.Result{Outcome: string(doc.Status), ResourceType: doc.GetDocumentType(), ResourceID: doc.ID.String(),
				ResourceVersion: int64(doc.Version), Payload: command.MustPayload(result)}, nil
		})
}
