// Package merchant provides the Merchant catalog.
// Merchants are business clients (shops) that accept crypto payments.
// M:N relationship with users via sys_merchant_users junction table.
package merchant

import (
	"context"

	"metapus/internal/core/apperror"
	"metapus/internal/core/entity"
	"metapus/internal/core/id"
)

// KYBStatus represents the Know-Your-Business verification state.
type KYBStatus string

const (
	KYBStatusPending  KYBStatus = "pending"  // ожидает верификации
	KYBStatusApproved KYBStatus = "approved" // верифицирован
	KYBStatusRejected KYBStatus = "rejected" // отклонён
)

// MerchantRole defines a user's role within a merchant context.
type MerchantRole int

const (
	_                        MerchantRole = iota
	MerchantRoleOwner                      // владелец — полный доступ
	MerchantRoleManager                    // менеджер — операции без настроек
	MerchantRoleViewer                     // наблюдатель — только чтение
)

// MerchantUser represents a user-merchant association with a role.
type MerchantUser struct {
	UserID     id.ID        `db:"user_id" json:"userId"`
	MerchantID id.ID        `db:"merchant_id" json:"merchantId"`
	Role       MerchantRole `db:"role" json:"role"`
}

// Merchant represents a business client that accepts crypto payments.
type Merchant struct {
	entity.Catalog

	// LegalName is the official legal entity name.
	LegalName string `db:"legal_name" json:"legalName" meta:"label:Юр. наименование"`

	// WebhookURL is the endpoint for payment event callbacks.
	WebhookURL string `db:"webhook_url" json:"webhookUrl" meta:"label:Webhook URL"`

	// CommissionRate in basis points (100 = 1%). Range [0, 10000].
	CommissionRate int `db:"commission_rate" json:"commissionRate" meta:"label:Комиссия (bp)"`

	// IsActive enables/disables the merchant for processing.
	IsActive bool `db:"is_active" json:"isActive" meta:"label:Активен"`

	// KYBStatus is the current verification status.
	KYBStatus KYBStatus `db:"kyb_status" json:"kybStatus" meta:"label:Статус KYB"`
}

const (
	_maxCommissionRate = 10000 // 100% in basis points
)

// NewMerchant creates a new Merchant with required fields.
func NewMerchant(code, name string) *Merchant {
	return &Merchant{
		Catalog:   entity.NewCatalog(code, name),
		IsActive:  true,
		KYBStatus: KYBStatusPending,
	}
}

// Validate implements entity.Validatable.
func (m *Merchant) Validate(ctx context.Context) error {
	if err := m.Catalog.Validate(ctx); err != nil {
		return err
	}

	if m.CommissionRate < 0 || m.CommissionRate > _maxCommissionRate {
		return apperror.NewValidation("commission rate must be between 0 and 10000 basis points").
			WithDetail("field", "commissionRate")
	}

	return nil
}
