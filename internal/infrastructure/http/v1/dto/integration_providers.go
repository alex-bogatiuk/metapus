package dto

import "github.com/alex-bogatiuk/metapus/internal/domain/integrations"

type IntegrationKindResponse struct {
	ID          integrations.Kind `json:"id"`
	Label       string            `json:"label"`
	Description string            `json:"description"`
}

type IntegrationCredentialFieldResponse struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type IntegrationCredentialProfileResponse struct {
	Fields        []IntegrationCredentialFieldResponse     `json:"fields"`
	ReplaceLabel  string                                   `json:"replaceLabel"`
	Description   string                                   `json:"description"`
	InputFormat   string                                   `json:"inputFormat"`
	KeyFormat     integrations.CredentialKeyFormat         `json:"keyFormat,omitempty"`
	RequireFields bool                                     `json:"requireFields,omitempty"`
	SingleKey     bool                                     `json:"singleKey,omitempty"`
	URLImports    []IntegrationCredentialURLImportResponse `json:"urlImports,omitempty"`
}

type IntegrationCredentialURLImportResponse struct {
	Host       string            `json:"host"`
	PathPrefix string            `json:"pathPrefix"`
	RolePaths  map[string]string `json:"rolePaths,omitempty"`
}

type IntegrationProviderResponse struct {
	ID          integrations.Provider                `json:"id"`
	Label       string                               `json:"label"`
	Description string                               `json:"description"`
	Kinds       []integrations.Kind                  `json:"kinds"`
	CanCreate   bool                                 `json:"canCreate"`
	Credentials IntegrationCredentialProfileResponse `json:"credentials"`
	QuotaPolicy integrations.QuotaPolicy             `json:"quotaPolicy"`
}

type IntegrationProvidersResponse struct {
	Kinds     []IntegrationKindResponse     `json:"kinds"`
	Providers []IntegrationProviderResponse `json:"providers"`
}

// IntegrationProviders describes the application's catalog for the settings UI.
func IntegrationProviders(catalog *integrations.Catalog) IntegrationProvidersResponse {
	out := IntegrationProvidersResponse{Kinds: []IntegrationKindResponse{}, Providers: []IntegrationProviderResponse{}}
	for _, kind := range catalog.Kinds() {
		out.Kinds = append(out.Kinds, IntegrationKindResponse{ID: kind.ID, Label: kind.Label, Description: kind.Description})
	}
	for _, provider := range catalog.Providers() {
		profile := IntegrationCredentialProfileResponse{Fields: []IntegrationCredentialFieldResponse{}, ReplaceLabel: provider.Credentials.ReplaceLabel, Description: provider.Credentials.Description, InputFormat: provider.Credentials.InputFormat}
		profile.KeyFormat, profile.RequireFields, profile.SingleKey = provider.Credentials.KeyFormat, provider.Credentials.RequireFields, provider.Credentials.SingleKey
		for _, rule := range provider.Credentials.URLImports {
			profile.URLImports = append(profile.URLImports, IntegrationCredentialURLImportResponse{Host: rule.Host, PathPrefix: rule.PathPrefix, RolePaths: rule.RolePaths})
		}
		for _, field := range provider.Credentials.Fields {
			profile.Fields = append(profile.Fields, IntegrationCredentialFieldResponse{Name: field.Name, Label: field.Label, Description: field.Description})
		}
		out.Providers = append(out.Providers, IntegrationProviderResponse{ID: provider.ID, Label: provider.Label, Description: provider.Description, Kinds: provider.Kinds, CanCreate: provider.CanCreate, Credentials: profile, QuotaPolicy: provider.QuotaPolicy})
	}
	return out
}
