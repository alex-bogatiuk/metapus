package outbound_repo

import "metapus/internal/application/outboundapp"

// Persistence implements application contracts without depending on worker runtime.
var (
	_ outboundapp.WithdrawalRepository                = (*OutboundRepo)(nil)
	_ outboundapp.SweepRepository                     = (*OutboundRepo)(nil)
	_ outboundapp.TreasuryRepository                  = (*OutboundRepo)(nil)
	_ outboundapp.SigningAuthorizationRepository      = (*OutboundRepo)(nil)
	_ outboundapp.ActivationRepository                = (*OutboundRepo)(nil)
	_ outboundapp.SettlementRepository                = (*OutboundRepo)(nil)
	_ outboundapp.ConfirmationClaims                  = (*OutboundRepo)(nil)
	_ outboundapp.NativeFundingRepository             = NativeFundingStore{}
	_ outboundapp.NativeFundingConfirmationRepository = NativeFundingConfirmationStore{}
	_ outboundapp.CustodyRepository                   = CustodyStore{}
	_ outboundapp.ActivationWorkflowRepository        = ActivationWorkflowStore{}
	_ outboundapp.FeeBudget                           = (*FeeBudgetManager)(nil)
	_ outboundapp.ReplacementFees                     = (*FeeBudgetManager)(nil)
	_ outboundapp.ActivationFees                      = (*FeeBudgetManager)(nil)
	_ outboundapp.DocumentFeeSettlement               = (*FeeBudgetManager)(nil)
)
