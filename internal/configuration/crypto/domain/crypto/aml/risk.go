// Package aml defines merchant-scoped risk evidence and routing. Classification
// never changes financial ownership or creates another receipt.
package aml

import (
	"slices"
	"time"

	"github.com/shopspring/decimal"

	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
)

type Class string

const (
	Low     Class = "low"
	Medium  Class = "medium"
	High    Class = "high"
	Unknown Class = "unknown"
)

func (c Class) Valid() bool {
	return c == Low || c == Medium || c == High || c == Unknown
}

func Classes() []Class { return []Class{Low, Medium, High, Unknown} }

// ScoreBP is an index on the provider's accepted scale, not a probability.
type ScoreBP int

func (s ScoreBP) Valid() bool { return s >= 0 && s <= 10000 }

type Signal struct {
	Name  string
	Value string // exact vendor decimal; never a reconstructed model weight
}

type Report struct {
	ID                id.ID
	Provider          string
	ProviderAccountID id.ID
	CheckHash         string
	RequestDigest     string
	ContractVersion   string
	AdapterVersion    string
	MappingVersion    string
	Score             *ScoreBP // nil is missing evidence, not zero risk
	Signals           []Signal
	CoverageAccepted  bool
	CompletedAt       time.Time
	ReceivedAt        time.Time
	DatasetAsOf       *time.Time
	RawResponse       []byte
}

func (r Report) Clone() Report {
	r.Signals = slices.Clone(r.Signals)
	r.RawResponse = slices.Clone(r.RawResponse)
	if r.Score != nil {
		score := *r.Score
		r.Score = &score
	}
	if r.DatasetAsOf != nil {
		asOf := *r.DatasetAsOf
		r.DatasetAsOf = &asOf
	}
	return r
}

type Policy struct {
	Version         string
	LowMax          ScoreBP
	MediumMax       ScoreBP
	MaxReportAge    time.Duration
	DecisionTTL     time.Duration
	AcceptedSignals []string
	CriticalSignals []string
}

func (p Policy) Validate() error {
	if p.Version == "" || !p.LowMax.Valid() || !p.MediumMax.Valid() || p.LowMax >= p.MediumMax || p.MaxReportAge <= 0 || p.DecisionTTL <= 0 || len(p.AcceptedSignals) == 0 {
		return apperror.NewValidation("invalid AML routing policy")
	}
	for _, name := range p.CriticalSignals {
		if !slices.Contains(p.AcceptedSignals, name) {
			return apperror.NewValidation("critical AML signal must have an accepted mapping")
		}
	}
	return nil
}

type Assessment struct {
	Class     Class
	Reason    string
	ExpiresAt time.Time
}

// Assess fails closed for automatic routing. A separate explicit unknown
// export can be permitted without mislabelling the source as low risk.
func (p Policy) Assess(report Report, now time.Time) (Assessment, error) {
	if err := p.Validate(); err != nil {
		return Assessment{}, err
	}
	unknown := func(reason string) (Assessment, error) {
		return Assessment{Class: Unknown, Reason: reason}, nil
	}
	if report.Score == nil || !report.Score.Valid() {
		return unknown("missing_or_invalid_score")
	}
	if id.IsNil(report.ID) || report.Provider == "" || id.IsNil(report.ProviderAccountID) || !report.CoverageAccepted || len(report.Signals) == 0 || report.CheckHash == "" || report.RequestDigest == "" || report.ContractVersion == "" || report.AdapterVersion == "" || report.MappingVersion == "" {
		return unknown("insufficient_evidence")
	}
	if report.CompletedAt.IsZero() || report.CompletedAt.After(now) || !now.Before(report.CompletedAt.Add(p.MaxReportAge)) {
		return unknown("report_not_fresh")
	}
	class := Low
	if *report.Score > p.MediumMax {
		class = High
	} else if *report.Score > p.LowMax {
		class = Medium
	}
	unknownSignal := false
	critical := false
	for _, signal := range report.Signals {
		value, err := decimal.NewFromString(signal.Value)
		if err != nil || len(signal.Value) > 96 || value.Exponent() < -100 || value.Exponent() > 100 || value.IsNegative() || value.GreaterThan(decimal.NewFromInt(1)) {
			return unknown("invalid_signal")
		}
		if !slices.Contains(p.AcceptedSignals, signal.Name) {
			unknownSignal = true
		}
		// Values are validated by the adapter. Any nonzero exact decimal of a
		// critical category is restrictive, regardless of the aggregate score.
		if slices.Contains(p.CriticalSignals, signal.Name) && !value.IsZero() {
			critical = true
		}
	}
	if critical {
		class = High
	} else if unknownSignal && class != High {
		return unknown("unknown_signal")
	}
	expires := now.Add(p.DecisionTTL)
	if reportExpiry := report.CompletedAt.Add(p.MaxReportAge); reportExpiry.Before(expires) {
		expires = reportExpiry
	}
	reason := "score_band"
	if critical {
		reason = "critical_signal"
	}
	return Assessment{Class: class, Reason: reason, ExpiresAt: expires}, nil
}

// PoolClass never averages away risk. A known high source stays high even
// while another contribution is awaiting assessment.
func PoolClass(classes ...Class) Class {
	result := Low
	for _, class := range classes {
		if class == High {
			return High
		}
		if !class.Valid() || class == Unknown {
			result = Unknown
		} else if class == Medium && result != Unknown {
			result = Medium
		}
	}
	if len(classes) == 0 {
		return Unknown
	}
	return result
}
