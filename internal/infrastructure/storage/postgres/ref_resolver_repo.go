package postgres

import (
	"context"
	"fmt"
	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/domain"
	"metapus/internal/metadata"
	"net/http"
	"strings"
	"time"
)

// RefResolverRepo presents only authorized fields and independently checks
// related entities. Missing and forbidden references are both omitted.
type RefResolverRepo struct{ registry *metadata.Registry }

func NewRefResolverRepo(registry *metadata.Registry) *RefResolverRepo {
	return &RefResolverRepo{registry}
}
func (r *RefResolverRepo) ResolveRefs(ctx context.Context, refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
	return r.resolveRefs(ctx, refs, false)
}

// ResolveRefsWithDeletionMarked also resolves deletion-marked objects, which
// ordinary reads hide. Tools that work on marked objects use it.
func (r *RefResolverRepo) ResolveRefsWithDeletionMarked(ctx context.Context, refs []domain.RefResolveRequest) ([]domain.RefResolveResult, error) {
	return r.resolveRefs(ctx, refs, true)
}

func (r *RefResolverRepo) resolveRefs(ctx context.Context, refs []domain.RefResolveRequest, includeMarked bool) ([]domain.RefResolveResult, error) {
	results := make([]domain.RefResolveResult, 0, len(refs))
	if len(refs) == 0 {
		return results, nil
	}
	reads, ok := readaccess.FromContext(ctx)
	if !ok {
		return nil, apperror.NewForbidden("authorized reference reader is unavailable")
	}
	groups := make(map[string][]id.ID)
	for _, ref := range refs {
		groups[ref.RefType] = append(groups[ref.RefType], ref.RefID)
	}
	resolved := make(map[domain.RefResolveRequest]domain.RefResolveResult)
	for key, ids := range groups {
		def, ok := r.registry.Get(key)
		if !ok {
			continue
		}
		if _, err := reads.Require(ctx, def.Key); err != nil {
			if apperror.GetHTTPStatus(err) == http.StatusForbidden {
				continue
			}
			return nil, err
		}
		read := reads.ReadIDs
		if includeMarked {
			read = reads.ReadIDsWithDeletionMarked
		}
		rows, err := read(ctx, def.Key, ids)
		if err != nil {
			return nil, err
		}
		previews, err := AuthorizedPreviewData(ctx, r.registry, reads, def, rows)
		if err != nil {
			return nil, err
		}
		money, err := reads.ReadMoney(ctx, rows, readaccess.MoneyFields{Amount: "totalAmount", CurrencyID: "currencyId", CurrencyEntity: "currency"})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			result := domain.RefResolveResult{RefType: key, RefID: row.ID, EntityType: string(def.Type), Route: def.RoutePrefix, PreviewData: previews[row.ID]}
			if def.Key == "currency" {
				if unit, ok := readaccess.CurrencyUnitFromProjection(row); ok {
					result.CurrencyCode = unit.Code
					result.DecimalPlaces = &unit.DecimalPlaces
				}
			}
			result.Number, _ = row.Fields["number"].(string)
			result.Date, _ = row.Fields["date"].(time.Time)
			result.Posted, _ = row.Fields["posted"].(bool)
			result.DeletionMark, _ = row.Fields["deletionMark"].(bool)
			if value, ok := money[row.ID]; ok {
				result.Amount = fmt.Sprint(value.Amount.MinorUnitsValue())
				result.CurrencyID = &value.Unit.ID
				result.CurrencyCode = value.Unit.Code
				result.DecimalPlaces = &value.Unit.DecimalPlaces
			}
			if def.Type == metadata.TypeDocument {
				result.Presentation = def.Presentation.Singular
				if result.Presentation == "" {
					result.Presentation = def.Name
				}
				if result.Number != "" {
					result.Presentation += " " + result.Number
				}
				if !result.Date.IsZero() {
					result.Presentation += " от " + result.Date.Format("02.01.2006")
				}
			} else {
				result.Presentation, _ = row.Fields["name"].(string)
				if code, ok := row.Fields["code"].(string); ok && code != "" {
					result.Presentation = strings.TrimSpace(result.Presentation + " (" + code + ")")
				}
				if result.Presentation == "" {
					result.Presentation = def.Presentation.Singular
				}
			}
			resolved[domain.RefResolveRequest{RefType: key, RefID: row.ID}] = result
		}
	}
	for _, ref := range refs {
		if result, ok := resolved[ref]; ok {
			results = append(results, result)
			delete(resolved, ref)
		}
	}
	return results, nil
}

// deriveTableName determines the SQL table name from EntityDef.
// Convention: catalogs → "cat_{key_plural}", documents → "doc_{key_plural}".
func deriveTableName(def metadata.EntityDef) string {
	if def.TableName != "" {
		return def.TableName
	}

	key := def.Key
	if key == "" {
		return ""
	}

	switch def.Type {
	case metadata.TypeCatalog:
		return "cat_" + pluralizeKey(key)
	case metadata.TypeDocument:
		return "doc_" + pluralizeKey(key)
	default:
		return ""
	}
}

// pluralizeKey applies simple English pluralization for known Metapus entity keys.
func pluralizeKey(key string) string {
	irregulars := map[string]string{
		"counterparty": "counterparties",
		"currency":     "currencies",
		"organization": "organizations",
		"contract":     "contracts",
	}

	if plural, ok := irregulars[key]; ok {
		return plural
	}

	if strings.HasSuffix(key, "s") || strings.HasSuffix(key, "x") || strings.HasSuffix(key, "z") {
		return key + "es"
	}
	if strings.HasSuffix(key, "y") && len(key) > 1 {
		c := key[len(key)-2]
		if c != 'a' && c != 'e' && c != 'i' && c != 'o' && c != 'u' {
			return key[:len(key)-1] + "ies"
		}
	}
	return key + "s"
}
