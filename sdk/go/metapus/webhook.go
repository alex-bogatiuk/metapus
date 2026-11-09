package metapus

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Event[T any] struct {
	ID         string    `json:"id"`
	Object     string    `json:"object"`
	APIVersion string    `json:"apiVersion"`
	Type       EventType `json:"type"`
	CreatedAt  time.Time `json:"createdAt"`
	Data       struct {
		Object T `json:"object"`
	} `json:"data"`
}

// VerifyWebhook verifies the unmodified HTTP request body, accepts either
// active or rotation-overlap v1 signatures, and rejects stale deliveries.
func VerifyWebhook[T any](rawBody []byte, signatureHeader, secret string, now time.Time, tolerance time.Duration) (*Event[T], error) {
	var timestamp string
	var signatures []string
	for _, field := range strings.Split(signatureHeader, ",") {
		field = strings.TrimSpace(field)
		switch {
		case strings.HasPrefix(field, "t="):
			timestamp = strings.TrimPrefix(field, "t=")
		case strings.HasPrefix(field, "v1="):
			signatures = append(signatures, strings.TrimPrefix(field, "v1="))
		}
	}
	seconds, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || len(signatures) == 0 {
		return nil, errors.New("invalid Metapus-Signature")
	}
	deliveredAt := time.Unix(seconds, 0)
	if delta := now.Sub(deliveredAt); delta > tolerance || delta < -tolerance {
		return nil, errors.New("webhook timestamp is outside tolerance")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(timestamp + "."))
	_, _ = mac.Write(rawBody)
	expected := mac.Sum(nil)
	valid := 0
	for _, encoded := range signatures {
		supplied, decodeErr := hex.DecodeString(encoded)
		if decodeErr == nil && len(supplied) == len(expected) {
			valid |= subtle.ConstantTimeCompare(supplied, expected)
		}
	}
	if valid != 1 {
		return nil, errors.New("invalid webhook signature")
	}
	var event Event[T]
	if err := json.Unmarshal(rawBody, &event); err != nil {
		return nil, err
	}
	return &event, nil
}
