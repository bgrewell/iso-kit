package logging

import (
	"github.com/go-logr/logr"
)

const (
	LEVEL_INFO  = 0
	LEVEL_DEBUG = 1
	LEVEL_TRACE = 2
)

// NewLogger creates a new Logger instance with the given configuration
func NewLogger(log logr.Logger) *Logger {
	if log.GetSink() == nil {
		log = logr.Discard()
	}
	return &Logger{log: log}
}

// DefaultLogger returns a SimpleTextLogger
func DefaultLogger() *Logger {
	//return &Logger{log: NewSimpleLogger(os.Stdout, LEVEL_TRACE, true)}
	return &Logger{log: logr.Discard()}
}

// Logger is a struct that wraps the logr.Logger interface.
type Logger struct {
	log logr.Logger
}

// Log methods (minimizing footprint in the rest of the library)
func (l *Logger) Debug(msg string, keysAndValues ...interface{}) {
	l.log.V(LEVEL_DEBUG).Info(msg, keysAndValues...)
}

func (l *Logger) Info(msg string, keysAndValues ...interface{}) {
	l.log.Info(msg, keysAndValues...)
}

func (l *Logger) Trace(msg string, keysAndValues ...interface{}) {
	l.log.V(LEVEL_TRACE).Info(msg, keysAndValues...)
}

func (l *Logger) Error(err error, msg string, keysAndValues ...interface{}) {
	l.log.Error(err, msg, keysAndValues...)
}
