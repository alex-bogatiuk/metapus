package auth

import (
	"fmt"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

// Expected lookup misses have no internal cause to log. Storage failures retain
// their cause and reach the HTTP error handler as server errors.
func lookupError(action string, err error, missing *apperror.AppError) error {
	if apperror.IsNotFound(err) {
		return missing
	}
	return apperror.NewInternal(fmt.Errorf("%s: %w", action, err))
}
