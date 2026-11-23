package integrations

import "github.com/alex-bogatiuk/metapus/internal/core/entity"

var AccountDefinition = entity.Definition{
	Key:   "integration_account",
	Name:  "IntegrationAccount",
	Route: "integration-accounts",
	Table: "cat_integration_accounts",
	Kind:  entity.CatalogKind,
}

func (Account) EntityDefinition() entity.Definition { return AccountDefinition }

var PoolDefinition = entity.Definition{
	Key:   "integration_pool",
	Name:  "IntegrationPool",
	Route: "integration-pools",
	Table: "cat_integration_pools",
	Kind:  entity.CatalogKind,
}

func (Pool) EntityDefinition() entity.Definition { return PoolDefinition }
