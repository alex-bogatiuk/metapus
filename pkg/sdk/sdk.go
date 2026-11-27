// Package sdk is the public API of the Metapus platform for configurations
// and client extensions. Its subpackages are the whole surface a
// configuration builds on:
//
//   - entity, id, types, apperror — the model: catalogs, documents, register
//     movements, identifiers, fixed-point quantities and money, errors;
//   - registry — declaring catalogs and documents (v1.Catalog, v1.Document);
//   - posting, registers — accumulation registers and their posting;
//   - configuration, integrations, contracts — the configuration contract;
//   - sdktest — the checks every configuration runs in its tests.
//
// The platform's infrastructure (HTTP handlers, repositories, the worker)
// stays in internal/. An architecture test keeps the reference configuration
// (configurations/trade, configurations/autoparts) on these packages alone.
//
// Versioning: APIVersion follows semver. A major bump changes or removes
// something here; a minor bump only adds. A configuration names the version
// it is built for (configuration.Configuration.APIVersion) and the platform
// refuses to start one it cannot serve (Compatible). docs/sdk/CHANGELOG.md
// records every change.
package sdk

import (
	"fmt"
	"strconv"
	"strings"
)

// APIVersion is the semantic version of this API.
const APIVersion = "3.8.0"

// Compatible reports whether the platform's API serves a configuration built
// for version required: the same major version and at least its minor one.
func Compatible(required string) error {
	have, err := parse(APIVersion)
	if err != nil {
		return err
	}
	want, err := parse(required)
	if err != nil {
		return fmt.Errorf("configuration API version: %w", err)
	}
	if have[0] != want[0] || have[1] < want[1] {
		return fmt.Errorf("the platform serves API %s, the configuration needs %s", APIVersion, required)
	}
	return nil
}

func parse(version string) ([3]int, error) {
	var result [3]int
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) != 3 {
		return result, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
	}
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return result, fmt.Errorf("%q is not MAJOR.MINOR.PATCH", version)
		}
		result[i] = n
	}
	return result, nil
}
