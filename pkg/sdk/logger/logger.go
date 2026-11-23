// Package logger is the platform's structured logger.
package logger

import (
	"context"

	core "github.com/alex-bogatiuk/metapus/pkg/logger"
)

type (
	// Logger wraps zap.SugaredLogger with context-aware logging.
	Logger = core.Logger
)

// Debug logs at debug level from context.
func Debug(ctx context.Context, msg string, keysAndValues ...any) {
	core.Debug(ctx, msg, keysAndValues...)
}

// Default returns a default logger writing to stdout.
func Default() *core.Logger { return core.Default() }

// Error logs at error level from context.
func Error(ctx context.Context, msg string, keysAndValues ...any) {
	core.Error(ctx, msg, keysAndValues...)
}

// Info logs at info level from context.
func Info(ctx context.Context, msg string, keysAndValues ...any) {
	core.Info(ctx, msg, keysAndValues...)
}

// Warn logs at warn level from context.
func Warn(ctx context.Context, msg string, keysAndValues ...any) {
	core.Warn(ctx, msg, keysAndValues...)
}
