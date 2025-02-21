package logging

import (
	"bytes"
	"errors"
	"github.com/go-logr/logr"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Test that if writer is nil, the logger defaults to os.Stdout.
func TestDefaultWriter(t *testing.T) {
	s := NewSimpleLogSink(nil, 1, true)
	if s.writer != os.Stdout {
		t.Errorf("expected default writer to be os.Stdout, got %v", s.writer)
	}
}

// Test that the Enabled method returns true only for levels less than or equal to minVerbosity.
func TestEnabled(t *testing.T) {
	s := NewSimpleLogSink(&bytes.Buffer{}, 1, true)
	if !s.Enabled(0) {
		t.Error("expected level 0 to be enabled")
	}
	if !s.Enabled(1) {
		t.Error("expected level 1 to be enabled")
	}
	if s.Enabled(2) {
		t.Error("expected level 2 to be disabled")
	}
}

// Test that Info() writes a properly formatted (and colored) log message.
func TestInfoLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	s.Info(0, "Hello world", "key", "value")
	output := buf.String()

	if !strings.Contains(output, "Hello world") {
		t.Errorf("expected output to contain 'Hello world', got %q", output)
	}
	if !strings.Contains(output, "key: value") {
		t.Errorf("expected output to contain key-value pair, got %q", output)
	}
	// Check that the correct label is used for level 0 (should be "[INFO]")
	if !strings.Contains(output, "[INFO]") {
		t.Errorf("expected output to contain [INFO] label, got %q", output)
	}
}

// Test that a log at a level higher than minVerbosity is not written.
func TestInfoNotLoggedWhenDisabled(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 0, true) // Only level 0 enabled.
	s.Info(1, "This should not be logged", "foo", "bar")
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

// Test that Error() writes an error log with the proper label and key/value output.
func TestErrorLogging(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 0, true)
	err := errors.New("sample error")
	s.Error(err, "An error occurred", "context", "testing")
	output := buf.String()

	if !strings.Contains(output, "[ERROR]") {
		t.Errorf("expected output to contain [ERROR] label, got %q", output)
	}
	if !strings.Contains(output, "An error occurred") {
		t.Errorf("expected error message, got %q", output)
	}
	// The Error method appends an "error" key and the error value.
	if !strings.Contains(output, "context: testing") {
		t.Errorf("expected context key-value, got %q", output)
	}
	if !strings.Contains(output, "error: sample error") {
		t.Errorf("expected error key-value, got %q", output)
	}
}

// Test that WithName returns a new logger whose messages include the name prefix.
func TestWithName(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	named := s.WithName("MyLogger")
	named.Info(0, "Test message")
	output := buf.String()

	if !strings.Contains(output, "[MyLogger]") {
		t.Errorf("expected output to contain [MyLogger], got %q", output)
	}
}

// Test that chaining WithName produces a combined name.
func TestChainedWithName(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	// Each WithName returns a new logr.LogSink; we type assert back to *SimpleLogSink.
	chain := s.WithName("A").WithName("B").(*SimpleLogSink)
	chain.Info(0, "Chained name")
	output := buf.String()

	if !strings.Contains(output, "[A.B]") {
		t.Errorf("expected output to contain [A.B], got %q", output)
	}
}

// Test that V returns a new logger and that a log with the given level is formatted correctly.
func TestVMethod(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	v := s.V(1)
	v.Info(1, "Verbose log")
	output := buf.String()

	// Level 1 should use the [DEBUG] label.
	if !strings.Contains(output, "[DEBUG]") {
		t.Errorf("expected output to contain [DEBUG] label, got %q", output)
	}
}

// Test that if a key in the key-value list isn’t a string, it is replaced with a formatted key.
func TestNonStringKey(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	// Pass an int instead of a string as the key.
	s.Info(0, "Non-string key", 123, "value")
	output := buf.String()

	if !strings.Contains(output, "key0: value") {
		t.Errorf("expected output to contain 'key0: value', got %q", output)
	}
}

// Test that Init properly sets the callDepth field (using reflection because the field is unexported).
func TestInitSetsCallDepth(t *testing.T) {
	buf := &bytes.Buffer{}
	s := NewSimpleLogSink(buf, 1, true)
	info := logr.RuntimeInfo{CallDepth: 5}
	s.Init(info)

	val := reflect.ValueOf(s).Elem()
	cd := val.FieldByName("callDepth").Int()
	if cd != 5 {
		t.Errorf("expected callDepth 5, got %d", cd)
	}
}

// Test that NewSimpleLogger returns a logr.Logger that writes output as expected.
func TestNewSimpleLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewSimpleLogger(buf, 1, true)
	logger.Info("Logger info", "testKey", "testValue")
	output := buf.String()

	if !strings.Contains(output, "Logger info") {
		t.Errorf("expected logger info message, got %q", output)
	}
}
