package portal_repo

import (
	"context"
	"errors"
	"fmt"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/readpolicy"
	"slices"
	"strings"

	"github.com/Masterminds/squirrel"
	"github.com/jackc/pgx/v5"

	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/portalapp"
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/domain/documents/withdrawal_request"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/apperror"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/cursor"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/domain"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/filter"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/id"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres"
	pgfilter "github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/filter"
	"github.com/alex-bogatiuk/metapus/pkg/sdk/postgres/keyset"
)

const adminWithdrawalColumns = `
	wr.id::text, wr.number, wr.date, wr.status,
	wr.merchant_id::text, COALESCE(m.name, ''),
	wr.token_id::text, t.symbol, COALESCE(n.name, ''),
	wr.amount, t.decimal_places, wr.dest_address,
	wr.approved_by::text,
	wr.approved_at, wr.rejection_reason, wr.withdrawal_id::text, wr.created_at,
	0::int AS total_count,` + amlWithdrawalHistoryColumns

var adminWithdrawalSort = map[string]string{
	"created_at":   "timestamptz",
	"date":         "timestamptz",
	"number":       "text",
	"status":       "text",
	"amount":       "numeric",
	"merchant_id":  "uuid",
	"token_id":     "uuid",
	"dest_address": "text",
	"approved_at":  "timestamptz",
}

func adminWithdrawalSortValue(item portalapp.AdminWithdrawalRequestItem, field string) any {
	switch field {
	case "date":
		return item.Date
	case "number":
		return item.Number
	case "status":
		return item.Status
	case "amount":
		return item.Amount
	case "merchant_id":
		return item.MerchantID
	case "token_id":
		return item.TokenID
	case "dest_address":
		return item.DestAddress
	case "approved_at":
		return item.ApprovedAt
	default:
		return item.CreatedAt
	}
}

// ListAdminWithdrawalRequestsPage implements the standard list contract over
// the enriched approval read model. Filter and sort expressions are whitelisted.
func (r *DashboardRepo) ListAdminWithdrawalRequestsPage(ctx context.Context, f domain.ListFilter, status string) (portalapp.AdminWithdrawalRequestListResponse, error) {
	result := portalapp.AdminWithdrawalRequestListResponse{Items: make([]portalapp.AdminWithdrawalRequestItem, 0)}
	orderBy := strings.TrimPrefix(f.OrderBy, "-")
	orderBy = strings.TrimPrefix(orderBy, "+")
	if orderBy == "" {
		orderBy = "created_at"
	}
	fieldType, ok := adminWithdrawalSort[orderBy]
	if !ok {
		return result, apperror.NewValidation("invalid withdrawal request orderBy")
	}
	spec, err := keyset.ParseOrderBy(f.OrderBy, "created_at", "DESC", map[string]struct{}{orderBy: {}})
	if err != nil {
		return result, err
	}
	spec.Field = "wr." + spec.Field
	spec.IDField = "wr.id"
	spec.FieldType = fieldType
	spec.Nullable = orderBy == "approved_at"

	conditions := []squirrel.Sqlizer{squirrel.Expr("wr._deleted_at IS NULL")}
	switch status {
	case "all":
	case "active":
		conditions = append(conditions, squirrel.NotEq{"wr.status": []string{"confirmed", "rejected", "failed", "partially_completed"}})
	default:
		conditions = append(conditions, squirrel.Eq{"wr.status": status})
	}
	if f.DataScope != nil {
		conditions = append(conditions, readpolicy.ApplyConditions(f.DataScope, "withdrawal_request", map[string]string{"merchant": "wr.merchant_id"})...)
	}
	search, advanced := filter.ExtractSearchQuery(f.AdvancedFilters)
	if strings.TrimSpace(f.Search) != "" {
		search = f.Search
	}
	if searchCondition := pgfilter.BuildSearchConditions(search, []string{"wr.number", "wr.dest_address"}); searchCondition != nil {
		conditions = append(conditions, searchCondition)
	}
	trusted := postgres.ExtractFilterFields[withdrawal_request.WithdrawalRequest](
		"number", "date", "status", "merchant_id", "token_id", "amount", "dest_address", "created_at", "approved_by",
	)
	for _, item := range advanced {
		field, ok := trusted[item.Field]
		if !ok {
			return result, apperror.NewValidation("invalid withdrawal request filter").WithDetail("field", item.Field)
		}
		resolved, err := filter.Resolve(item, field)
		if err != nil {
			return result, apperror.NewValidation("invalid withdrawal request filter").WithCause(err)
		}
		condition, err := pgfilter.BuildSingleCondition(resolved, "wr."+item.Field)
		if err != nil {
			return result, apperror.NewValidation("invalid withdrawal request filter").WithCause(err)
		}
		conditions = append(conditions, condition)
	}

	builder := squirrel.StatementBuilder.PlaceholderFormat(squirrel.Dollar)
	q := querier(ctx)
	if !f.SkipCount && f.CursorReq == nil {
		count := builder.Select("COUNT(*)").From("doc_withdrawal_requests wr")
		for _, condition := range conditions {
			count = count.Where(condition)
		}
		countSQL, countArgs, err := count.ToSql()
		if err != nil {
			return result, err
		}
		var total int64
		if err := q.QueryRow(ctx, countSQL, countArgs...).Scan(&total); err != nil {
			return result, fmt.Errorf("count admin withdrawal requests: %w", err)
		}
		result.TotalCount = &total
		result.Total = int(total)
	}

	backward := false
	if f.CursorReq != nil {
		var boundary []any
		switch f.CursorReq.Direction {
		case cursor.DirAfter, cursor.DirBefore:
			boundary, err = keyset.DecodeCursor(f.CursorReq.Token, spec)
			backward = f.CursorReq.Direction == cursor.DirBefore
		case cursor.DirAround:
			target, parseErr := id.Parse(f.CursorReq.TargetID)
			if parseErr != nil {
				return result, apperror.NewValidation("invalid target ID")
			}
			var value *string
			targetQuery := builder.Select(spec.Field + "::text").From("doc_withdrawal_requests wr").Where(squirrel.Eq{"wr.id": target})
			for _, condition := range conditions {
				targetQuery = targetQuery.Where(condition)
			}
			var targetSQL string
			var targetArgs []any
			targetSQL, targetArgs, err = targetQuery.ToSql()
			if err != nil {
				return result, err
			}
			err = q.QueryRow(ctx, targetSQL, targetArgs...).Scan(&value)
			if errors.Is(err, pgx.ErrNoRows) {
				return result, apperror.NewNotFound("withdrawal request", target.String())
			}
			boundary = []any{value, target.String()}
			index := 0
			result.TargetIndex = &index
		default:
			return result, apperror.NewValidation("invalid cursor direction")
		}
		if err != nil {
			return result, err
		}
		conditions = append(conditions, keyset.TupleConditionInclusive(spec, boundary, true))
		if f.CursorReq.Direction != cursor.DirAround {
			conditions[len(conditions)-1] = keyset.TupleCondition(spec, boundary, !backward)
		}
	}

	page := builder.Select(adminWithdrawalColumns).From("doc_withdrawal_requests wr").
		Join("cat_merchants m ON m.id = wr.merchant_id").
		Join("cat_tokens t ON t.id = wr.token_id").
		LeftJoin("cat_blockchain_networks n ON n.id = t.network_id")
	for _, condition := range conditions {
		page = page.Where(condition)
	}
	if backward {
		page = page.OrderBy(spec.InvertedOrderByClause())
	} else {
		page = page.OrderBy(spec.OrderByClause())
	}
	limit := max(1, min(f.Limit, 500))
	page = page.Limit(uint64(limit + 1))
	querySQL, queryArgs, err := page.ToSql()
	if err != nil {
		return result, err
	}
	rows, err := q.Query(ctx, querySQL, queryArgs...)
	if err != nil {
		return result, fmt.Errorf("list admin withdrawal requests: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, _, err := scanAdminWithdrawalRequestListItem(rows)
		if err != nil {
			return result, fmt.Errorf("scan admin withdrawal request: %w", err)
		}
		result.Items = append(result.Items, item)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	rows.Close()
	more := len(result.Items) > limit
	if more {
		result.Items = result.Items[:limit]
	}
	if err := nameAdminWithdrawalApprovers(ctx, result.Items); err != nil {
		return result, err
	}
	if backward {
		slices.Reverse(result.Items)
		result.HasPrev = more
		result.HasMore = true
	} else {
		result.HasMore = more
		result.HasPrev = f.CursorReq != nil && f.CursorReq.Direction != cursor.DirBefore
	}
	if len(result.Items) > 0 {
		first, last := result.Items[0], result.Items[len(result.Items)-1]
		if result.HasPrev {
			result.PrevCursor, err = keyset.BuildCursorFromRow(spec, adminWithdrawalSortValue(first, orderBy), first.ID)
			if err != nil {
				return result, err
			}
		}
		if result.HasMore {
			result.NextCursor, err = keyset.BuildCursorFromRow(spec, adminWithdrawalSortValue(last, orderBy), last.ID)
			if err != nil {
				return result, err
			}
		}
	}
	return result, nil
}
