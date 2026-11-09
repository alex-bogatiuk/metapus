package schema

// Source declares every pre-aggregation input and its entity policy. Aliases
// are fixed by the dataset registration, never accepted from HTTP parameters.
type Source struct {
	Key, Entity, Alias string
	Relations          []SourceRelation
}

type SourceRelation struct {
	Entity, Alias, Column string
	// Required excludes the source row if the reference is inaccessible. This
	// is used for scope dimensions before SUM, not to mask aggregate outputs.
	Required bool
}

type FieldAccess struct {
	Entity string
	Fields []string
}
