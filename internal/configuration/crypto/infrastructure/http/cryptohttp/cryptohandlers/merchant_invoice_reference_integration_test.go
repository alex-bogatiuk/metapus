//go:build integration

package cryptohandlers

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/cryptoapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/catalogs/merchant"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/crypto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_invoice"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptodto"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/http/cryptohttp/cryptomiddleware"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_document_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/crypto_repo/receipttest"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/infrastructure/storage/postgres/portal_repo"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/eventlog"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/middleware"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/sdktest"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/tenant"
)

type referenceTopUp struct {
	invoiceID, paymentID, walletID id.ID
	externalID                     string
}

type referenceFixture struct {
	ctx                                                    context.Context
	pool                                                   *pgxpool.Pool
	txm                                                    *sdktest.TxManager
	repo                                                   *crypto_document_repo.CryptoInvoiceRepo
	docSvc                                                 *crypto_invoice.Service
	merchantID, otherMerchantID, keyID, networkID, tokenID id.ID
}

func newReferenceFixture(t *testing.T) *referenceFixture {
	t.Helper()
	dsn := os.Getenv("METAPUS_TEST_DATABASE_DSN")
	require.NotEmpty(t, dsn, "METAPUS_TEST_DATABASE_DSN must point to the isolated test database")
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	f := &referenceFixture{pool: pool, merchantID: id.New(), otherMerchantID: id.New(), keyID: id.New(), networkID: id.New(), tokenID: id.New()}
	f.txm = sdktest.NewTxManager(pool)
	f.ctx = tenant.WithTxManager(ctx, f.txm)
	f.repo = crypto_document_repo.NewCryptoInvoiceRepo()
	f.docSvc = crypto_invoice.NewService(f.repo, nil, nil, nil)
	suffix := id.Compact(f.networkID)
	_, err = pool.Exec(ctx, `
		INSERT INTO cat_blockchain_networks(id,code,name,chain_family,network_identifier,native_token_symbol,native_decimals,is_active)
		VALUES($1,$2,'Order reference test network','tron',$2,'TRX',6,FALSE);
		INSERT INTO cat_merchants(id,code,name) VALUES($3,$4,'Order reference merchant'),($5,$6,'Other order reference merchant');
		INSERT INTO cat_tokens(id,code,name,network_id,contract_address,symbol,decimal_places,token_standard)
		VALUES($7,$8,'Order reference token',$1,$9,'ORT',6,'TRC-20')`, pgx.QueryExecModeSimpleProtocol,
		f.networkID, "OR-NET-"+suffix, f.merchantID, "OR-MER-"+suffix, f.otherMerchantID, "OR-OTHER-"+suffix,
		f.tokenID, "OR-TOKEN-"+suffix, "TOrderToken"+suffix)
	require.NoError(t, err)
	var indexDefinition string
	require.NoError(t, pool.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname='idx_crypto_invoices_merchant_order_id'`).Scan(&indexDefinition))
	require.Contains(t, indexDefinition, "UNIQUE")
	require.Contains(t, indexDefinition, "merchant_id, order_id")
	return f
}

func (f *referenceFixture) topUp(t *testing.T, merchantID id.ID) referenceTopUp {
	t.Helper()
	p := referenceTopUp{invoiceID: id.New(), paymentID: id.New(), walletID: id.New()}
	suffix := id.Compact(p.invoiceID)
	txHash := suffix + suffix
	p.externalID = "ERP-" + suffix
	_, err := f.pool.Exec(f.ctx, `
		INSERT INTO cat_wallets(id,code,name,network_id,address,vault_key_id,tier,wallet_role,status,allocation_mode,merchant_id,customer_id,is_active,chain_activation_status)
		VALUES($1,$2,'Order reference wallet',$3,$4,$2,'pool','merchant_pool','assigned','persistent',$5,$6,TRUE,'active');
		INSERT INTO doc_crypto_invoices(id,public_ref,number,date,merchant_id,token_id,wallet_id,expected_amount,status,expires_at,ttl_minutes,creation_source,invoice_kind,external_id,metadata)
		VALUES($7,$8,$9,NOW(),$5,$10,$1,100,'created',NOW()+interval '1 hour',60,'system','top_up',$11,'{"original":"value"}');
		INSERT INTO doc_crypto_payments(id,number,date,basis_type,basis_id,invoice_id,merchant_id,network_id,token_id,wallet_id,tx_hash,from_address,amount,event_kind,event_index,block_number,block_hash,required_confs,status,detected_at)
		VALUES($12,$13,NOW(),'CryptoInvoice',$7,$7,$5,$3,$10,$1,$14,$15,100,'contract_log',0,100,$16,1,'detected',NOW())`,
		pgx.QueryExecModeSimpleProtocol, p.walletID, "OR-WAL-"+suffix, f.networkID, "TOrderWallet"+suffix,
		merchantID, "customer-"+suffix, p.invoiceID, "ci_"+suffix, "OR-INV-"+suffix, f.tokenID, p.externalID,
		p.paymentID, "OR-PAY-"+suffix, txHash, "TOrderSender"+suffix, "block-"+suffix)
	require.NoError(t, err)
	return p
}

func (f *referenceFixture) service(t *testing.T, events eventlog.Writer) *cryptoapp.MerchantInvoiceService {
	t.Helper()
	svc, err := cryptoapp.NewMerchantInvoiceService(cryptoapp.MerchantInvoiceServiceConfig{
		InvoiceService: f.docSvc, InvoiceRepo: f.repo, ReferenceRepo: f.repo, Events: events,
		IsUniqueViolation: postgres.IsUniqueViolation,
	})
	require.NoError(t, err)
	return svc
}

func (f *referenceFixture) bind(svc *cryptoapp.MerchantInvoiceService, p referenceTopUp, merchantID id.ID, orderID string, metadata map[string]string) (*crypto_invoice.CryptoInvoice, error) {
	return svc.BindMerchantInvoiceReference(f.ctx, cryptoapp.BindMerchantInvoiceReferenceInput{
		MerchantID: merchantID, InvoiceID: p.invoiceID, APIKeyID: f.keyID, OrderID: orderID, Metadata: metadata,
	})
}

// settle accounts the payment's transfer as a receipt and posts the payment as
// its attribution, crediting the merchant with the whole amount.
func (f *referenceFixture) settle(t *testing.T, p referenceTopUp) error {
	t.Helper()
	var txHash, from string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT tx_hash,from_address FROM doc_crypto_payments WHERE id=$1`, p.paymentID).Scan(&txHash, &from))
	receipt, proof := receipttest.Record(t, f.ctx, receipttest.Deps{Proofs: crypto_repo.NewBlockIngestionRepo(), Repo: crypto_repo.NewReceiptIntakeRepo(), Guard: crypto_repo.NewBlockIngestionRepo()},
		receipttest.Transfer{WalletID: p.walletID, TokenID: f.tokenID, TxHash: txHash, FromAddress: from, Amount: "100", BlockNumber: 100})
	lifecycle := crypto.NewInvoiceLifecycleService(f.repo, crypto_document_repo.NewCryptoPaymentRepo(), nil, f.txm, nil)
	return f.txm.RunInTransaction(f.ctx, func(ctx context.Context) error {
		if _, err := f.repo.GetByIDForUpdate(ctx, p.invoiceID); err != nil {
			return err
		}
		_, err := f.txm.GetQuerier(ctx).Exec(ctx, `
			UPDATE doc_crypto_payments SET status='settled',canonicality='canonical',finality_status='finalized',confirmations=1,posted=TRUE,posted_version=1,confirmed_at=NOW(),receipt_id=$2,verification_id=$3 WHERE id=$1;
			INSERT INTO reg_crypto_receipt_suspense_movements(recorder_id,recorder_type,recorder_version,period,record_type,receipt_id,token_id,amount)
			SELECT $1,'CryptoPayment',1,NOW(),'expense',$2,token_id,100 FROM doc_crypto_payments WHERE id=$1;
			INSERT INTO reg_crypto_merchant_balance_movements(line_id,recorder_id,recorder_type,recorder_version,period,record_type,merchant_id,token_id,amount)
			SELECT $1,$1,'CryptoPayment',1,NOW(),'receipt',merchant_id,token_id,100 FROM doc_crypto_payments WHERE id=$1`, pgx.QueryExecModeSimpleProtocol, p.paymentID, receipt, proof)
		if err != nil {
			return err
		}
		_, err = lifecycle.SyncInTransaction(ctx, p.invoiceID)
		return err
	})
}

func TestInvoiceReferenceHTTPReplaySecurityAndReconciliation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newReferenceFixture(t)
	p := f.topUp(t, f.merchantID)
	require.NoError(t, f.settle(t, p))
	before, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, crypto_invoice.InvoiceStatusCompleted, before.Status)
	svc := f.service(t, postgres.NewEventLogRepo())
	queries := cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo())
	h := NewMerchantInvoiceHandler(f.docSvc, svc, nil, nil, queries)
	scopes := []merchant.APIKeyScope{merchant.ScopeInvoiceCreate, merchant.ScopeInvoiceRead}
	currentMerchant := f.merchantID
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := cryptomiddleware.WithMerchant(f.ctx, &cryptomiddleware.MerchantContext{MerchantID: currentMerchant, KeyID: f.keyID, Scopes: scopes})
		ctx = merchant.WithPortalScope(ctx, merchant.PortalScope{MerchantIDs: []id.ID{currentMerchant}, Roles: map[id.ID]merchant.PortalRole{currentMerchant: merchant.PortalRoleOwner}})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.PATCH("/merchant/v1/invoices/:id/reference", cryptomiddleware.RequireMerchantScope(merchant.ScopeInvoiceCreate), h.AuthorizeInvoiceReference, middleware.CommandIdempotency(true), h.BindInvoiceReference)
	router.GET("/merchant/v1/invoices/:id", h.GetInvoice)
	router.GET("/merchant/v1/invoices", h.ListInvoices)
	portal := &PortalHandler{repo: portal_repo.NewDashboardRepo()}
	router.GET("/portal/v1/invoices/export", portal.ExportInvoicesCSV)
	request := func(method, path, key, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		router.ServeHTTP(w, r)
		return w
	}
	path := "/merchant/v1/invoices/" + p.invoiceID.String() + "/reference"
	order := "ORD,\"99881\"\nSAP-45001"
	body, err := json.Marshal(map[string]any{"orderId": order, "metadata": map[string]string{"sap_doc": "45001"}})
	require.NoError(t, err)
	first := request(http.MethodPatch, path, "first", string(body))
	require.Equal(t, 200, first.Code, first.Body.String())
	var invoice cryptodto.MerchantInvoiceResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &invoice))
	require.Equal(t, order, *invoice.OrderID)
	require.Equal(t, p.externalID, *invoice.ExternalID)
	require.Equal(t, "completed", invoice.Status)
	replayed := request(http.MethodPatch, path, "first", string(body))
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.JSONEq(t, first.Body.String(), replayed.Body.String())
	require.Equal(t, "true", replayed.Header().Get("Idempotent-Replayed"))
	noOp := request(http.MethodPatch, path, "new-key", string(body))
	require.Equal(t, 200, noOp.Code, noOp.Body.String())
	after, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, before.Version+1, after.Version)
	require.Equal(t, before.ExpectedAmount, after.ExpectedAmount)
	require.Equal(t, before.ReceivedAmount, after.ReceivedAmount)
	require.Equal(t, before.ConfirmedAmount, after.ConfirmedAmount)
	require.Equal(t, before.CreditedAmount, after.CreditedAmount)
	require.Equal(t, before.WalletID, after.WalletID)
	require.Equal(t, before.APIKeyID, after.APIKeyID)
	var audits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_event_log WHERE entity_id=$1 AND details->>'command'='bind_merchant_reference'`, p.invoiceID).Scan(&audits))
	require.Equal(t, 1, audits)
	require.Equal(t, 409, request(http.MethodPatch, path, "first", `{"orderId":"another"}`).Code)
	require.Equal(t, 409, request(http.MethodPatch, path, "different-order", `{"orderId":"another"}`).Code)
	for i, invalid := range []string{
		`{"orderId":"ORD","metadata":null}`, `{"orderId":"ORD","amount":"1"}`,
		`{"orderId":"ORD","status":"paid"}`, `{"orderId":"ORD","externalId":null}`,
		`{"orderId":"ORD","walletId":"new"}`, `{"orderId":"ORD","currency":"new"}`,
		`{"orderId":" "}`, `{"orderId":null}`, `{}`,
		`{"orderId":"ORD\u0000X"}`, `{"orderId":"ORD","metadata":{"sap_doc":"45\u000001"}}`,
		`{"orderId":"ORD","metadata":{"sap\u0000doc":"45001"}}`,
	} {
		require.Equal(t, 400, request(http.MethodPatch, path, fmt.Sprint("invalid-", i), invalid).Code, invalid)
	}
	unchanged, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, after.Version, unchanged.Version)
	require.Equal(t, after.OrderID, unchanged.OrderID)
	require.Equal(t, after.Metadata, unchanged.Metadata)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_event_log WHERE entity_id=$1 AND details->>'command'='bind_merchant_reference'`, p.invoiceID).Scan(&audits))
	require.Equal(t, 1, audits)
	require.Equal(t, 400, request(http.MethodPatch, path, "", string(body)).Code)
	scopes = []merchant.APIKeyScope{merchant.ScopeInvoiceRead}
	require.Equal(t, 403, request(http.MethodPatch, path, "first", string(body)).Code)
	scopes = []merchant.APIKeyScope{merchant.ScopeInvoiceCreate, merchant.ScopeInvoiceRead}
	currentMerchant = f.otherMerchantID
	require.Equal(t, 404, request(http.MethodPatch, path, "first", string(body)).Code)
	currentMerchant = f.merchantID
	list := request(http.MethodGet, "/merchant/v1/invoices?orderId="+url.QueryEscape(order), "", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	var page cryptodto.MerchantInvoiceListResponse
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &page))
	require.Len(t, page.Items, 1)
	require.Equal(t, order, *page.Items[0].OrderID)
	detail := request(http.MethodGet, "/merchant/v1/invoices/"+p.invoiceID.String(), "", "")
	require.Equal(t, 200, detail.Code, detail.Body.String())
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &invoice))
	require.Equal(t, map[string]string{"sap_doc": "45001"}, invoice.Metadata)
	portalPage, _, err := portal.repo.ListInvoices(f.ctxWithPortal(), &f.merchantID, portal_repo.InvoiceFilter{Search: order}, 100, 0)
	require.NoError(t, err)
	require.Len(t, portalPage, 1)
	require.Equal(t, order, portalPage[0].OrderID)
	export := request(http.MethodGet, "/portal/v1/invoices/export?search="+url.QueryEscape(order), "", "")
	require.Equal(t, 200, export.Code, export.Body.String())
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(export.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, order, rows[1][10])
	clearBody, err := json.Marshal(map[string]any{"orderId": order, "metadata": map[string]string{}})
	require.NoError(t, err)
	require.Equal(t, 200, request(http.MethodPatch, path, "clear", string(clearBody)).Code)
	preserveBody, err := json.Marshal(map[string]string{"orderId": order})
	require.NoError(t, err)
	require.Equal(t, 200, request(http.MethodPatch, path, "preserve", string(preserveBody)).Code)
	after, err = f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Empty(t, after.Metadata)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET deletion_mark=TRUE WHERE id=$1`, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, 404, request(http.MethodPatch, path, "first", string(body)).Code)
}

func TestInvoiceReferenceFormulaExportPreservesLiteralReference(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newReferenceFixture(t)
	p := f.topUp(t, f.merchantID)
	svc := f.service(t, postgres.NewEventLogRepo())
	queries := cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo())
	h := NewMerchantInvoiceHandler(f.docSvc, svc, nil, nil, queries)
	portal := &PortalHandler{repo: portal_repo.NewDashboardRepo()}
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := cryptomiddleware.WithMerchant(f.ctxWithPortal(), &cryptomiddleware.MerchantContext{
			MerchantID: f.merchantID, KeyID: f.keyID,
			Scopes: []merchant.APIKeyScope{merchant.ScopeInvoiceCreate, merchant.ScopeInvoiceRead},
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.PATCH("/merchant/v1/invoices/:id/reference", cryptomiddleware.RequireMerchantScope(merchant.ScopeInvoiceCreate), h.AuthorizeInvoiceReference, middleware.CommandIdempotency(true), h.BindInvoiceReference)
	router.GET("/merchant/v1/invoices/:id", h.GetInvoice)
	router.GET("/portal/v1/invoices/export", portal.ExportInvoicesCSV)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "formula-reference")
		router.ServeHTTP(w, r)
		return w
	}
	orderID := "=1+1"
	path := "/merchant/v1/invoices/" + p.invoiceID.String()
	bound := request(http.MethodPatch, path+"/reference", `{"orderId":"=1+1"}`)
	require.Equal(t, 200, bound.Code, bound.Body.String())
	var invoice cryptodto.MerchantInvoiceResponse
	require.NoError(t, json.Unmarshal(bound.Body.Bytes(), &invoice))
	require.Equal(t, orderID, *invoice.OrderID)
	require.Equal(t, p.externalID, *invoice.ExternalID)
	export := request(http.MethodGet, "/portal/v1/invoices/export?search="+url.QueryEscape(orderID), "")
	require.Equal(t, 200, export.Code, export.Body.String())
	rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(export.Body.String(), "\xEF\xBB\xBF"))).ReadAll()
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "\t"+orderID, rows[1][10])
	require.Equal(t, p.externalID, rows[1][11])
	require.Contains(t, export.Body.String(), ",\"\t=1+1\"")
	saved, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, orderID, saved.OrderID)
	require.Equal(t, p.externalID, saved.ExternalID)
	detail := request(http.MethodGet, path, "")
	require.Equal(t, 200, detail.Code, detail.Body.String())
	require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &invoice))
	require.Equal(t, orderID, *invoice.OrderID)
}

func (f *referenceFixture) ctxWithPortal() context.Context {
	return merchant.WithPortalScope(f.ctx, merchant.PortalScope{MerchantIDs: []id.ID{f.merchantID}, Roles: map[id.ID]merchant.PortalRole{f.merchantID: merchant.PortalRoleOwner}})
}

func TestInvoiceReferenceExternalOnlyHTTPAndSystemClassification(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newReferenceFixture(t)
	p := f.topUp(t, f.merchantID)
	_, err := f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET external_id='' WHERE id=$1`, p.invoiceID)
	require.NoError(t, err)
	require.NoError(t, f.settle(t, p))
	before, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	svc := f.service(t, postgres.NewEventLogRepo())
	h := NewMerchantInvoiceHandler(f.docSvc, svc, nil, nil, cryptoapp.NewMerchantCryptoQueryService(crypto_repo.NewMerchantCryptoQueryRepo()))
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		c.Request = c.Request.WithContext(cryptomiddleware.WithMerchant(f.ctx, &cryptomiddleware.MerchantContext{
			MerchantID: f.merchantID, KeyID: f.keyID, Scopes: []merchant.APIKeyScope{merchant.ScopeInvoiceCreate},
		}))
		c.Next()
	})
	router.PATCH("/merchant/v1/invoices/:id/reference", cryptomiddleware.RequireMerchantScope(merchant.ScopeInvoiceCreate), h.AuthorizeInvoiceReference, middleware.CommandIdempotency(true), h.BindInvoiceReference)
	request := func(key, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodPatch, "/merchant/v1/invoices/"+p.invoiceID.String()+"/reference", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", key)
		router.ServeHTTP(w, r)
		return w
	}
	body := `{"externalId":"topup_1С-проводка-001","metadata":{"erp":"1c"}}`
	first := request("external-only", body)
	require.Equal(t, 200, first.Code, first.Body.String())
	var invoice cryptodto.MerchantInvoiceResponse
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &invoice))
	require.Nil(t, invoice.OrderID)
	require.Equal(t, "topup_1С-проводка-001", *invoice.ExternalID)
	replayed := request("external-only", body)
	require.Equal(t, 200, replayed.Code, replayed.Body.String())
	require.Equal(t, "true", replayed.Header().Get("Idempotent-Replayed"))
	require.JSONEq(t, first.Body.String(), replayed.Body.String())
	require.Equal(t, 200, request("external-noop", body).Code)
	for i, invalid := range []string{
		`{}`, `{"metadata":{}}`, `{"externalId":null}`, `{"externalId":""}`, `{"externalId":" "}`,
		`{"externalId":123}`, `{"externalId":"ERP","orderId":null}`, `{"externalId":"ERP","orderId":""}`,
		`{"externalId":"ERP\u0000X"}`, `{"externalId":"ERP","invoiceKind":"top_up"}`,
	} {
		require.Equal(t, 400, request(fmt.Sprint("external-invalid-", i), invalid).Code, invalid)
	}
	require.Equal(t, 409, request("external-conflict", `{"externalId":"another"}`).Code)
	after, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, before.Version+1, after.Version)
	require.Equal(t, before.CreditedAmount, after.CreditedAmount)
	require.Equal(t, before.Status, after.Status)
	require.Equal(t, before.InvoiceKind, after.InvoiceKind)
	var movements int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1`, p.paymentID).Scan(&movements))
	require.Equal(t, 1, movements)
	reorg := f.topUp(t, f.merchantID)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET status='reversed' WHERE id=$1;
		UPDATE doc_crypto_payments SET canonicality='orphaned',status='reorged' WHERE id=$2`, pgx.QueryExecModeSimpleProtocol, reorg.invoiceID, reorg.paymentID)
	require.NoError(t, err)
	topUp, err := f.repo.IsPersistentTopUp(f.ctx, f.merchantID, reorg.invoiceID)
	require.NoError(t, err)
	require.True(t, topUp)
	_, err = svc.BindMerchantInvoiceReference(f.ctx, cryptoapp.BindMerchantInvoiceReferenceInput{
		MerchantID: f.merchantID, InvoiceID: reorg.invoiceID, APIKeyID: f.keyID, ExternalID: reorg.externalID,
	})
	require.NoError(t, err)

	ordinary := f.topUp(t, f.merchantID)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices i SET invoice_kind='payment',external_id='topup_' || p.tx_hash
		FROM doc_crypto_payments p WHERE i.id=$1 AND p.invoice_id=i.id`, ordinary.invoiceID)
	require.NoError(t, err)
	topUp, err = f.repo.IsPersistentTopUp(f.ctx, f.merchantID, ordinary.invoiceID)
	require.NoError(t, err)
	require.False(t, topUp, "a marker and system source do not turn an ordinary invoice into a top-up")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET creation_source='portal' WHERE id=$1`, p.invoiceID)
	require.Error(t, err, "the database must reject a top-up created by the portal")
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET invoice_kind='unknown' WHERE id=$1`, p.invoiceID)
	require.Error(t, err, "the database must reject unknown classifications")
}

func TestInvoiceReferenceExternalCollisionsAndMultipleEventsOfOneTransaction(t *testing.T) {
	f := newReferenceFixture(t)
	svc := f.service(t, postgres.NewEventLogRepo())
	p1, p2 := f.topUp(t, f.merchantID), f.topUp(t, f.merchantID)
	_, err := f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET external_id='' WHERE id IN ($1,$2);
		UPDATE doc_crypto_payments SET tx_hash=(SELECT tx_hash FROM doc_crypto_payments WHERE id=$3),event_index=1 WHERE id=$4`,
		pgx.QueryExecModeSimpleProtocol, p1.invoiceID, p2.invoiceID, p1.paymentID, p2.paymentID)
	require.NoError(t, err)
	externalID := "ERP-race-" + id.Compact(p1.invoiceID)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, p := range []referenceTopUp{p1, p2} {
		go func() {
			<-start
			_, err := svc.BindMerchantInvoiceReference(f.ctx, cryptoapp.BindMerchantInvoiceReferenceInput{
				MerchantID: f.merchantID, InvoiceID: p.invoiceID, APIKeyID: f.keyID, ExternalID: externalID,
			})
			results <- err
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		if err := <-results; err == nil {
			successes++
		} else {
			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 409, appErr.HTTPStatus)
			conflicts++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	for _, p := range []referenceTopUp{p1, p2} {
		topUp, err := f.repo.IsPersistentTopUp(f.ctx, f.merchantID, p.invoiceID)
		require.NoError(t, err)
		require.True(t, topUp)
	}
	other := f.topUp(t, f.otherMerchantID)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET external_id='' WHERE id=$1`, other.invoiceID)
	require.NoError(t, err)
	_, err = svc.BindMerchantInvoiceReference(f.ctx, cryptoapp.BindMerchantInvoiceReferenceInput{
		MerchantID: f.otherMerchantID, InvoiceID: other.invoiceID, APIKeyID: f.keyID, ExternalID: externalID,
	})
	require.NoError(t, err, "another merchant may use the same reference")
}

func TestInvoiceReferenceConcurrentCollisionsAndTenantOwnership(t *testing.T) {
	f := newReferenceFixture(t)
	svc := f.service(t, postgres.NewEventLogRepo())
	for _, sameInvoice := range []bool{false, true} {
		p1 := f.topUp(t, f.merchantID)
		p2 := p1
		order1 := "race-" + id.Compact(p1.invoiceID)
		order2 := order1
		if sameInvoice {
			order2 += "-other"
		} else {
			p2 = f.topUp(t, f.merchantID)
		}
		start := make(chan struct{})
		results := make(chan error, 2)
		go func() { <-start; _, err := f.bind(svc, p1, f.merchantID, order1, nil); results <- err }()
		go func() { <-start; _, err := f.bind(svc, p2, f.merchantID, order2, nil); results <- err }()
		close(start)
		successes, conflicts := 0, 0
		for range 2 {
			err := <-results
			if err == nil {
				successes++
				continue
			}
			var appErr *apperror.AppError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, 409, appErr.HTTPStatus)
			conflicts++
		}
		require.Equal(t, 1, successes)
		require.Equal(t, 1, conflicts)
	}
	p1, p2 := f.topUp(t, f.merchantID), f.topUp(t, f.otherMerchantID)
	order := "shared-" + id.Compact(p1.invoiceID)
	_, err := f.bind(svc, p1, f.merchantID, order, nil)
	require.NoError(t, err)
	_, err = f.bind(svc, p2, f.otherMerchantID, order, nil)
	require.NoError(t, err)
	_, err = f.bind(svc, p1, f.otherMerchantID, "forbidden", nil)
	var appErr *apperror.AppError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 404, appErr.HTTPStatus)
	_, err = f.pool.Exec(f.ctx, `UPDATE cat_wallets SET allocation_mode='transient' WHERE id=$1`, p1.walletID)
	require.NoError(t, err)
	_, err = f.bind(svc, p1, f.merchantID, order, nil)
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, 409, appErr.HTTPStatus)
}

type failingReferenceAudit struct{ eventlog.Writer }

func (w failingReferenceAudit) Write(ctx context.Context, event eventlog.Event) error {
	if err := w.Writer.Write(ctx, event); err != nil {
		return err
	}
	return errors.New("injected reference audit failure")
}

func TestInvoiceReferenceAuditFailureRollsBackAndSettlementRacePreservesCredit(t *testing.T) {
	f := newReferenceFixture(t)
	p := f.topUp(t, f.merchantID)
	before, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	_, err = f.bind(f.service(t, failingReferenceAudit{postgres.NewEventLogRepo()}), p, f.merchantID, "ROLLBACK", nil)
	require.Error(t, err)
	after, err := f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, before.Version, after.Version)
	require.Empty(t, after.OrderID)
	var audits int
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM sys_event_log WHERE entity_id=$1`, p.invoiceID).Scan(&audits))
	require.Zero(t, audits)
	svc := f.service(t, postgres.NewEventLogRepo())
	start, results := make(chan struct{}), make(chan error, 2)
	go func() { <-start; _, err := f.bind(svc, p, f.merchantID, "ORD-SETTLE", nil); results <- err }()
	go func() { <-start; results <- f.settle(t, p) }()
	close(start)
	for range 2 {
		require.NoError(t, <-results)
	}
	after, err = f.repo.GetByID(f.ctx, p.invoiceID)
	require.NoError(t, err)
	require.Equal(t, "ORD-SETTLE", after.OrderID)
	require.Equal(t, p.externalID, after.ExternalID)
	require.Equal(t, crypto_invoice.InvoiceStatusCompleted, after.Status)
	require.Equal(t, "100", after.CreditedAmount.String())
	var movements int
	var balance string
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT COUNT(*) FROM reg_crypto_merchant_balance_movements WHERE recorder_id=$1`, p.paymentID).Scan(&movements))
	require.Equal(t, 1, movements)
	require.NoError(t, f.pool.QueryRow(f.ctx, `SELECT amount::text FROM reg_crypto_merchant_balance_balances WHERE merchant_id=$1 AND token_id=$2`, f.merchantID, f.tokenID).Scan(&balance))
	require.Equal(t, "100", balance)
	_, err = f.pool.Exec(f.ctx, `UPDATE doc_crypto_invoices SET status='reversed' WHERE id=$1; UPDATE doc_crypto_payments SET status='reorged',canonicality='orphaned' WHERE id=$2`, pgx.QueryExecModeSimpleProtocol, p.invoiceID, p.paymentID)
	require.NoError(t, err)
	_, err = f.bind(svc, p, f.merchantID, "ORD-SETTLE", map[string]string{"reorg": "accounted"})
	require.NoError(t, err)
}
