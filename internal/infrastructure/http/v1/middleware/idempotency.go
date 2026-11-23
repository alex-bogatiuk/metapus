package middleware

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/alex-bogatiuk/metapus/internal/application/command"
	"github.com/alex-bogatiuk/metapus/internal/core/apperror"
	appctx "github.com/alex-bogatiuk/metapus/internal/core/context"
	"github.com/alex-bogatiuk/metapus/internal/core/json"
	"github.com/alex-bogatiuk/metapus/internal/infrastructure/storage/postgres"
)

const HeaderIdempotencyKey = "Idempotency-Key"
const HeaderIdempotentReplayed = "Idempotent-Replayed"
const _maxIdempotencyBodyBytes = 1 << 20 // 1 MiB

// CommandIdempotency executes each mutating request as one durable application
// command. Authentication and merchant-scope middleware must run before it.
func CommandIdempotency(required bool) gin.HandlerFunc {
	return CommandIdempotencyWithReplay(required, nil)
}

// ReplayPresenter reauthorizes the resource and produces its current permitted
// representation. It never reexecutes the mutation.
type ReplayPresenter func(context.Context, command.Result) (command.Result, error)

func CommandIdempotencyWithReplay(required bool, replay ReplayPresenter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isMutation(c.Request.Method) {
			c.Next()
			return
		}

		key := strings.TrimSpace(c.GetHeader(HeaderIdempotencyKey))
		if key == "" {
			if required {
				_ = c.Error(apperror.NewIdempotencyKeyRequired())
				c.Abort()
				return
			}
			if replay != nil {
				executeUnkeyedCatalogMutation(c)
				return
			}
			c.Next()
			return
		}
		if len(key) > 200 {
			_ = c.Error(apperror.NewValidation("Idempotency-Key is too long"))
			c.Abort()
			return
		}

		body, canonicalBody, err := readCanonicalCommandBody(c.Request)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))

		scopeType, scopeID, err := trustedCommandScope(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		operation := c.Request.Method + " " + c.FullPath()
		fingerprint, err := command.Fingerprint(struct {
			Path  string          `json:"path"`
			Query string          `json:"query"`
			Body  json.RawMessage `json:"body"`
		}{
			Path:  c.Request.URL.EscapedPath(),
			Query: canonicalQuery(c.Request),
			Body:  canonicalBody,
		})
		if err != nil {
			_ = c.Error(apperror.NewInternal(err))
			c.Abort()
			return
		}

		metadata := command.Metadata{
			ScopeType:          scopeType,
			ScopeID:            scopeID,
			Operation:          operation,
			IdempotencyKey:     key,
			RequestFingerprint: fingerprint,
		}
		txm := postgres.MustGetTxManager(c.Request.Context())
		executor := command.NewExecutor(txm, postgres.NewCommandRepository())
		originalWriter := c.Writer
		buffered := newCommandResponseBuffer(originalWriter)
		c.Writer = buffered
		originalRequest := c.Request

		execution, execErr := executor.ExecuteWithFailureMapper(c.Request.Context(), metadata, func(txCtx context.Context) (command.Result, error) {
			c.Request = c.Request.WithContext(txCtx)
			c.Next()
			if len(c.Errors) > 0 {
				return command.Result{}, c.Errors.Last().Err
			}
			return buffered.commandResult(), nil
		}, stableCommandFailure)
		c.Request = originalRequest
		c.Writer = originalWriter
		if execErr != nil {
			c.Abort()
			switch {
			case errors.Is(execErr, command.ErrKeyReused):
				_ = c.Error(apperror.NewIdempotencyMismatch(key))
			case errors.Is(execErr, command.ErrInProgress):
				_ = c.Error(apperror.NewIdempotencyConflict(key).WithDetail("retryAfterSeconds", 3))
			default:
				_ = c.Error(execErr)
			}
			return
		}

		if execution.Replayed {
			if replay != nil && !strings.HasPrefix(execution.Result.Outcome, "error:") {
				execution.Result, err = replay(c.Request.Context(), execution.Result)
				if err != nil {
					_ = c.Error(err)
					c.Abort()
					return
				}
			}
			originalWriter.Header().Set(HeaderIdempotentReplayed, "true")
			writeCommandResult(originalWriter, execution.Result)
			c.Abort()
			return
		}
		if strings.HasPrefix(execution.Result.Outcome, "error:") {
			// The error has been serialized into the durable command result. Do
			// not let the outer error middleware render it a second time.
			c.Errors = nil
			writeCommandResult(originalWriter, execution.Result)
			c.Abort()
			return
		}
		buffered.flushTo(originalWriter)
	}
}

// Catalog presentation participates in the mutation boundary, including when
// the caller did not request durable idempotency. A failed projection or FK read
// must not commit a write while returning an error response.
func executeUnkeyedCatalogMutation(c *gin.Context) {
	originalRequest, originalWriter := c.Request, c.Writer
	buffered := newCommandResponseBuffer(originalWriter)
	c.Writer = buffered
	err := postgres.MustGetTxManager(originalRequest.Context()).RunInTransaction(originalRequest.Context(), func(ctx context.Context) error {
		c.Request = originalRequest.WithContext(ctx)
		c.Next()
		if len(c.Errors) > 0 {
			return c.Errors.Last().Err
		}
		return nil
	})
	c.Request, c.Writer = originalRequest, originalWriter
	if err != nil {
		if len(c.Errors) == 0 {
			_ = c.Error(err)
		}
		c.Abort()
		return
	}
	buffered.flushTo(originalWriter)
}

// stableCommandFailure deliberately excludes security, retryable capacity and
// server failures. Replaying those without re-running current authorization or
// infrastructure checks would be unsafe.
func stableCommandFailure(err error) (command.Result, bool) {
	appErr, ok := apperror.AsAppError(err)
	if !ok {
		return command.Result{}, false
	}
	switch appErr.HTTPStatus {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity:
	default:
		return command.Result{}, false
	}
	payload, marshalErr := json.Marshal(struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details,omitempty"`
	}{Code: appErr.Code, Message: appErr.Message, Details: appErr.Details})
	if marshalErr != nil {
		return command.Result{}, false
	}
	return command.Result{
		Outcome: "error:" + strconv.Itoa(appErr.HTTPStatus),
		Payload: payload,
	}, true
}

func readCanonicalCommandBody(request *http.Request) ([]byte, json.RawMessage, error) {
	limited := io.LimitReader(request.Body, _maxIdempotencyBodyBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, nil, apperror.NewInternal(err).WithDetail("component", "command_body")
	}
	if len(body) > _maxIdempotencyBodyBytes {
		err := apperror.NewValidation("request body too large for idempotency")
		err.HTTPStatus = http.StatusRequestEntityTooLarge
		return nil, nil, err.WithDetail("max_bytes", _maxIdempotencyBodyBytes)
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return body, json.RawMessage(`{}`), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, nil, apperror.NewInvalidRequest(err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("request body must contain exactly one JSON value")
		}
		return nil, nil, apperror.NewInvalidRequest(err)
	}
	canonical, err := json.MarshalDeterministic(decoded)
	if err != nil {
		return nil, nil, apperror.NewInvalidRequest(err)
	}
	return body, canonical, nil
}

func canonicalQuery(request *http.Request) string {
	query := request.URL.Query()
	for key := range query {
		sort.Strings(query[key])
	}
	return query.Encode()
}

// CommandScopeResolver names the trusted owner that idempotent command keys
// are scoped to, such as a configuration's partner account. Middleware that
// authenticates such an owner installs it with WithCommandScope.
type CommandScopeResolver func(c *gin.Context) (kind, ownerID string, err error)

type commandScopeKey struct{}

// WithCommandScope makes idempotent commands in this request scope their
// keys through resolve instead of the authenticated user.
func WithCommandScope(ctx context.Context, resolve CommandScopeResolver) context.Context {
	return context.WithValue(ctx, commandScopeKey{}, resolve)
}

func trustedCommandScope(c *gin.Context) (string, string, error) {
	if resolve, ok := c.Request.Context().Value(commandScopeKey{}).(CommandScopeResolver); ok && resolve != nil {
		return resolve(c)
	}
	if user := appctx.GetUser(c.Request.Context()); user != nil && strings.TrimSpace(user.UserID) != "" {
		return "user", user.UserID, nil
	}
	return "", "", apperror.NewUnauthorized("authenticated command scope is required")
}

func isMutation(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}

type commandResponseBuffer struct {
	gin.ResponseWriter
	body   bytes.Buffer
	status int
}

func newCommandResponseBuffer(writer gin.ResponseWriter) *commandResponseBuffer {
	return &commandResponseBuffer{ResponseWriter: writer, status: http.StatusOK}
}

func (w *commandResponseBuffer) WriteHeader(status int) { w.status = status }
func (w *commandResponseBuffer) WriteHeaderNow()        {}
func (w *commandResponseBuffer) Write(data []byte) (int, error) {
	return w.body.Write(data)
}
func (w *commandResponseBuffer) WriteString(data string) (int, error) {
	return w.body.WriteString(data)
}
func (w *commandResponseBuffer) Status() int { return w.status }
func (w *commandResponseBuffer) Size() int   { return w.body.Len() }
func (w *commandResponseBuffer) Written() bool {
	return w.body.Len() > 0 || w.status != http.StatusOK
}
func (w *commandResponseBuffer) Flush() {}

func (w *commandResponseBuffer) commandResult() command.Result {
	outcome := "success"
	switch w.status {
	case http.StatusCreated:
		outcome = "created"
	case http.StatusNoContent:
		outcome = "no_content"
	}
	result := command.Result{Outcome: outcome}
	if w.body.Len() > 0 {
		result.Payload = append(json.RawMessage(nil), w.body.Bytes()...)
		var resource struct {
			ID              string `json:"id"`
			Object          string `json:"object"`
			Version         int64  `json:"version"`
			ResourceVersion int64  `json:"resourceVersion"`
		}
		if json.Unmarshal(w.body.Bytes(), &resource) == nil {
			result.ResourceID = resource.ID
			result.ResourceType = resource.Object
			result.ResourceVersion = resource.Version
			if result.ResourceVersion == 0 {
				result.ResourceVersion = resource.ResourceVersion
			}
		}
	}
	return result
}

func (w *commandResponseBuffer) flushTo(target gin.ResponseWriter) {
	target.WriteHeader(w.status)
	if w.body.Len() > 0 {
		_, _ = target.Write(w.body.Bytes())
	}
}

func writeCommandResult(writer gin.ResponseWriter, result command.Result) {
	status := http.StatusOK
	switch result.Outcome {
	case "created":
		status = http.StatusCreated
	case "no_content":
		status = http.StatusNoContent
	default:
		if rawStatus, ok := strings.CutPrefix(result.Outcome, "error:"); ok {
			if parsed, err := strconv.Atoi(rawStatus); err == nil && parsed >= 400 && parsed <= 499 {
				status = parsed
			}
		}
	}
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	if len(result.Payload) > 0 && status != http.StatusNoContent {
		_, _ = writer.Write(result.Payload)
	}
}
