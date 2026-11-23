package outbound_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
)

// ResourceBroadcastCommand binds a resource reservation to the exact signed
// chain effect. AllocationID is optional so a retry after a process crash can
// recover the reservation by its immutable business basis.
type ResourceBroadcastCommand = outboundapp.ResourceBroadcastCommand
