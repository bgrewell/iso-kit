package logging

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fatih/color"
	"github.com/go-logr/logr"
)

// Define colored labels using fatih/color
var (
	infoColor  = color.New(color.FgGreen).SprintFunc()
	debugColor = color.New(color.FgCyan).SprintFunc()
	traceColor = color.New(color.FgYellow).SprintFunc() // Yellow is closest to brown
	errorColor = color.New(color.FgRed).SprintFunc()
)

// SimpleLogSink implements the logr.LogSink interface for human-readable output with colors.
type SimpleLogSink struct {
	writer       io.Writer
	minVerbosity int
	name         string
	keyValues    []interface{}
	mutex        sync.Mutex
	callDepth    int
	useColor     bool
}

// NewSimpleLogSink creates a new SimpleLogSink.
// If writer is nil, it defaults to os.Stdout.
// minVerbosity sets the minimum verbosity level to log.
func NewSimpleLogSink(writer io.Writer, minVerbosity int, useColor bool) *SimpleLogSink {
	if writer == nil {
		writer = os.Stdout
	}
	return &SimpleLogSink{
		writer:       writer,
		minVerbosity: minVerbosity,
		name:         "",
		keyValues:    []interface{}{},
		useColor:     useColor,
	}
}

// Init initializes the logger with runtime information.
func (s *SimpleLogSink) Init(info logr.RuntimeInfo) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.callDepth = info.CallDepth
}

// Enabled determines if the logger is enabled for the given verbosity level.
func (s *SimpleLogSink) Enabled(level int) bool {
	return level <= s.minVerbosity
}

// Info logs a non-error message with key-value pairs.
func (s *SimpleLogSink) Info(level int, msg string, keysAndValues ...interface{}) {
	if !s.Enabled(level) {
		return
	}
	s.log(false, level, msg, keysAndValues...)
}

// Error logs an error message with key-value pairs.
func (s *SimpleLogSink) Error(err error, msg string, keysAndValues ...interface{}) {
	allKeysAndValues := append(keysAndValues, "error", err)
	s.log(true, 0, msg, allKeysAndValues...) // Level is irrelevant for errors
}

// WithValues adds key-value pairs to the logger.
func (s *SimpleLogSink) WithValues(keysAndValues ...interface{}) logr.LogSink {
	newKeyValues := append(append([]interface{}{}, s.keyValues...), keysAndValues...)
	return &SimpleLogSink{
		writer:       s.writer,
		minVerbosity: s.minVerbosity,
		name:         s.name,
		keyValues:    newKeyValues,
	}
}

// WithName adds a name to the logger.
func (s *SimpleLogSink) WithName(name string) logr.LogSink {
	newName := name
	if s.name != "" {
		newName = fmt.Sprintf("%s.%s", s.name, name)
	}
	return &SimpleLogSink{
		writer:       s.writer,
		minVerbosity: s.minVerbosity,
		name:         newName,
		keyValues:    append([]interface{}{}, s.keyValues...),
	}
}

// V returns a new SimpleLogSink with the specified verbosity level.
func (s *SimpleLogSink) V(level int) logr.LogSink {
	return &SimpleLogSink{
		writer:       s.writer,
		minVerbosity: s.minVerbosity,
		name:         s.name,
		keyValues:    append([]interface{}{}, s.keyValues...),
	}
}

// log handles the formatting and writing of log messages with colors.
func (s *SimpleLogSink) log(isError bool, level int, msg string, keysAndValues ...interface{}) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	var label string
	if isError {
		label = fmt.Sprintf("%s%s ", errorColor("[ERROR]"), "") // Reset is handled by SprintFunc
	} else {
		switch level {
		case 0:
			label = fmt.Sprintf("%s%s ", infoColor("[INFO]"), "")
		case 1:
			label = fmt.Sprintf("%s%s ", debugColor("[DEBUG]"), "")
		case 2:
			label = fmt.Sprintf("%s%s ", traceColor("[TRACE]"), "")
		default:
			label = fmt.Sprintf("[LEVEL %d] ", level)
		}
	}

	// Construct the full message with optional name
	fullMsg := msg
	if s.name != "" {
		fullMsg = fmt.Sprintf("[%s] %s", s.name, msg)
	}

	// Combine label and message
	fullMsg = label + fullMsg

	// Write the message
	fmt.Fprintln(s.writer, fullMsg)

	// Write key-value pairs indented by two spaces (no color)
	for i := 0; i < len(keysAndValues)-1; i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok {
			key = fmt.Sprintf("key%d", i/2)
		}
		value := keysAndValues[i+1]
		fmt.Fprintf(s.writer, "  %s: %v\n", key, value)
	}
}

// NewSimpleLogger creates a new logr.Logger using SimpleLogSink.
// If writer is nil, it defaults to os.Stdout.
// minVerbosity sets the minimum verbosity level to log.
func NewSimpleLogger(writer io.Writer, minVerbosity int, useColor bool) logr.Logger {
	sink := NewSimpleLogSink(writer, minVerbosity, useColor)
	return logr.New(sink)
}
