// Package logger provides structured logging with context support.
package logger

import (
	"context"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	
	appctx "metapus/internal/core/context"
)

// Logger wraps zap.SugaredLogger with context-aware logging.
type Logger struct {
	*zap.SugaredLogger
}

// loggerKey is the context key for Logger.
type loggerKey struct{}

// Config holds logger configuration.
type Config struct {
	Level       string // debug, info, warn, error
	Development bool   // pretty print for dev
	OutputPaths []string
}

// New creates a new Logger from configuration.
func New(cfg Config) (*Logger, error) {
	level, err := zapcore.ParseLevel(cfg.Level)
	if err != nil {
		level = zapcore.InfoLevel
	}
	
	var config zap.Config
	if cfg.Development {
		config = zap.NewDevelopmentConfig()
		config.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		config = zap.NewProductionConfig()
	}
	
	config.Level = zap.NewAtomicLevelAt(level)
	if len(cfg.OutputPaths) > 0 {
		config.OutputPaths = cfg.OutputPaths
	}
	
	zapLogger, err := config.Build(zap.AddCallerSkip(1))
	if err != nil {
		return nil, err
	}
	
	return &Logger{zapLogger.Sugar()}, nil
}

// Default returns a default logger writing to stdout.
func Default() *Logger {
	defaultOnce.Do(func() {
		config := zap.NewProductionConfig()
		config.OutputPaths = []string{"stdout"}
		zapLogger, _ := config.Build(zap.AddCallerSkip(1))
		defaultLogger = &Logger{zapLogger.Sugar()}
	})
	return defaultLogger
}

var (
	defaultOnce   sync.Once
	defaultLogger *Logger
)

// WithContext adds trace and user info from context.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	sugar := l.SugaredLogger
	
	// Add trace info
	if trace := appctx.GetTrace(ctx); trace != nil {
		sugar = sugar.With(
			"trace_id", trace.TraceID,
			"request_id", trace.RequestID,
		)
	}
	
	// Add user info
	if user := appctx.GetUser(ctx); user != nil {
		sugar = sugar.With(
			"user_id", user.UserID,
			"tenant_id", user.TenantID,
		)
	}
	
	return &Logger{sugar}
}

// With adds key-value pairs to logger.
func (l *Logger) With(keysAndValues ...any) *Logger {
	return &Logger{l.SugaredLogger.With(keysAndValues...)}
}

// WithComponent adds component name to logger.
func (l *Logger) WithComponent(name string) *Logger {
	return &Logger{l.SugaredLogger.With("component", name)}
}

// --- Context-based logger access ---

// WithLogger adds Logger to context.
func WithLogger(ctx context.Context, logger *Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, logger)
}

// FromContext returns Logger from context or default logger.
func FromContext(ctx context.Context) *Logger {
	if l, ok := ctx.Value(loggerKey{}).(*Logger); ok {
		return l.WithContext(ctx)
	}
	return Default().WithContext(ctx)
}

// Debug logs at debug level from context.
func Debug(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Debugw(msg, keysAndValues...)
}

// Info logs at info level from context.
func Info(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Infow(msg, keysAndValues...)
}

// Warn logs at warn level from context.
func Warn(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Warnw(msg, keysAndValues...)
}

// Error logs at error level from context.
func Error(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Errorw(msg, keysAndValues...)
}

// Fatal logs at fatal level and exits.
func Fatal(ctx context.Context, msg string, keysAndValues ...any) {
	FromContext(ctx).Fatalw(msg, keysAndValues...)
	os.Exit(1)
}
