package outbound_repo

import (
	"github.com/alex-bogatiuk/metapus/internal/configuration/crypto/application/outboundapp"
)

func holdExecution(err error) error { return outboundapp.HoldExecution(err) }
