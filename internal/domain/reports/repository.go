package reports

import (
	"context"
)

// Repository defines report data access interface.
type Repository interface {
	// Stock reports
	GetStockBalanceReport(ctx context.Context, filter StockBalanceReportFilter) (*StockBalanceReport, error)
	GetStockTurnoverReport(ctx context.Context, filter StockTurnoverReportFilter) (*StockTurnoverReport, error)

	// Document journal
	GetDocumentJournal(ctx context.Context, filter DocumentJournalFilter) (*DocumentJournal, error)
	GetDocumentTypeSummary(ctx context.Context, filter DocumentJournalFilter) ([]DocumentTypeSummary, error)
}
