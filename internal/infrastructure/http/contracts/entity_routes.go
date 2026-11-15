package contracts

import "metapus/internal/infrastructure/http/contracts/spec"

func bundleContracts(bundles []spec.Bundle) []Contract {
	var out []Contract
	for _, bundle := range bundles {
		out = append(out, bundle.Contracts...)
	}
	return out
}

func bundleRoutes(bundles []spec.Bundle) []RouteContract {
	var out []RouteContract
	for _, bundle := range bundles {
		out = append(out, bundle.Routes...)
	}
	return out
}
