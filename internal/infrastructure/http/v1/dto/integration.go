package dto

import (
	"github.com/alex-bogatiuk/metapus/internal/core/id"
	"github.com/alex-bogatiuk/metapus/internal/domain/integrations"
	"time"
)

type IntegrationAccountFields struct {
	Code                 string                    `json:"code"`
	Name                 string                    `json:"name" binding:"required"`
	Provider             integrations.Provider     `json:"provider" binding:"required"`
	ExternalAccountRef   string                    `json:"externalAccountRef"`
	IsActive             bool                      `json:"isActive"`
	ResetDay             int                       `json:"resetDay"`
	ResetTimezone        string                    `json:"resetTimezone"`
	CredentialsExpiresAt *time.Time                `json:"credentialsExpiresAt"`
	Credentials          *integrations.Credentials `json:"credentials,omitempty"`
}
type CreateIntegrationAccountRequest struct{ IntegrationAccountFields }
type UpdateIntegrationAccountRequest struct {
	IntegrationAccountFields
	Version int `json:"version" binding:"required"`
}

func (f IntegrationAccountFields) Apply(a *integrations.Account) {
	a.Code = f.Code
	a.Name = f.Name
	a.Provider = f.Provider
	a.ExternalAccountRef = f.ExternalAccountRef
	a.IsActive = f.IsActive
	a.ResetDay = f.ResetDay
	a.ResetTimezone = f.ResetTimezone
	a.PendingCredentials = f.Credentials
	a.CredentialsExpiresAt = f.CredentialsExpiresAt
}
func (f *CreateIntegrationAccountRequest) ToEntity() *integrations.Account {
	a := integrations.NewAccount(f.Code, f.Name)
	f.Apply(a)
	return a
}
func (f *UpdateIntegrationAccountRequest) ApplyTo(a *integrations.Account) {
	f.Apply(a)
	a.Version = f.Version
}

type IntegrationAccountResponse struct {
	integrations.Account
	HasCredentials bool `json:"hasCredentials"`
}

func FromIntegrationAccount(a *integrations.Account) *IntegrationAccountResponse {
	return &IntegrationAccountResponse{Account: *a, HasCredentials: len(a.CredentialsEnc) > 0}
}

type IntegrationPoolFields struct {
	Code       string                `json:"code"`
	Name       string                `json:"name" binding:"required"`
	Provider   integrations.Provider `json:"provider" binding:"required"`
	IsActive   bool                  `json:"isActive"`
	AccountIDs []id.ID               `json:"accountIds"`
}
type CreateIntegrationPoolRequest struct{ IntegrationPoolFields }
type UpdateIntegrationPoolRequest struct {
	IntegrationPoolFields
	Version int `json:"version" binding:"required"`
}

func (f IntegrationPoolFields) Apply(p *integrations.Pool) {
	p.Code = f.Code
	p.Name = f.Name
	p.Provider = f.Provider
	p.IsActive = f.IsActive
	p.AccountIDs = append([]id.ID{}, f.AccountIDs...)
}
func (f *CreateIntegrationPoolRequest) ToEntity() *integrations.Pool {
	p := integrations.NewPool(f.Code, f.Name)
	f.Apply(p)
	return p
}
func (f *UpdateIntegrationPoolRequest) ApplyTo(p *integrations.Pool) {
	f.Apply(p)
	p.Version = f.Version
}

type IntegrationPoolResponse struct{ integrations.Pool }

func FromIntegrationPool(p *integrations.Pool) *IntegrationPoolResponse {
	return &IntegrationPoolResponse{Pool: *p}
}
