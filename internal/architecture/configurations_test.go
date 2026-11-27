package architecture_test

import "strings"

// module is the repository's module path with a trailing slash.
const module = "github.com/alex-bogatiuk/metapus/"

// repositoryConfiguration is a configuration of the repository: its name, its
// directory from the repository root and, for an add-on (надстройка), the
// base configuration it is built on. A base lives in configurations/ when an
// add-on in another repository may import it (internal/ is closed to other
// modules); crypto, which no add-on builds on, stays in internal/.
type repositoryConfiguration struct {
	name string
	dir  string
	base string
}

// repositoryConfigurations lists every configuration of the repository; the
// architecture tests take the configurations' roots from it.
var repositoryConfigurations = []repositoryConfiguration{
	{name: "crypto", dir: "internal/configuration/crypto"},
	{name: "trade", dir: "configurations/trade"},
	{name: "automotive", dir: "configurations/automotive", base: "trade"},
}

// pkg is the configuration's root import path.
func (c repositoryConfiguration) pkg() string { return module + c.dir }

// owns reports whether importPath is the configuration's root or below it.
func (c repositoryConfiguration) owns(importPath string) bool {
	return importPath == c.pkg() || strings.HasPrefix(importPath, c.pkg()+"/")
}

// configurationNamed returns the repository's configuration of that name.
func configurationNamed(name string) repositoryConfiguration {
	for _, c := range repositoryConfigurations {
		if c.name == name {
			return c
		}
	}
	panic("no repository configuration " + name)
}

// configurationOwning returns the configuration importPath belongs to.
func configurationOwning(importPath string) (repositoryConfiguration, bool) {
	for _, c := range repositoryConfigurations {
		if c.owns(importPath) {
			return c, true
		}
	}
	return repositoryConfiguration{}, false
}
