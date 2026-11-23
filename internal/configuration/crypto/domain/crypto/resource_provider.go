package crypto

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/json"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/types"
)

const ResourceProviderITRX = "itrx"

type ResourceKind string

const ResourceKindEnergy ResourceKind = "energy"

func (k ResourceKind) Valid() bool {
	return k == ResourceKindEnergy
}

type ResourceOrderStatus string

const (
	ResourceOrderPending    ResourceOrderStatus = "pending"
	ResourceOrderActive     ResourceOrderStatus = "active"
	ResourceOrderReclaiming ResourceOrderStatus = "reclaiming"
	ResourceOrderClosed     ResourceOrderStatus = "closed"
	ResourceOrderFailed     ResourceOrderStatus = "failed"
)

func (s ResourceOrderStatus) Valid() bool {
	switch s {
	case ResourceOrderPending, ResourceOrderActive, ResourceOrderReclaiming, ResourceOrderClosed, ResourceOrderFailed:
		return true
	default:
		return false
	}
}

func (s ResourceOrderStatus) Terminal() bool {
	return s == ResourceOrderClosed || s == ResourceOrderFailed
}

var (
	ErrResourceProviderTransient    = errors.New("resource provider is transiently unavailable")
	ErrResourceProviderUnauthorized = errors.New("resource provider authorization failed")
	ErrResourceProviderInvalidReply = errors.New("resource provider returned an invalid response")
	ErrResourceOrderAmbiguous       = errors.New("resource provider order status is ambiguous")
	ErrResourceProviderNotFound     = errors.New("resource provider not found")
	// ErrResourceOrderNotFound is the provider's explicit answer that it has no
	// order for the client order ID: unlike a transport failure it is evidence.
	ErrResourceOrderNotFound = errors.New("resource provider has no such order")
)

// ExternalResourceCapabilities is the compile-time contract of one vendor
// adapter. Runtime credentials and operator policy are separate.
type ExternalResourceCapabilities struct {
	Code                         string         `json:"code"`
	Family                       ChainFamily    `json:"family"`
	ResourceKinds                []ResourceKind `json:"resourceKinds"`
	ReusableFixedLease           bool           `json:"reusableFixedLease"`
	SupportsIdempotentPlace      bool           `json:"supportsIdempotentPlace"`
	SupportsQueryByClientOrderID bool           `json:"supportsQueryByClientOrderID"`
	SupportsReclaim              bool           `json:"supportsReclaim"`
	DefaultPeriod                string         `json:"defaultPeriod"`
	DefaultDuration              time.Duration  `json:"defaultDuration"`
	MinQuantity                  int64          `json:"minQuantity"`
}

func (c ExternalResourceCapabilities) Validate() error {
	if err := ValidateResourceProviderCode(c.Code); err != nil {
		return err
	}
	if !c.Family.Valid() {
		return errors.New("resource provider capabilities require a valid chain family")
	}
	if len(c.ResourceKinds) == 0 {
		return errors.New("resource provider capabilities require at least one resource kind")
	}
	seen := make(map[ResourceKind]struct{}, len(c.ResourceKinds))
	for _, kind := range c.ResourceKinds {
		if !kind.Valid() {
			return fmt.Errorf("resource provider capabilities have invalid resource kind %q", kind)
		}
		if _, duplicate := seen[kind]; duplicate {
			return fmt.Errorf("resource provider capabilities duplicate resource kind %q", kind)
		}
		seen[kind] = struct{}{}
	}
	if strings.TrimSpace(c.DefaultPeriod) == "" || c.DefaultDuration <= 0 {
		return errors.New("resource provider capabilities require a default period and duration")
	}
	if c.MinQuantity < 0 {
		return errors.New("resource provider capabilities min quantity cannot be negative")
	}
	return nil
}

func ValidateResourceProviderCode(code string) error {
	if code == "" {
		return errors.New("resource provider code is required")
	}
	if code[0] < 'a' || code[0] > 'z' {
		return fmt.Errorf("resource provider code %q must start with a lowercase letter", code)
	}
	for i := 1; i < len(code); i++ {
		ch := code[i]
		if (ch < 'a' || ch > 'z') && (ch < '0' || ch > '9') && ch != '_' {
			return fmt.Errorf("resource provider code %q is invalid", code)
		}
	}
	return nil
}

type ResourceOrderIntent struct {
	ClientOrderID  string
	ResourceKind   ResourceKind
	Quantity       int64
	ReceiveAddress string
	Period         string
}

type ResourceQuote struct {
	Quantity      int64
	EstimatedCost types.RawAmount
	ExpiresAt     time.Time
}

type ResourceOrderRef struct {
	ClientOrderID string
	ExternalID    string
}

type ResourceOrder struct {
	Provider        string
	ClientOrderID   string
	ExternalID      string
	Status          ResourceOrderStatus
	DelegatedAmount int64
	CostAmount      types.RawAmount
	RefundAmount    types.RawAmount
	ProviderBalance *types.RawAmount
	ExpiresAt       time.Time
	RequestJSON     json.RawMessage
	ResponseJSON    json.RawMessage
}

type ProviderAccountRef struct {
	Provider  string
	NetworkID id.ID
}

// ExternalResourceProvider is the vendor-neutral port for chain-external
// resource rental. Adapters map proprietary HTTP onto these operations.
type ExternalResourceProvider interface {
	Code() string
	Capabilities() ExternalResourceCapabilities
	Quote(ctx context.Context, intent ResourceOrderIntent) (ResourceQuote, error)
	Place(ctx context.Context, intent ResourceOrderIntent) (ResourceOrder, error)
	Query(ctx context.Context, ref ResourceOrderRef) (ResourceOrder, error)
	Reclaim(ctx context.Context, ref ResourceOrderRef) (ResourceOrder, error)
	Balance(ctx context.Context, account ProviderAccountRef) (types.RawAmount, error)
}
