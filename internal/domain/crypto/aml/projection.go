package aml

import "time"

type Contribution struct {
	Class     Class
	ExpiresAt *time.Time
}

type Projection struct {
	Class     Class
	Covered   bool
	Reason    string
	ExpiresAt *time.Time
}

// ProjectInventory classifies the entire fungible pool conservatively. It does
// not attribute unverified ledger receipts to a convenient checked amount.
// A historical high fact is sticky until an explicit reviewed resolution.
func ProjectInventory(contributions []Contribution, covered, knownHigh bool, now time.Time) Projection {
	result := Projection{Class: Unknown, Covered: covered, Reason: "missing_inventory_coverage"}
	for _, item := range contributions {
		if item.Class == High {
			knownHigh = true
		}
	}
	if knownHigh {
		result.Class = High
		result.Reason = "known_high_exposure"
		return result
	}
	if !covered {
		return result
	}
	if len(contributions) == 0 {
		result.Reason = "no_assessed_contributions"
		return result
	}
	classes := make([]Class, 0, len(contributions))
	for _, item := range contributions {
		class := item.Class
		if class != High && (item.ExpiresAt == nil || !now.Before(*item.ExpiresAt)) {
			class = Unknown
		}
		classes = append(classes, class)
		if class != High && item.ExpiresAt != nil && now.Before(*item.ExpiresAt) && (result.ExpiresAt == nil || item.ExpiresAt.Before(*result.ExpiresAt)) {
			expires := *item.ExpiresAt
			result.ExpiresAt = &expires
		}
	}
	result.Class = PoolClass(classes...)
	result.Reason = "conservative_inventory"
	if result.Class == High {
		result.ExpiresAt = nil
		result.Reason = "known_high_exposure"
	}
	return result
}
