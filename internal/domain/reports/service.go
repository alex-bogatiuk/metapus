package reports

import (
	"context"
	"fmt"
	"time"
)

// Service provides report generation operations.
type Service struct {
	repo Repository
}

// NewService creates a new reports service.
func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

// GetStockBalance generates stock balance report.
func (s *Service) GetStockBalance(ctx context.Context, filter StockBalanceReportFilter) (*StockBalanceReport, error) {
	// Default to current time if not specified
	if filter.AsOfDate == nil {
		now := time.Now()
		filter.AsOfDate = &now
	}

	// Set default pagination
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	report, err := s.repo.GetStockBalanceReport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("get stock balance report: %w", err)
	}

	return report, nil
}

// GetStockTurnover generates stock turnover (оборотная ведомость) report.
func (s *Service) GetStockTurnover(ctx context.Context, filter StockTurnoverReportFilter) (*StockTurnoverReport, error) {
	// Validate required dates
	if filter.FromDate.IsZero() || filter.ToDate.IsZero() {
		return nil, fmt.Errorf("fromDate and toDate are required")
	}

	if filter.FromDate.After(filter.ToDate) {
		return nil, fmt.Errorf("fromDate must be before toDate")
	}

	// Set default pagination
	if filter.Limit <= 0 {
		filter.Limit = 100
	}
	if filter.Limit > 1000 {
		filter.Limit = 1000
	}

	report, err := s.repo.GetStockTurnoverReport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("get stock turnover report: %w", err)
	}

	return report, nil
}

// GetDocumentJournal returns document journal.
func (s *Service) GetDocumentJournal(ctx context.Context, filter DocumentJournalFilter) (*DocumentJournal, error) {
	// Set default pagination
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	if filter.Limit > 500 {
		filter.Limit = 500
	}

	// Default sort
	if filter.SortBy == "" {
		filter.SortBy = "date"
	}
	if filter.SortOrder == "" {
		filter.SortOrder = "desc"
	}

	journal, err := s.repo.GetDocumentJournal(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("get document journal: %w", err)
	}

	// Get summary if requested (when no pagination offset)
	if filter.Offset == 0 {
		summary, err := s.repo.GetDocumentTypeSummary(ctx, filter)
		if err == nil {
			journal.Summary = summary
		}
	}

	return journal, nil
}
