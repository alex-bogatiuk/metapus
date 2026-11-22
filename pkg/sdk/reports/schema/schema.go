// Package schema declares report datasets.
package schema

import (
	core "metapus/internal/domain/reports/schema"
)

type (
	// Dataset describes a single data source for the Query Engine.
	//
	// Simple datasets (e.g. a flat table) set BaseTable and leave Executor nil.
	// Complex datasets (CTE, UNION ALL, parametric subqueries) implement Executor
	// and the Query Compiler calls Executor.BuildQuery() instead of selecting from BaseTable.
	Dataset = core.Dataset
	// Field describes a single field in a Dataset.
	Field       = core.Field
	FieldAccess = core.FieldAccess
	// FilterDef describes a dataset-level filter parameter (not a column).
	// These are rendered as filter controls in the frontend.
	FilterDef = core.FilterDef
	// SortDef defines a default sort order.
	SortDef = core.SortDef
	// Source declares every pre-aggregation input and its entity policy. Aliases
	// are fixed by the dataset registration, never accepted from HTTP parameters.
	Source         = core.Source
	SourceRelation = core.SourceRelation
)

const (
	AggSum = core.AggSum
	// FieldAttribute is an informational field that is not aggregated (e.g. description).
	FieldAttribute = core.FieldAttribute
	// FieldDimension is a grouping/filterable field (e.g. wallet, token).
	FieldDimension = core.FieldDimension
	// FieldMeasure is an aggregatable numeric field (e.g. quantity, amount).
	FieldMeasure  = core.FieldMeasure
	FilterBoolean = core.FilterBoolean
	FilterDate    = core.FilterDate
	TypeBoolean   = core.TypeBoolean
	TypeDate      = core.TypeDate
	TypeInteger   = core.TypeInteger
	TypeMoney     = core.TypeMoney
	TypeString    = core.TypeString
)
