package cryptodto

import "metapus/internal/infrastructure/http/httpvalue"

type BindMerchantInvoiceReferenceRequest struct {
	OrderID    httpvalue.BusinessReference `json:"orderId,omitzero"`
	ExternalID httpvalue.BusinessReference `json:"externalId,omitzero"`
	Metadata   httpvalue.StringDictionary  `json:"metadata,omitzero"`
}
