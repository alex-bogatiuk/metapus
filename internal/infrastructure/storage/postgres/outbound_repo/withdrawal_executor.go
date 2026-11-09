package outbound_repo

import (
	"metapus/internal/application/outboundapp"
)

func holdExecution(err error) error { return outboundapp.HoldExecution(err) }
