package mir

import (
	logging "github.com/ipfs/go-log/v2"

	mirlogging "github.com/filecoin-project/mir/pkg/logging"
)

// managerLog is an Eudico logger used by a Mir node.
var managerLog = logging.Logger("mir-manager")

// mirLogger implements Mir's Log interface.
type mirLogger struct {
	logger *logging.ZapEventLogger
}

func newMirLogger(logger *logging.ZapEventLogger) *mirLogger {
	return &mirLogger{
		logger: logger,
	}
}

// Log logs a message with additional context.
func (m *mirLogger) Log(level mirlogging.LogLevel, text string, args ...interface{}) {
	switch level {
	case mirlogging.LevelError:
		m.logger.Errorw(text, "error", args)
	case mirlogging.LevelInfo:
		m.logger.Infow(text, "info", args)
	case mirlogging.LevelWarn:
		m.logger.Warnw(text, "warn", args)
	case mirlogging.LevelDebug:
		m.logger.Debugw(text, "debug", args)
	}
}
