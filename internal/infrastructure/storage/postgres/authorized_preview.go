package postgres

import (
	"context"
	"fmt"
	"net/http"

	"metapus/internal/application/readaccess"
	"metapus/internal/core/apperror"
	"metapus/internal/core/id"
	"metapus/internal/metadata"
)

// AuthorizedPreviewData batches related rows by their own entity policy. Only
// references present in the source's field projection may be expanded.
func AuthorizedPreviewData(ctx context.Context, registry *metadata.Registry, reads *readaccess.Service, def metadata.EntityDef, rows []readaccess.Projection) (map[id.ID]map[string]string, error) {
	type link struct {
		source     id.ID
		label, key string
		target     id.ID
	}
	links := make([]link, 0)
	groups := make(map[string][]id.ID)
	result := make(map[id.ID]map[string]string, len(rows))
	for _, row := range rows {
		result[row.ID] = make(map[string]string)
		for _, preview := range def.PreviewFields {
			value := row.Fields[preview.Name]
			if value == nil {
				continue
			}
			if preview.ReferenceType == "" {
				if text := fmt.Sprint(value); text != "" {
					result[row.ID][preview.Label] = text
				}
				continue
			}
			name, ok := registry.GetEntityByRefType(preview.ReferenceType)
			if !ok {
				name = preview.ReferenceType
			}
			target, ok := registry.Get(name)
			if !ok {
				continue
			}
			uid, err := id.Parse(fmt.Sprint(value))
			if err != nil || id.IsNil(uid) {
				continue
			}
			links = append(links, link{row.ID, preview.Label, target.Key, uid})
			groups[target.Key] = append(groups[target.Key], uid)
		}
	}
	names := make(map[string]map[id.ID]string)
	for key, ids := range groups {
		if _, err := reads.Require(ctx, key); err != nil {
			if apperror.GetHTTPStatus(err) == http.StatusForbidden {
				continue
			}
			return nil, err
		}
		related, err := reads.ReadIDs(ctx, key, ids)
		if err != nil {
			return nil, err
		}
		names[key] = make(map[id.ID]string, len(related))
		for _, row := range related {
			if name, ok := row.Fields["name"].(string); ok {
				names[key][row.ID] = name
			}
		}
	}
	for _, link := range links {
		if name := names[link.key][link.target]; name != "" {
			result[link.source][link.label] = name
		}
	}
	return result, nil
}
