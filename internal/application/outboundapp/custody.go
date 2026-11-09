package outboundapp

import (
	"context"
	"fmt"
	"strings"

	"metapus/internal/core/id"
	"metapus/internal/domain/crypto"
)

type CustodyWallet struct {
	ID         id.ID
	NetworkID  id.ID
	Address    string
	VaultKeyID string
	Role       string
	MerchantID *id.ID
	Family     string
}

type CustodyRepository interface {
	LoadWallet(context.Context, id.ID) (CustodyWallet, error)
	FindWallet(context.Context, id.ID, string) (CustodyWallet, error)
	IsEnforced(context.Context, id.ID) (bool, error)
	IsManagedDestination(context.Context, id.ID, string, string) (bool, error)
	RequireTransferPlan(context.Context, crypto.TransferIntent) error
	HasAML(context.Context, id.ID) (bool, error)
	BeginDispatch(context.Context, string, string) error
	FindFeeWalletForOwner(context.Context, id.ID, *id.ID) (id.ID, error)
	LoadFundingWallet(context.Context, id.ID, id.ID, string) (HotWallet, error)
}
type CustodyService struct{ repo CustodyRepository }

func NewCustodyService(repo CustodyRepository) *CustodyService { return &CustodyService{repo} }
func (s *CustodyService) BeginDispatch(ctx context.Context, id, stage string) error {
	return s.repo.BeginDispatch(ctx, id, stage)
}

// PrepareTransferCustody is called before resources, authorization and dispatch.
// Ownership is immutable in PostgreSQL, and Vault enforces its independent
// registry. This function does not filter existing attempts out of recovery.
func (s *CustodyService) PrepareTransferCustody(ctx context.Context, intent crypto.TransferIntent, expected *id.ID) (*id.ID, error) {
	source, err := s.repo.LoadWallet(ctx, intent.SourceWalletID)
	if err != nil {
		return nil, err
	}
	addressMatches := source.Address == intent.SourceAddress
	if source.Family == "evm" {
		addressMatches = strings.EqualFold(source.Address, intent.SourceAddress)
	}
	if source.NetworkID != intent.Network.ID || !addressMatches || source.Address == "" || source.VaultKeyID == "" {
		return nil, fmt.Errorf("%w: source network/address mismatch", ErrMerchantCustody)
	}
	enforced, err := s.repo.IsEnforced(ctx, intent.Network.ID)
	if err != nil {
		return nil, err
	}
	if expected != nil && (id.IsNil(*expected) || !crypto.CustodyOwnerMatchesMerchant(source.MerchantID, *expected, enforced)) {
		return nil, fmt.Errorf("%w: source does not belong to requested merchant", ErrMerchantCustody)
	}
	if source.MerchantID != nil {
		if err := s.repo.RequireTransferPlan(ctx, intent); err != nil {
			return nil, err
		}
	}
	switch intent.Purpose {
	case crypto.SigningPurposeWithdrawal:
		if expected == nil || (source.Role != "liquidity_hot" && source.Role != "merchant_pool") {
			return nil, fmt.Errorf("%w: withdrawal requires its merchant and principal source", ErrMerchantCustody)
		}
		managed, err := s.repo.IsManagedDestination(ctx, intent.Network.ID, intent.DestinationAddress, source.Family)
		if err != nil {
			return nil, err
		}
		if managed {
			return nil, fmt.Errorf("%w: withdrawal destination must be external", ErrMerchantCustody)
		}
		return expected, nil
	case crypto.SigningPurposeTreasury:
		if source.MerchantID != nil || (source.Role != "treasury" && (enforced || source.Role != "liquidity_hot")) {
			return nil, fmt.Errorf("%w: treasury cannot spend merchant principal", ErrMerchantCustody)
		}
		return nil, nil
	case crypto.SigningPurposeSweep, crypto.SigningPurposeNativeFunding, crypto.SigningPurposeActivation:
		destination, err := s.repo.FindWallet(ctx, intent.Network.ID, intent.DestinationAddress)
		if err != nil {
			return nil, err
		}
		if destination.Address == "" || destination.VaultKeyID == "" || source.ID == destination.ID || !crypto.SameCustodyOwner(source.MerchantID, destination.MerchantID) || (enforced && source.MerchantID == nil) {
			return nil, fmt.Errorf("%w: internal transfer crosses merchant ownership", ErrMerchantCustody)
		}
		if intent.Purpose == crypto.SigningPurposeSweep && (source.Role != "merchant_pool" || destination.Role != "liquidity_hot") {
			return nil, fmt.Errorf("%w: invalid sweep wallet roles", ErrMerchantCustody)
		}
		if intent.Purpose == crypto.SigningPurposeNativeFunding && source.Role != "network_fee" {
			return nil, fmt.Errorf("%w: native funding requires a network fee source", ErrMerchantCustody)
		}
		if intent.Purpose == crypto.SigningPurposeActivation && source.Role != "network_fee" && source.Role != "liquidity_hot" {
			return nil, fmt.Errorf("%w: invalid activation source role", ErrMerchantCustody)
		}
		return source.MerchantID, nil
	default:
		return nil, fmt.Errorf("%w: unsupported signing purpose", ErrMerchantCustody)
	}
}

// CheckCustody revalidates a stored authorization, including old signed bytes
// before a new broadcast. It never rewrites the authorization's merchant.
func (s *CustodyService) CheckCustody(ctx context.Context, auth crypto.SigningAuthorization) error {
	source, err := s.repo.FindWallet(ctx, auth.NetworkID, auth.SourceAddress)
	if err != nil {
		return err
	}
	if source.VaultKeyID != auth.VaultKeyID {
		return fmt.Errorf("%w: source signing key mismatch", ErrMerchantCustody)
	}
	intent := crypto.TransferIntent{BasisType: auth.BasisType, BasisID: auth.BasisID, Attempt: auth.Attempt, Generation: auth.Generation, Amount: auth.AmountMinorUnits, Purpose: auth.Purpose, SourceWalletID: source.ID, SourceAddress: auth.SourceAddress,
		DestinationAddress: auth.DestinationAddress, Network: crypto.NetworkRef{ID: auth.NetworkID}, Asset: crypto.AssetRef{ContractAddress: auth.ContractAddress}}
	owner, err := s.PrepareTransferCustody(ctx, intent, auth.MerchantID)
	if err != nil {
		return err
	}
	if !crypto.SameCustodyOwner(owner, auth.MerchantID) {
		return fmt.Errorf("%w: immutable authorization has no matching merchant binding", ErrMerchantCustody)
	}
	return nil
}

// ResolveNativeFundingSource selects the fee source registered to the exact
// custody owner. Enforced custody requires an explicit merchant owner.
func (s *CustodyService) ResolveNativeFundingSource(ctx context.Context, networkID, destinationID id.ID) (HotWallet, error) {
	destination, err := s.repo.LoadWallet(ctx, destinationID)
	if err != nil || destination.NetworkID != networkID {
		return HotWallet{}, fmt.Errorf("%w: funding destination scope is invalid", ErrMerchantCustody)
	}
	enforced, err := s.repo.IsEnforced(ctx, networkID)
	if err != nil {
		return HotWallet{}, err
	}
	if destination.MerchantID == nil && enforced {
		return HotWallet{}, fmt.Errorf("%w: funding destination has no custody owner", ErrMerchantCustody)
	}
	feeID, err := s.repo.FindFeeWalletForOwner(ctx, networkID, destination.MerchantID)
	if err != nil {
		return HotWallet{}, err
	}
	feeOwner, err := s.repo.LoadWallet(ctx, feeID)
	if err != nil {
		return HotWallet{}, err
	}
	if feeOwner.NetworkID != networkID || feeOwner.Role != "network_fee" || feeOwner.ID == destinationID || !crypto.SameCustodyOwner(feeOwner.MerchantID, destination.MerchantID) {
		return HotWallet{}, fmt.Errorf("%w: funding source has a different custody owner", ErrMerchantCustody)
	}
	return s.repo.LoadFundingWallet(ctx, feeID, networkID, "network_fee")
}

// CheckCustodySigner fails closed if an enforced SQL network is paired with a
// legacy plugin or a plugin that has not enabled its own independent policy.
func (s *CustodyService) CheckCustodySigner(ctx context.Context, network crypto.NetworkRef, signer crypto.PolicyAwareTransactionSigner) error {
	enforced, err := s.repo.IsEnforced(ctx, network.ID)
	if err != nil {
		return err
	}
	if !enforced {
		return nil
	}
	guard, ok := signer.(crypto.MerchantCustodySigner)
	if !ok {
		return fmt.Errorf("%w: signer has no merchant custody support", ErrMerchantCustody)
	}
	if err := guard.CheckMerchantCustodyReady(ctx, network); err != nil {
		return fmt.Errorf("%w: signer custody readiness: %v", ErrMerchantCustody, err)
	}
	amlEnabled, err := s.repo.HasAML(ctx, network.ID)
	if err != nil {
		return err
	}
	if amlEnabled {
		amlSigner, ok := signer.(crypto.AMLSigner)
		if !ok {
			return fmt.Errorf("%w: signer has no AML signing contract support", ErrMerchantCustody)
		}
		if err := amlSigner.CheckAMLReady(ctx, network); err != nil {
			return fmt.Errorf("%w: signer AML readiness: %w", ErrMerchantCustody, err)
		}
	}
	return nil
}

func (s *CustodyService) CheckCustodyDispatch(ctx context.Context, auth crypto.SigningAuthorization, registry *crypto.FamilySignerRegistry) error {
	return s.CheckCustodyDispatchGuarded(ctx, auth, registry, nil)
}
func (s *CustodyService) CheckCustodyDispatchGuarded(ctx context.Context, auth crypto.SigningAuthorization, registry *crypto.FamilySignerRegistry, guard SigningExecutionGuard) error {
	if err := requireActivationSigningGuard(auth.Purpose, guard); err != nil {
		return err
	}
	if err := s.CheckCustody(ctx, auth); err != nil {
		return err
	}
	if registry == nil {
		return fmt.Errorf("%w: signer registry unavailable", ErrMerchantCustody)
	}
	signer, err := registry.Resolve(auth.ChainFamily)
	if err != nil {
		return err
	}
	if err := s.CheckCustodySigner(ctx, crypto.NetworkRef{ID: auth.NetworkID, Family: auth.ChainFamily, NetworkIdentifier: auth.NetworkIdentifier}, signer); err != nil {
		return err
	}
	return runSigningDispatch(ctx, guard, auth, "broadcast", func(ctx context.Context) error { return s.repo.BeginDispatch(ctx, auth.AuthorizationID, "broadcast") })
}
