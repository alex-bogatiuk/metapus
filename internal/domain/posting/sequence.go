package posting

import (
	"fmt"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
)

// CodeSequenceNotRestored: a closing of a period is posted while documents
// of its sequence before the period were changed after their own closing
// (entity.SequenceRule).
const CodeSequenceNotRestored = "SEQUENCE_NOT_RESTORED"

// NewSequenceNotRestored refuses a closing whose sequence is restored only up
// to boundary, before the period it closes.
func NewSequenceNotRestored(boundary time.Time) *apperror.AppError {
	return apperror.NewBusinessRule(CodeSequenceNotRestored, fmt.Sprintf(
		"Документы изменены задним числом с %s: сначала перепроведите закрытие периода, в который попадает эта дата",
		boundary.UTC().Format("02.01.2006"))).WithDetail("boundary", boundary)
}
