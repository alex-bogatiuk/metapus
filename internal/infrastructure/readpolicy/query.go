package readpolicy

import (
	"context"
	"fmt"
	"github.com/Masterminds/squirrel"
)

type DatasetExecutor interface {
	BuildQuery(context.Context, map[string]any, ReadConstraints) (squirrel.SelectBuilder, error)
}

// ReadConstraints contains complete application-authorized source builders.
// Each builder already contains FROM, source policies, and secured joins. An
// executor can only add its business filters and aggregates to these builders.
type ReadConstraints struct {
	Sources map[string]squirrel.SelectBuilder
}

func (r ReadConstraints) Source(key string) (squirrel.SelectBuilder, error) {
	q, ok := r.Sources[key]
	if !ok {
		return squirrel.SelectBuilder{}, fmt.Errorf("report source %q is not authorized", key)
	}
	return q, nil
}
