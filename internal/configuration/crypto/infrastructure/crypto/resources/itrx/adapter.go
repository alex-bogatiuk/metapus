package itrx

import (
	"context"
	"fmt"
	"time"

	"metapus/internal/configuration/crypto/domain/crypto"
	"metapus/pkg/sdk/types"
)

// Adapter maps the iTRX energy API onto crypto.ExternalResourceProvider.
type Adapter struct {
	client      *client
	callbackURL string
}

func New(cfg Config) (*Adapter, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &Adapter{client: newClient(cfg), callbackURL: cfg.CallbackURL}, nil
}

func Capabilities() crypto.ExternalResourceCapabilities {
	return crypto.ExternalResourceCapabilities{
		Code:                         crypto.ResourceProviderITRX,
		Family:                       crypto.ChainFamilyTRON,
		ResourceKinds:                []crypto.ResourceKind{crypto.ResourceKindEnergy},
		ReusableFixedLease:           true,
		SupportsIdempotentPlace:      true,
		SupportsQueryByClientOrderID: true,
		SupportsReclaim:              true,
		DefaultPeriod:                "1H",
		DefaultDuration:              time.Hour,
		MinQuantity:                  1,
	}
}

func (a *Adapter) Code() string { return crypto.ResourceProviderITRX }

func (a *Adapter) Capabilities() crypto.ExternalResourceCapabilities { return Capabilities() }

func (a *Adapter) Quote(_ context.Context, intent crypto.ResourceOrderIntent) (crypto.ResourceQuote, error) {
	return crypto.ResourceQuote{Quantity: intent.Quantity}, nil
}

func (a *Adapter) Place(ctx context.Context, intent crypto.ResourceOrderIntent) (crypto.ResourceOrder, error) {
	req, err := a.placeRequest(intent)
	if err != nil {
		return crypto.ResourceOrder{}, err
	}
	requestJSON, err := canonicalJSON(req)
	if err != nil {
		return crypto.ResourceOrder{}, err
	}
	resp, err := a.client.PlaceEnergyOrder(ctx, req)
	order := a.orderFromPlace(intent, requestJSON, resp)
	if err != nil {
		return order, err
	}
	return order, nil
}

func (a *Adapter) Query(ctx context.Context, ref crypto.ResourceOrderRef) (crypto.ResourceOrder, error) {
	if ref.ExternalID == "" && ref.ClientOrderID == "" {
		return crypto.ResourceOrder{}, fmt.Errorf("itrx query requires serial or out_trade_no")
	}
	resp, err := a.client.QueryOrder(ctx, ref.ExternalID, ref.ClientOrderID)
	if err != nil {
		return crypto.ResourceOrder{}, err
	}
	return a.orderFromQuery(ref, resp), nil
}

func (a *Adapter) Reclaim(ctx context.Context, ref crypto.ResourceOrderRef) (crypto.ResourceOrder, error) {
	if ref.ExternalID == "" {
		return crypto.ResourceOrder{}, fmt.Errorf("itrx reclaim serial is required")
	}
	resp, err := a.client.ReclaimOrder(ctx, ref.ExternalID)
	if err != nil {
		return crypto.ResourceOrder{Provider: crypto.ResourceProviderITRX, ClientOrderID: ref.ClientOrderID, ExternalID: ref.ExternalID, ResponseJSON: resp.RawJSON}, err
	}
	return crypto.ResourceOrder{
		Provider:      crypto.ResourceProviderITRX,
		ClientOrderID: ref.ClientOrderID,
		ExternalID:    ref.ExternalID,
		Status:        crypto.ResourceOrderReclaiming,
		ResponseJSON:  resp.RawJSON,
	}, nil
}

func (a *Adapter) Balance(context.Context, crypto.ProviderAccountRef) (types.RawAmount, error) {
	return types.ZeroRawAmount(), fmt.Errorf("itrx balance is observed from order replies")
}

func (a *Adapter) placeRequest(intent crypto.ResourceOrderIntent) (energyOrderRequest, error) {
	if intent.Quantity <= 0 || intent.ReceiveAddress == "" || intent.ClientOrderID == "" {
		return energyOrderRequest{}, fmt.Errorf("itrx energy order requires quantity, receive address, and client order id")
	}
	period := intent.Period
	if period == "" {
		period = Capabilities().DefaultPeriod
	}
	return energyOrderRequest{
		EnergyAmount:   intent.Quantity,
		Period:         period,
		ReceiveAddress: intent.ReceiveAddress,
		CallbackURL:    a.callbackURL,
		OutTradeNo:     intent.ClientOrderID,
	}, nil
}

func (a *Adapter) orderFromPlace(intent crypto.ResourceOrderIntent, requestJSON []byte, resp energyOrderResponse) crypto.ResourceOrder {
	order := crypto.ResourceOrder{
		Provider:        crypto.ResourceProviderITRX,
		ClientOrderID:   intent.ClientOrderID,
		ExternalID:      resp.Serial,
		Status:          crypto.ResourceOrderActive,
		DelegatedAmount: intent.Quantity,
		RequestJSON:     requestJSON,
		ResponseJSON:    resp.RawJSON,
	}
	if resp.Amount >= 0 {
		order.CostAmount = types.NewRawAmountFromInt64(resp.Amount)
	}
	order.ProviderBalance = optionalAmount(resp.Balance)
	return order
}

func (a *Adapter) orderFromQuery(ref crypto.ResourceOrderRef, resp orderQueryResponse) crypto.ResourceOrder {
	clientOrderID := ref.ClientOrderID
	delegated := resp.EnergyAmount
	order := crypto.ResourceOrder{
		Provider:        crypto.ResourceProviderITRX,
		ClientOrderID:   clientOrderID,
		ExternalID:      resp.OrderNo,
		Status:          mapITRXStatus(resp.Status),
		DelegatedAmount: delegated,
		ResponseJSON:    resp.RawJSON,
	}
	if resp.Amount >= 0 {
		order.CostAmount = types.NewRawAmountFromInt64(resp.Amount)
	}
	if resp.RefundAmount >= 0 {
		order.RefundAmount = types.NewRawAmountFromInt64(resp.RefundAmount)
	}
	order.ProviderBalance = optionalAmount(resp.Balance)
	return order
}

func mapITRXStatus(status int) crypto.ResourceOrderStatus {
	switch status {
	case 40, 41:
		return crypto.ResourceOrderClosed
	case 43:
		return crypto.ResourceOrderFailed
	default:
		return crypto.ResourceOrderActive
	}
}

func optionalAmount(value *int64) *types.RawAmount {
	if value == nil {
		return nil
	}
	if *value < 0 {
		return nil
	}
	amount := types.NewRawAmountFromInt64(*value)
	return &amount
}

var _ crypto.ExternalResourceProvider = (*Adapter)(nil)
