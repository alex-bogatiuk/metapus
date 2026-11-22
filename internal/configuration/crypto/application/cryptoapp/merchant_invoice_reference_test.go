package cryptoapp

import (
	"context"
	"maps"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"metapus/pkg/sdk/apperror"
	"metapus/pkg/sdk/eventlog"
	"metapus/pkg/sdk/id"
	"metapus/pkg/sdk/tenant"

	"metapus/pkg/sdk/types"
)

type referenceRepoStub struct {
	invoice *crypto_invoice.CryptoInvoice
	topUp   bool
	writes  int
}

func (r *referenceRepoStub) GetMerchantInvoiceForUpdate(context.Context, id.ID, id.ID) (*crypto_invoice.CryptoInvoice, error) {
	copy := *r.invoice
	copy.Metadata = maps.Clone(r.invoice.Metadata)
	return &copy, nil
}

func (r *referenceRepoStub) IsPersistentTopUp(context.Context, id.ID, id.ID) (bool, error) {
	return r.topUp, nil
}

func (r *referenceRepoStub) UpdateMerchantReference(_ context.Context, in crypto_invoice.ReferenceUpdate) (crypto_invoice.ReferenceUpdateResult, error) {
	r.writes++
	r.invoice.OrderID = in.OrderID
	r.invoice.ExternalID = in.ExternalID
	if in.Metadata != nil {
		r.invoice.Metadata = maps.Clone(in.Metadata)
	}
	r.invoice.Version++
	return crypto_invoice.ReferenceUpdateResult{Version: r.invoice.Version, UpdatedAt: time.Now().UTC()}, nil
}

type referenceAuditStub struct{ events []eventlog.Event }

func (w *referenceAuditStub) Write(_ context.Context, event eventlog.Event) error {
	w.events = append(w.events, event)
	return nil
}

func (w *referenceAuditStub) WriteBatch(context.Context, []eventlog.Event) error { return nil }

func TestMerchantReferenceBindingIsIndependentOfStatusAndNoOpOnRepeat(t *testing.T) {
	for _, status := range []crypto_invoice.InvoiceStatus{
		crypto_invoice.InvoiceStatusCreated, crypto_invoice.InvoiceStatusPaid,
		crypto_invoice.InvoiceStatusConfirmed, crypto_invoice.InvoiceStatusCompleted,
		crypto_invoice.InvoiceStatusReversed, crypto_invoice.InvoiceStatusCancelled,
	} {
		t.Run(string(status), func(t *testing.T) {
			invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(100))
			invoice.Status, invoice.ExternalID = status, "ERP-45001"
			invoice.Metadata = map[string]string{"original": "value"}
			creationKey := id.New()
			invoice.APIKeyID = &creationKey
			repo := &referenceRepoStub{invoice: invoice, topUp: true}
			audit := &referenceAuditStub{}
			svc := &MerchantInvoiceService{referenceRepo: repo, events: audit}
			ctx := tenant.WithTxManager(context.Background(), &merchantInvoiceTestTxManager{})
			input := BindMerchantInvoiceReferenceInput{MerchantID: invoice.MerchantID, InvoiceID: invoice.ID, APIKeyID: id.New(), OrderID: "ORD-1"}
			result, err := svc.BindMerchantInvoiceReference(ctx, input)
			require.NoError(t, err)
			require.Equal(t, input.OrderID, result.OrderID)
			require.Equal(t, map[string]string{"original": "value"}, result.Metadata)
			require.Equal(t, status, result.Status)
			require.Equal(t, "ERP-45001", result.ExternalID)
			require.Equal(t, creationKey, *result.APIKeyID)
			require.Len(t, audit.events, 1)
			require.Equal(t, input.APIKeyID.String(), audit.events[0].Details["apiKeyId"])
			version := result.Version
			result, err = svc.BindMerchantInvoiceReference(ctx, input)
			require.NoError(t, err)
			require.Equal(t, version, result.Version)
			require.Equal(t, 1, repo.writes)
			require.Len(t, audit.events, 1)
			input.Metadata = map[string]string{"sap_doc": "45001"}
			result, err = svc.BindMerchantInvoiceReference(ctx, input)
			require.NoError(t, err)
			require.Equal(t, input.Metadata, result.Metadata)
			input.Metadata = map[string]string{}
			result, err = svc.BindMerchantInvoiceReference(ctx, input)
			require.NoError(t, err)
			require.Empty(t, result.Metadata)
			input.OrderID = "ORD-2"
			_, err = svc.BindMerchantInvoiceReference(ctx, input)
			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 409, appErr.HTTPStatus)
			require.Equal(t, "ORD-1", repo.invoice.OrderID)
		})
	}
}

func TestMerchantReferenceBindsExternalIDWithoutOrderAndPreservesOmittedReferences(t *testing.T) {
	invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(100))
	invoice.InvoiceKind, invoice.CreationSource = crypto_invoice.InvoiceKindTopUp, crypto_invoice.CreationSourceSystem
	repo := &referenceRepoStub{invoice: invoice, topUp: true}
	audit := &referenceAuditStub{}
	svc := &MerchantInvoiceService{referenceRepo: repo, events: audit}
	ctx := tenant.WithTxManager(context.Background(), &merchantInvoiceTestTxManager{})
	input := BindMerchantInvoiceReferenceInput{MerchantID: invoice.MerchantID, InvoiceID: invoice.ID, APIKeyID: id.New(), ExternalID: "1С-проводка-001"}
	result, err := svc.BindMerchantInvoiceReference(ctx, input)
	require.NoError(t, err)
	require.Empty(t, result.OrderID)
	require.Equal(t, input.ExternalID, result.ExternalID)
	require.Equal(t, crypto_invoice.InvoiceKindTopUp, result.InvoiceKind)
	require.Equal(t, invoice.ExpectedAmount, result.ExpectedAmount)
	version := result.Version
	_, err = svc.BindMerchantInvoiceReference(ctx, input)
	require.NoError(t, err)
	require.Equal(t, version, repo.invoice.Version)
	require.Len(t, audit.events, 1)
	require.Equal(t, "1С-проводка-001", audit.events[0].Details["after"].(map[string]any)["externalId"])
	input.OrderID, input.ExternalID = "ORD-1", ""
	result, err = svc.BindMerchantInvoiceReference(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "1С-проводка-001", result.ExternalID)
	input.OrderID, input.ExternalID = "", "1С-проводка-001"
	input.Metadata = map[string]string{"erp": "1c"}
	result, err = svc.BindMerchantInvoiceReference(ctx, input)
	require.NoError(t, err)
	require.Equal(t, "ORD-1", result.OrderID)
	input.ExternalID = "another"
	_, err = svc.BindMerchantInvoiceReference(ctx, input)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.HTTPStatus)
	require.Equal(t, "1С-проводка-001", repo.invoice.ExternalID)
	input.ExternalID = ""
	_, err = svc.BindMerchantInvoiceReference(ctx, input)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 400, appErr.HTTPStatus)
}

func TestMerchantReferenceRejectsOtherOwnerAndNonTopUp(t *testing.T) {
	invoice := crypto_invoice.NewCryptoInvoice(id.New(), id.New(), types.NewRawAmountFromInt64(100))
	repo := &referenceRepoStub{invoice: invoice}
	svc := &MerchantInvoiceService{referenceRepo: repo, events: &referenceAuditStub{}}
	ctx := tenant.WithTxManager(context.Background(), &merchantInvoiceTestTxManager{})
	input := BindMerchantInvoiceReferenceInput{MerchantID: id.New(), InvoiceID: invoice.ID, APIKeyID: id.New(), OrderID: "ORD-1"}
	_, err := svc.BindMerchantInvoiceReference(ctx, input)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.HTTPStatus)
	input.MerchantID = invoice.MerchantID
	_, err = svc.BindMerchantInvoiceReference(ctx, input)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.HTTPStatus)
	require.Zero(t, repo.writes)
}
