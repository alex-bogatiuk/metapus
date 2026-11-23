package cryptoapp

import (
	"context"
	"errors"
	"maps"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type BindMerchantInvoiceReferenceInput struct {
	MerchantID id.ID
	InvoiceID  id.ID
	APIKeyID   id.ID
	OrderID    string
	ExternalID string
	// Nil preserves the stored dictionary; an empty map clears it.
	Metadata map[string]string
}

// CheckInvoiceReferenceAccess runs before command replay, so every request
// reauthorizes the invoice against the current merchant and deletion state.
func (s *MerchantInvoiceService) CheckInvoiceReferenceAccess(ctx context.Context, merchantID, invoiceID id.ID) error {
	invoice, err := s.invoiceService.GetByID(ctx, invoiceID)
	if err != nil {
		return err
	}
	if invoice.MerchantID != merchantID || invoice.IsDeletionMarked() || invoice.DeletedAt != nil {
		return apperror.NewNotFound("invoice", invoiceID.String())
	}
	return nil
}

func (s *MerchantInvoiceService) BindMerchantInvoiceReference(ctx context.Context, in BindMerchantInvoiceReferenceInput) (*crypto_invoice.CryptoInvoice, error) {
	if id.IsNil(in.MerchantID) || id.IsNil(in.InvoiceID) || id.IsNil(in.APIKeyID) {
		return nil, apperror.NewValidation("merchant, invoice and API key are required")
	}
	if in.OrderID == "" && in.ExternalID == "" {
		return nil, apperror.NewValidation("orderId or externalId is required")
	}
	if in.OrderID != "" {
		if err := crypto_invoice.ValidateOrderReference(in.OrderID); err != nil {
			return nil, err
		}
	}
	if in.ExternalID != "" {
		if err := crypto_invoice.ValidateExternalReference(in.ExternalID); err != nil {
			return nil, err
		}
	}
	if err := crypto_invoice.ValidateMetadata(in.Metadata); err != nil {
		return nil, err
	}
	if s.referenceRepo == nil || s.events == nil {
		return nil, apperror.NewInternal(errors.New("invoice reference binding is not configured"))
	}
	txm, err := tenant.GetTxManager(ctx)
	if err != nil {
		return nil, apperror.NewInternal(err)
	}
	var result *crypto_invoice.CryptoInvoice
	err = txm.RunInTransaction(ctx, func(txCtx context.Context) error {
		invoice, err := s.referenceRepo.GetMerchantInvoiceForUpdate(txCtx, in.MerchantID, in.InvoiceID)
		if err != nil {
			return err
		}
		if invoice.MerchantID != in.MerchantID || invoice.IsDeletionMarked() || invoice.DeletedAt != nil {
			return apperror.NewNotFound("invoice", in.InvoiceID.String())
		}
		topUp, err := s.referenceRepo.IsPersistentTopUp(txCtx, in.MerchantID, in.InvoiceID)
		if err != nil {
			return err
		}
		if !topUp {
			return apperror.NewConflict("reference binding is only available for persistent wallet top-ups")
		}
		if in.OrderID != "" && invoice.OrderID != "" && invoice.OrderID != in.OrderID {
			return apperror.NewConflict("invoice already has a different orderId").WithDetail("field", "orderId")
		}
		if in.ExternalID != "" && invoice.ExternalID != "" && invoice.ExternalID != in.ExternalID {
			return apperror.NewConflict("invoice already has a different externalId").WithDetail("field", "externalId")
		}
		orderID, externalID := invoice.OrderID, invoice.ExternalID
		if in.OrderID != "" {
			orderID = in.OrderID
		}
		if in.ExternalID != "" {
			externalID = in.ExternalID
		}
		result = invoice
		if invoice.OrderID == orderID && invoice.ExternalID == externalID && (in.Metadata == nil || maps.Equal(invoice.Metadata, in.Metadata)) {
			return nil
		}
		before := map[string]any{"orderId": invoice.OrderID, "externalId": invoice.ExternalID, "metadata": maps.Clone(invoice.Metadata)}
		updated, err := s.referenceRepo.UpdateMerchantReference(txCtx, crypto_invoice.ReferenceUpdate{
			MerchantID: in.MerchantID, InvoiceID: in.InvoiceID, Version: invoice.Version,
			OrderID: orderID, ExternalID: externalID, Metadata: in.Metadata,
		})
		if err != nil {
			return err
		}
		invoice.OrderID, invoice.ExternalID = orderID, externalID
		if in.Metadata != nil {
			invoice.Metadata = maps.Clone(in.Metadata)
		}
		invoice.Version, invoice.UpdatedAt = updated.Version, updated.UpdatedAt
		return s.events.Write(txCtx, eventlog.Event{
			Category: eventlog.CategoryData, Severity: eventlog.SeverityInfo,
			EventType: eventlog.EventDocumentUpdate, Source: "merchant_api",
			EntityType: crypto_invoice.DocumentType, EntityID: &invoice.ID, EntityNumber: invoice.Number,
			Message: "Merchant invoice business references bound",
			Details: map[string]any{
				"command": "bind_merchant_reference", "invoiceId": in.InvoiceID.String(),
				"merchantId": in.MerchantID.String(), "apiKeyId": in.APIKeyID.String(),
				"before": before, "after": map[string]any{"orderId": invoice.OrderID, "externalId": invoice.ExternalID, "metadata": invoice.Metadata},
			},
		})
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
