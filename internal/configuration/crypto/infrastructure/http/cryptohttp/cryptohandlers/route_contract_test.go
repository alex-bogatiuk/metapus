package cryptohandlers

import v1 "metapus/internal/infrastructure/http/v1"

// Crypto payments expose the platform repost route.
var _ v1.DocumentRepostHandler = (*CryptoPaymentHandler)(nil)
