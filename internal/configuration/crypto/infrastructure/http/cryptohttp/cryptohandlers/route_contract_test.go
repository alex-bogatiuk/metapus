package cryptohandlers

import "github.com/alex-bogatiuk/metapus/pkg/sdk/http/v1"

// Crypto payments expose the platform repost route.
var _ v1.DocumentRepostHandler = (*CryptoPaymentHandler)(nil)
