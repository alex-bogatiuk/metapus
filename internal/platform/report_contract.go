// Package platform reexports neutral report contracts for extension compatibility.
package platform

import "github.com/alex-bogatiuk/metapus/internal/domain/reports/contract"

type ReportMeta = contract.ReportMeta
type FieldTreeNode = contract.FieldTreeNode
type EnumValue = contract.EnumValue
type ReportFilter = contract.ReportFilter
type ReportColumn = contract.ReportColumn
type ReportGroupBy = contract.ReportGroupBy
type ReportTotal = contract.ReportTotal
type MoneySources = contract.MoneySources
type ReportSort = contract.ReportSort
