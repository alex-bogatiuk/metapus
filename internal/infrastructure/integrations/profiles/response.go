package profiles

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alex-bogatiuk/metapus/internal/core/json"
	model "github.com/alex-bogatiuk/metapus/internal/domain/integrations"
)

// Classify is the default classification of a failed provider response.
func Classify(status int, body []byte) model.Failure { return ClassifyResponse(status, body, false) }

// ClassifyResponse classifies a response whose body may carry an error
// envelope the default does not know; a profile reports it as extraError.
func ClassifyResponse(status int, body []byte, extraError bool) model.Failure {
	var envelope struct {
		Error  json.RawMessage `json:"error"`
		Status struct {
			ErrorCode int `json:"error_code"`
		} `json:"status"`
	}
	_ = json.Unmarshal(body, &envelope)
	hasError := len(envelope.Error) > 0 && string(envelope.Error) != "null" || extraError || envelope.Status.ErrorCode != 0
	if status < 400 && !hasError {
		return ""
	}
	lower := strings.ToLower(string(body))
	if strings.Contains(lower, "monthly") && (strings.Contains(lower, "exceeded") || strings.Contains(lower, "exhausted") || strings.Contains(lower, "reached")) && (strings.Contains(lower, "limit") || strings.Contains(lower, "capacity") || strings.Contains(lower, "quota")) {
		return model.Exhausted
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return model.Unauthorized
	}
	if status == http.StatusTooManyRequests {
		return model.Throttled
	}
	return ""
}

func RetryAt(value string, now time.Time) time.Time {
	if n, err := strconv.Atoi(value); err == nil && n > 0 {
		return now.Add(time.Duration(min(n, 86400)) * time.Second)
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return MinTime(at, now.Add(24*time.Hour))
	}
	return now.Add(5 * time.Second)
}

// MinTime returns the earlier time.
func MinTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
