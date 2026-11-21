package getblocknet

import (
	"slices"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"metapus/internal/configuration/crypto/domain/crypto/aml"
	"metapus/internal/core/id"
	"metapus/internal/core/json"
)

type vendorReport struct {
	RiskScore json.RawMessage            `json:"riskscore"`
	Signals   map[string]json.RawMessage `json:"signals"`
}

func parseFraction(raw json.RawMessage) (decimal.Decimal, error) {
	value := strings.TrimSpace(string(raw))
	if value == "" || value == "null" || len(value) > 96 || strings.HasPrefix(value, "\"") {
		return decimal.Zero, &ProtocolError{Kind: "invalid_fraction"}
	}
	number, err := decimal.NewFromString(value)
	if err != nil || number.Exponent() < -100 || number.Exponent() > 100 || number.IsNegative() || number.GreaterThan(decimal.NewFromInt(1)) {
		return decimal.Zero, &ProtocolError{Kind: "invalid_fraction"}
	}
	return number, nil
}

func normalizeReport(check vendorCheck, ticket aml.Ticket, contract string, raw []byte, receivedAt time.Time) (aml.Report, error) {
	if check.Report == nil || len(check.Report.Signals) == 0 {
		return aml.Report{}, &ProtocolError{Kind: "missing_report"}
	}
	fraction, err := parseFraction(check.Report.RiskScore)
	if err != nil {
		return aml.Report{}, err
	}
	completedAt, err := time.Parse(time.RFC3339, check.ResultDate)
	if err != nil {
		return aml.Report{}, &ProtocolError{Kind: "completion_time"}
	}
	// Round upwards: sub-basis-point risk must not cross a threshold downwards.
	score := aml.ScoreBP(fraction.Mul(decimal.NewFromInt(10000)).Ceil().IntPart())
	names := make([]string, 0, len(check.Report.Signals))
	for name := range check.Report.Signals {
		names = append(names, name)
	}
	slices.Sort(names)
	signals := make([]aml.Signal, 0, len(names))
	for _, name := range names {
		if name == "" {
			return aml.Report{}, &ProtocolError{Kind: "signal_shape"}
		}
		value, err := parseFraction(check.Report.Signals[name])
		if err != nil {
			return aml.Report{}, err
		}
		signals = append(signals, aml.Signal{Name: name, Value: value.String()})
	}
	coverage := ticket.Request.Kind == aml.AddressSubject ||
		(ticket.Request.MatchingEffects == 1 && ticket.Request.EventKind != "internal_value")
	return aml.Report{
		ID: id.New(), Provider: ProviderName, ProviderAccountID: ticket.ProviderAccountID, CheckHash: ticket.CheckHash,
		RequestDigest: ticket.RequestDigest, ContractVersion: contract, AdapterVersion: _adapter, MappingVersion: ticket.MappingVersion,
		Score: &score, Signals: signals, CoverageAccepted: coverage,
		CompletedAt: completedAt.UTC(), ReceivedAt: receivedAt, RawResponse: slices.Clone(raw),
	}, nil
}
