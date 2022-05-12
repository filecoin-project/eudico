package logging

import (
	"context"

	logging "github.com/ipfs/go-log/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type contextKey string

const loggerKey = contextKey("logger")

func FromContext(ctx context.Context, logger *logging.ZapEventLogger) *logging.ZapEventLogger {
	if logger, ok := ctx.Value(loggerKey).(*logging.ZapEventLogger); ok {
		return logger
	}
	return logger
}

func WithLogger(ctx context.Context, logger *logging.ZapEventLogger) context.Context {
	return context.WithValue(ctx, loggerKey, logger)
}

func NewFileLogger(level string, name string) (*logging.ZapEventLogger, error) {
	cfg := zap.NewDevelopmentConfig()
	cfg.OutputPaths = []string{
		name,
	}
	if level == "" {
		level = zap.InfoLevel.String()
	}
	logLevel, err := logging.LevelFromString(level)
	if err != nil {
		return nil, err
	}
	cfg.Level = zap.NewAtomicLevelAt(zapcore.Level(logLevel))
	logger, err := cfg.Build()
	if err != nil {
		return nil, err
	}

	sugaredLogger := logger.Sugar()
	realLogger := logging.ZapEventLogger{
		SugaredLogger: *sugaredLogger,
	}

	return &realLogger, nil

}
