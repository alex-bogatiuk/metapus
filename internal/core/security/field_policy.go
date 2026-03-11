package security

import (
	"fmt"
	"strings"
)

// FieldPolicy describes which fields a role is allowed to read or write
// for a specific entity and action.
//
// AllowedFields uses a simple DSL:
//   - ["*"] — all fields allowed
//   - ["-status", "-organization_id"] — all except these fields
//   - ["quantity", "unit_price"] — only these specific fields
//
// TableParts optionally restricts which columns within a table part (tabular section)
// can be modified. Key = table part name (e.g. "items"), value = allowed column names.
type FieldPolicy struct {
	EntityName    string
	Action        string            // "read" or "write"
	AllowedFields []string          // field-level whitelist/blacklist DSL
	TableParts    map[string][]string // table part name → allowed columns
}

// IsFieldAllowed checks if a specific field name is allowed by this policy.
//
// Rules:
//   - If AllowedFields is empty or nil → deny all (fail-closed)
//   - If AllowedFields contains "*" → allow all, then check exclusions ("-fieldName")
//   - If AllowedFields contains specific names → only those are allowed
func (p *FieldPolicy) IsFieldAllowed(field string) bool {
	if p == nil || len(p.AllowedFields) == 0 {
		return false // fail-closed
	}

	hasWildcard := false
	exclusions := make(map[string]struct{})
	inclusions := make(map[string]struct{})

	for _, f := range p.AllowedFields {
		if f == "*" {
			hasWildcard = true
		} else if strings.HasPrefix(f, "-") {
			exclusions[f[1:]] = struct{}{}
		} else {
			inclusions[f] = struct{}{}
		}
	}

	if hasWildcard {
		// Wildcard: allow everything except exclusions
		_, excluded := exclusions[field]
		return !excluded
	}

	// Explicit inclusion list
	_, included := inclusions[field]
	return included
}

// IsTablePartFieldAllowed checks if a column in a table part is allowed.
// If the table part is not mentioned in policy, all columns are denied (fail-closed).
func (p *FieldPolicy) IsTablePartFieldAllowed(partName, column string) bool {
	if p == nil || p.TableParts == nil {
		return false
	}

	allowedCols, ok := p.TableParts[partName]
	if !ok {
		return false
	}

	for _, col := range allowedCols {
		if col == "*" {
			return true
		}
		if col == column {
			return true
		}
	}
	return false
}

// ValidateFieldChanges compares two maps of field values (old vs new) and returns
// an error if any changed field is not allowed by the policy.
//
// This implements "Approach B": we only block fields that actually changed,
// allowing the client to send the full DTO without errors as long as
// restricted fields remain unchanged.
func (p *FieldPolicy) ValidateFieldChanges(oldFields, newFields map[string]any) error {
	for field, newVal := range newFields {
		oldVal, hadOld := oldFields[field]

		// If field didn't change, skip
		if hadOld && fmt.Sprintf("%v", oldVal) == fmt.Sprintf("%v", newVal) {
			continue
		}

		// Field changed — check if policy allows it
		if !p.IsFieldAllowed(field) {
			return fmt.Errorf("field '%s' is read-only", field)
		}
	}
	return nil
}
