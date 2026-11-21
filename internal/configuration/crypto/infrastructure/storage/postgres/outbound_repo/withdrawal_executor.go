package outbound_repo

import (
	"metapus/internal/configuration/crypto/application/outboundapp"
)

func holdExecution(err error) error { return outboundapp.HoldExecution(err) }
