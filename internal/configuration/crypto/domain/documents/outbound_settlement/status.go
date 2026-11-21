package outbound_settlement

import (
	"database/sql/driver"
	"fmt"
)

// Status is the worker settlement FSM for already-broadcast outbound transactions.
type Status string

const (
	StatusUnknown       Status = "unknown"
	StatusRepairing     Status = "repairing"
	StatusRecorded      Status = "recorded"
	StatusNotApplicable Status = "not_applicable"
	StatusHeld          Status = "held"
)

func (s Status) String() string {
	return string(s)
}

func (s Status) IsValid() bool {
	switch s {
	case StatusUnknown, StatusRepairing, StatusRecorded, StatusNotApplicable, StatusHeld:
		return true
	default:
		return false
	}
}

func (s *Status) Scan(src any) error {
	value, err := scanString(src)
	if err != nil {
		return err
	}
	status := Status(value)
	if !status.IsValid() {
		return fmt.Errorf("invalid outbound settlement status %q", value)
	}
	*s = status
	return nil
}

func (s Status) Value() (driver.Value, error) {
	if !s.IsValid() {
		return nil, fmt.Errorf("invalid outbound settlement status %q", s)
	}
	return string(s), nil
}

// ChainResult stores the normalized chain receipt result. Values are chain-defined.
type ChainResult string

const ChainResultSuccess ChainResult = "SUCCESS"

func (r ChainResult) String() string {
	return string(r)
}

func (r *ChainResult) Scan(src any) error {
	value, err := scanString(src)
	if err != nil {
		return err
	}
	*r = ChainResult(value)
	return nil
}

func (r ChainResult) Value() (driver.Value, error) {
	return string(r), nil
}

func scanString(src any) (string, error) {
	switch value := src.(type) {
	case string:
		return value, nil
	case []byte:
		return string(value), nil
	case nil:
		return "", fmt.Errorf("cannot scan NULL into outbound settlement string")
	default:
		return "", fmt.Errorf("cannot scan %T into outbound settlement string", src)
	}
}
