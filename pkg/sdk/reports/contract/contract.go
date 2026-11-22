// Package contract is the wire contract of reports.
package contract

import (
	core "metapus/internal/domain/reports/contract"
)

type (
	// MoneySources names the row fields that define a monetary column's unit.
	// Values are minor units; no report-wide currency scale is assumed.
	MoneySources = core.MoneySources
)

const (
	// AmountTypeRaw marks a money column of arbitrary-precision raw asset units
	// (types.RawAmount) instead of checked int64 minor units.
	AmountTypeRaw = core.AmountTypeRaw
)
