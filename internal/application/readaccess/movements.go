package readaccess

import (
	"context"
	"strings"
	"time"

	"metapus/internal/core/entity"
	"metapus/internal/core/id"
	"metapus/internal/core/security"
	"metapus/internal/domain"
)

type ReadSnapshot func(context.Context, func(context.Context) error) error

// ReadMovements authorizes the complete source document before any provider is
// called. Source, movement data and related presentations share one snapshot.
func (s *Service) ReadMovements(ctx context.Context, key string, uid id.ID, snapshot ReadSnapshot, authorize func(context.Context, id.ID) error, providers []entity.MovementProvider, resolver domain.RefResolver) ([]entity.DocumentMovement, error) {
	if _, err := s.Require(ctx, key); err != nil {
		return nil, err
	}
	result := make([]entity.DocumentMovement, 0)
	err := snapshot(ctx, func(ctx context.Context) error {
		if err := authorize(ctx, uid); err != nil {
			return err
		}
		for _, provider := range providers {
			rows, err := provider.GetDocumentMovements(ctx, uid)
			if err != nil {
				return err
			}
			for _, row := range rows {
				projected, ok := projectMovement(ctx, key, row)
				if ok {
					result = append(result, projected)
				}
			}
		}
		if resolver != nil {
			return EnrichMovementRefs(ctx, result, resolver)
		}
		for _, row := range result {
			for _, column := range row.Columns {
				if column.Type == "ref" || column.Type == "amount" {
					delete(row.Data, column.Key)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

var cryptoMovementSources = map[string]map[string][]string{
	"crypto_payment": {
		"merchant": {"merchant_id"}, "wallet": {"wallet_id"}, "token": {"token_id"},
		"amount": {"amount", "fee_fixed", "fee_percent_bp", "fee_min", "fee_max"}, "feeType": {"fee_fixed", "fee_percent_bp"},
	},
	"crypto_withdrawal": {
		"merchant": {"merchant_id"}, "wallet": {"source_wallet_id"}, "token": {"token_id", "network_fee_token_id"},
		"amount": {"amount", "network_fee"}, "feeType": {"network_fee"},
	},
	"crypto_sweep": {
		"wallet": {"hot_wallet_id", "lines.wallet_id"}, "token": {"token_id"},
		"amount": {"total_amount", "total_fee", "lines.amount", "lines.network_fee"},
	},
	"crypto_provider_balance_operation": {
		"providerAccount": {"provider_account_id"}, "token": {"token_id"}, "wallet": {"source_wallet_id"},
		"operationType": {"operation_type"}, "externalRef": {"external_ref"}, "amount": {"amount"},
	},
	"crypto_resource_rental": {
		"providerAccount": {"provider_account_id"}, "network": {"network_id"}, "wallet": {"receive_wallet_id"},
		"token": {"cost_token_id"}, "costToken": {"cost_token_id"}, "provider": {"provider"},
		"costType": {"resource_type"}, "purpose": {"purpose"}, "externalOrderId": {"external_order_id"},
		"amount": {"gross_cost_amount", "refund_amount", "net_cost_amount", "allocated_cost_amount", "unused_cost_amount"},
	},
	"crypto_network_operation": {
		"network": {"network_id"}, "wallet": {"source_wallet_id", "target_wallet_id"},
		"token": {"cost_token_id"}, "costToken": {"cost_token_id"}, "costType": {"operation_type"},
		"purpose": {"purpose"}, "provider": {"provider"}, "amount": {"cost_amount"},
	},
	"treasury_withdrawal": {
		"merchant": {"feeLines.merchantId"}, "feeType": {"feeLines.feeType"},
		"wallet": {"source_wallet_id"}, "token": {"token_id"}, "amount": {"amount", "network_fee", "feeLines.amount"},
	},
	"withdrawal_request": {
		"merchant": {"merchant_id"}, "token": {"token_id"}, "amount": {"amount"},
	},
	// A receipt adds custody and the unattributed remainder of its own transfer.
	"crypto_custody_receipt": {
		"wallet": {"wallet_id"}, "receipt": {"id"}, "token": {"token_id"}, "amount": {"amount"},
	},
	"crypto_merchant_credit": {
		"receipt": {"receipt_id"}, "merchant": {"merchant_id"}, "token": {"token_id"},
		"amount": {"amount", "net_amount", "processing_fee"}, "feeType": {"processing_fee"},
	},
	"crypto_platform_funding": {
		"receipt": {"receipt_id"}, "token": {"token_id"}, "amount": {"amount"},
	},
}

func sourceFieldAllowed(policy *security.FieldPolicy, path string) bool {
	if part, field, ok := strings.Cut(path, "."); ok {
		return policy.IsFieldAllowed(path) && policy.IsFieldAllowed(part) && policy.IsTablePartFieldAllowed(part, field)
	}
	return policy.IsFieldAllowed(path)
}

func projectMovement(ctx context.Context, source string, row entity.DocumentMovement) (entity.DocumentMovement, bool) {
	sourcePolicy := security.GetFieldPolicy(ctx, source, "read")
	registerPolicy := security.GetFieldPolicy(ctx, row.PolicyEntity, "read")
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("record_type") {
		row.RecordType = ""
	}
	if registerPolicy != nil {
		if !registerPolicy.IsFieldAllowed("period") {
			row.Period = time.Time{}
		}
		if !registerPolicy.IsFieldAllowed("record_type") {
			row.RecordType = ""
		}
	}
	dependencies := cryptoMovementSources[source]
	// Presence of a movement would disclose the posting state itself.
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("posted") {
		return entity.DocumentMovement{}, false
	}
	if sourcePolicy != nil && !sourcePolicy.IsFieldAllowed("date") {
		row.Period = time.Time{}
	}
	columns := make([]entity.MovementColumnDef, 0, len(row.Columns))
	values := make(map[string]any)
	for _, column := range row.Columns {
		paths, mapped := dependencies[column.Key]
		allowed := true
		if sourcePolicy != nil {
			allowed = mapped
			for _, path := range paths {
				allowed = allowed && sourceFieldAllowed(sourcePolicy, path)
			}
		}
		if registerPolicy != nil {
			allowed = allowed && column.PolicyField != "" && registerPolicy.IsFieldAllowed(column.PolicyField)
		}
		if allowed {
			columns = append(columns, column)
			if value, ok := row.Data[column.Key]; ok {
				values[column.Key] = value
			}
		}
	}
	row.Columns, row.Data = columns, values
	return row, len(columns) > 0
}
