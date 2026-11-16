package dto

import (
	"time"

	"metapus/internal/domain/integrations"
)

type IntegrationPoolCommandRequest struct {
	Action          string `json:"action" binding:"required"`
	Reason          string `json:"reason" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion"`
}
type IntegrationAccountStatus struct {
	InvalidSlots         []string                 `json:"invalidSlots"`
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	IsActive             bool                     `json:"isActive"`
	HasCredentials       bool                     `json:"hasCredentials"`
	QuotaPolicy          integrations.QuotaPolicy `json:"quotaPolicy"`
	CredentialsExpiresAt *time.Time               `json:"credentialsExpiresAt"`
	ManualPeriodVersion  int64                    `json:"manualPeriodVersion"`
	Availability         string                   `json:"availability"`
	CredentialsVersion   int                      `json:"credentialsVersion"`
	ExhaustedUntil       *time.Time               `json:"exhaustedUntil"`
	ThrottledUntil       *time.Time               `json:"throttledUntil"`
	LastError            string                   `json:"lastError"`
	NextResetAt          *time.Time               `json:"nextResetAt"`
}
type IntegrationPoolStatusResponse struct {
	PoolID           string                     `json:"poolId"`
	AccountID        *string                    `json:"accountId"`
	SelectionVersion int64                      `json:"selectionVersion"`
	Reason           string                     `json:"reason"`
	Accounts         []IntegrationAccountStatus `json:"accounts"`
}

type IntegrationConnectionTestResponse struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message"`
}

func FromIntegrationPoolStatus(s integrations.PoolStatus) IntegrationPoolStatusResponse {
	out := IntegrationPoolStatusResponse{PoolID: s.PoolID, AccountID: s.AccountID, SelectionVersion: s.SelectionVersion, Reason: s.Reason, Accounts: []IntegrationAccountStatus{}}
	for _, a := range s.Accounts {
		out.Accounts = append(out.Accounts, IntegrationAccountStatus{ID: a.ID, Name: a.Name, IsActive: a.IsActive, HasCredentials: a.HasCredentials, CredentialsVersion: a.CredentialsVersion, QuotaPolicy: a.QuotaPolicy, CredentialsExpiresAt: a.CredentialsExpiresAt, ManualPeriodVersion: a.ManualPeriodVersion, Availability: a.Availability, ExhaustedUntil: a.ExhaustedUntil, ThrottledUntil: a.ThrottledUntil, LastError: a.LastError, NextResetAt: a.NextResetAt, InvalidSlots: a.InvalidSlots})
	}
	return out
}
