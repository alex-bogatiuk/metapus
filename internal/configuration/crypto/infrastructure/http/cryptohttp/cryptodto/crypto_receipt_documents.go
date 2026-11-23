package cryptodto

import (
	"time"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_custody_receipt"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_merchant_credit"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/crypto_platform_funding"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/entity"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/http/dto"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
)

// The custody receipt and its manual attributions are system-owned documents:
// the API lists and opens them read-only. Their posting line identities stay
// internal; references carry display names next to their IDs.

func documentResponse(d entity.Document) dto.DocumentResponse {
	return dto.DocumentResponse{
		BaseResponse: dto.BaseResponse{ID: d.ID.String(), DeletionMark: d.DeletionMark, Version: d.Version,
			CreatedAt: d.CreatedAt, UpdatedAt: d.UpdatedAt},
		Number: d.Number, Date: d.Date, Posted: d.Posted, PostedVersion: d.PostedVersion, Description: d.Description,
	}
}

func optionalID(v *id.ID) *string {
	if v == nil {
		return nil
	}
	s := v.String()
	return &s
}

// CryptoCustodyReceiptResponse is a proven external receipt into custody.
type CryptoCustodyReceiptResponse struct {
	dto.DocumentResponse
	NetworkID         string  `json:"networkId"`
	WalletID          string  `json:"walletId"`
	TokenID           string  `json:"tokenId"`
	CustodyMerchantID *string `json:"custodyMerchantId,omitempty"`
	VerificationID    string  `json:"verificationId"`
	Amount            string  `json:"amount"`
	ReasonCode        string  `json:"reasonCode"`

	Network         *postgres.RefDisplay `json:"network,omitempty"`
	Wallet          *postgres.RefDisplay `json:"wallet,omitempty"`
	Token           *postgres.RefDisplay `json:"token,omitempty"`
	CustodyMerchant *postgres.RefDisplay `json:"custodyMerchant,omitempty"`
}

func CollectCryptoCustodyReceiptRefs(resolver *postgres.ReferenceResolver, r *crypto_custody_receipt.CryptoCustodyReceipt) {
	resolver.Add(TableBlockchainNetworks, r.NetworkID)
	resolver.Add(TableWallets, r.WalletID)
	resolver.Add(TableTokens, r.TokenID)
	resolver.AddPtr(TableMerchants, r.CustodyMerchantID)
}

func FromCryptoCustodyReceipt(r *crypto_custody_receipt.CryptoCustodyReceipt, refs postgres.ResolvedRefs) CryptoCustodyReceiptResponse {
	resp := CryptoCustodyReceiptResponse{
		DocumentResponse: documentResponse(r.Document),
		NetworkID:        r.NetworkID.String(), WalletID: r.WalletID.String(), TokenID: r.TokenID.String(),
		CustodyMerchantID: optionalID(r.CustodyMerchantID), VerificationID: r.VerificationID.String(),
		Amount: r.Amount.String(), ReasonCode: string(r.ReasonCode),
	}
	if refs != nil {
		network, wallet, token := refs.Get(TableBlockchainNetworks, r.NetworkID), refs.Get(TableWallets, r.WalletID), refs.Get(TableTokens, r.TokenID)
		resp.Network, resp.Wallet, resp.Token = &network, &wallet, &token
		resp.CustodyMerchant = refs.GetPtr(TableMerchants, r.CustodyMerchantID)
	}
	return resp
}

// CryptoMerchantCreditResponse attributes a receipt remainder to a merchant.
type CryptoMerchantCreditResponse struct {
	dto.DocumentResponse
	ReceiptID        string     `json:"receiptId"`
	MerchantID       string     `json:"merchantId"`
	InvoiceID        *string    `json:"invoiceId,omitempty"`
	TokenID          string     `json:"tokenId"`
	Amount           string     `json:"amount"`
	NetAmount        string     `json:"netAmount"`
	ProcessingFee    string     `json:"processingFee"`
	FeeFixed         string     `json:"feeFixed"`
	FeePercentBP     int        `json:"feePercentBp"`
	FeeMin           string     `json:"feeMin"`
	FeeMax           string     `json:"feeMax"`
	Status           string     `json:"status"`
	ApprovalRequired bool       `json:"approvalRequired"`
	PreparedBy       string     `json:"preparedBy"`
	PreparedAt       time.Time  `json:"preparedAt"`
	DecidedBy        *string    `json:"decidedBy,omitempty"`
	DecidedAt        *time.Time `json:"decidedAt,omitempty"`
	DecisionReason   string     `json:"decisionReason,omitempty"`

	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
	Token    *postgres.RefDisplay `json:"token,omitempty"`
}

func CollectCryptoMerchantCreditRefs(resolver *postgres.ReferenceResolver, c *crypto_merchant_credit.CryptoMerchantCredit) {
	resolver.Add(TableMerchants, c.MerchantID)
	resolver.Add(TableTokens, c.TokenID)
}

func FromCryptoMerchantCredit(c *crypto_merchant_credit.CryptoMerchantCredit, refs postgres.ResolvedRefs) CryptoMerchantCreditResponse {
	resp := CryptoMerchantCreditResponse{
		DocumentResponse: documentResponse(c.Document),
		ReceiptID:        c.ReceiptID.String(), MerchantID: c.MerchantID.String(), InvoiceID: optionalID(c.InvoiceID),
		TokenID: c.TokenID.String(), Amount: c.Amount.String(), NetAmount: c.NetAmount.String(),
		ProcessingFee: c.ProcessingFee.String(), FeeFixed: c.FeeFixed.String(), FeePercentBP: c.FeePercentBP,
		FeeMin: c.FeeMin.String(), FeeMax: c.FeeMax.String(), Status: string(c.Status), ApprovalRequired: c.ApprovalRequired,
		PreparedBy: c.PreparedBy.String(), PreparedAt: c.PreparedAt, DecidedBy: optionalID(c.DecidedBy), DecidedAt: c.DecidedAt,
		DecisionReason: c.DecisionReason,
	}
	if refs != nil {
		merchant, token := refs.Get(TableMerchants, c.MerchantID), refs.Get(TableTokens, c.TokenID)
		resp.Merchant, resp.Token = &merchant, &token
	}
	return resp
}

// CryptoPlatformFundingResponse takes a receipt remainder as platform money.
type CryptoPlatformFundingResponse struct {
	dto.DocumentResponse
	ReceiptID                string     `json:"receiptId"`
	WalletID                 string     `json:"walletId"`
	TokenID                  string     `json:"tokenId"`
	Amount                   string     `json:"amount"`
	Reason                   string     `json:"reason"`
	MerchantID               *string    `json:"merchantId,omitempty"`
	MerchantLink             *string    `json:"merchantLink,omitempty"`
	MerchantLinkAcknowledged bool       `json:"merchantLinkAcknowledged"`
	Status                   string     `json:"status"`
	PreparedBy               string     `json:"preparedBy"`
	PreparedAt               time.Time  `json:"preparedAt"`
	DecidedBy                *string    `json:"decidedBy,omitempty"`
	DecidedAt                *time.Time `json:"decidedAt,omitempty"`
	DecisionReason           string     `json:"decisionReason,omitempty"`

	Wallet   *postgres.RefDisplay `json:"wallet,omitempty"`
	Token    *postgres.RefDisplay `json:"token,omitempty"`
	Merchant *postgres.RefDisplay `json:"merchant,omitempty"`
}

func CollectCryptoPlatformFundingRefs(resolver *postgres.ReferenceResolver, f *crypto_platform_funding.CryptoPlatformFunding) {
	resolver.Add(TableWallets, f.WalletID)
	resolver.Add(TableTokens, f.TokenID)
	resolver.AddPtr(TableMerchants, f.MerchantID)
}

func FromCryptoPlatformFunding(f *crypto_platform_funding.CryptoPlatformFunding, refs postgres.ResolvedRefs) CryptoPlatformFundingResponse {
	resp := CryptoPlatformFundingResponse{
		DocumentResponse: documentResponse(f.Document),
		ReceiptID:        f.ReceiptID.String(), WalletID: f.WalletID.String(), TokenID: f.TokenID.String(),
		Amount: f.Amount.String(), Reason: f.Reason, MerchantID: optionalID(f.MerchantID),
		MerchantLinkAcknowledged: f.MerchantLinkAcknowledged, Status: string(f.Status),
		PreparedBy: f.PreparedBy.String(), PreparedAt: f.PreparedAt, DecidedBy: optionalID(f.DecidedBy), DecidedAt: f.DecidedAt,
		DecisionReason: f.DecisionReason,
	}
	if f.MerchantLink != nil {
		link := string(*f.MerchantLink)
		resp.MerchantLink = &link
	}
	if refs != nil {
		wallet, token := refs.Get(TableWallets, f.WalletID), refs.Get(TableTokens, f.TokenID)
		resp.Wallet, resp.Token = &wallet, &token
		resp.Merchant = refs.GetPtr(TableMerchants, f.MerchantID)
	}
	return resp
}
