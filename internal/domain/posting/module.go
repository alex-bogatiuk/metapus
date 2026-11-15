package posting

import "metapus/internal/core/entity"

// Module plugs one configuration's registers into a posting engine: it adds
// visitors, recorders and in-transaction validators, and returns the movement
// providers that expose those registers' document movements.
type Module interface {
	InstallPosting(engine *Engine) []entity.MovementProvider
}
