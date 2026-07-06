package logging

import (
	"context"
	"fmt"
	"github.com/zoncaesaradmin/platformkit/ctxutil"
	"strings"
	"sync"
)

const (
	fatalLogMessage = "Fatal log called: "
)

// MockLogger implements the Logger interface for testing purposes
type MockLogger struct {
	mu sync.RWMutex

	// Configuration
	level         Level
	componentName string
	serviceName   string
	fields        Fields
	context       context.Context

	// Captured logs for verification
	LogEntries []LogEntry

	// Behavior configuration
	ShouldPanic bool
	ShouldFatal bool
}

// LogEntry represents a captured log entry for testing verification
type LogEntry struct {
	Level   Level
	Message string
	Fields  Fields
	Error   error
}

// NewMockLogger creates a new mock logger for testing
func NewMockLogger() *MockLogger {
	return &MockLogger{
		level:      InfoLevel,
		LogEntries: make([]LogEntry, 0),
		fields:     make(Fields),
	}
}

// NewMockLoggerWithLevel creates a mock logger with a specific level
func NewMockLoggerWithLevel(level Level) *MockLogger {
	mock := NewMockLogger()
	mock.SetLevel(level)
	return mock
}

// NewMockLoggerWithConfig creates a mock logger from LoggerConfig for testing
func NewMockLoggerWithConfig(config *LoggerConfig) *MockLogger {
	mock := NewMockLogger()
	if config != nil {
		mock.SetLevel(config.Level)
		mock.SetComponentName(config.ComponentName)
		mock.SetServiceName(config.ServiceName)
	}
	return mock
}

// Level control methods
func (m *MockLogger) SetLevel(level Level) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.level = level
}

func (m *MockLogger) GetLevel() Level {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.level
}

func (m *MockLogger) IsLevelEnabled(level Level) bool {
	m.mu.RLock()
	currentLevel := m.level
	ctx := m.context
	m.mu.RUnlock()
	return level >= effectiveLevel(currentLevel, ctx)
}

// Basic logging methods
func (m *MockLogger) Debug(msg string) {
	m.log(DebugLevel, msg, nil, nil)
}

func (m *MockLogger) Info(msg string) {
	m.log(InfoLevel, msg, nil, nil)
}

func (m *MockLogger) Warn(msg string) {
	m.log(WarnLevel, msg, nil, nil)
}

func (m *MockLogger) Error(msg string) {
	m.log(ErrorLevel, msg, nil, nil)
}

func (m *MockLogger) Fatal(msg string) {
	m.log(FatalLevel, msg, nil, nil)
	if m.ShouldFatal {
		panic(fatalLogMessage + msg)
	}
}

func (m *MockLogger) Panic(msg string) {
	m.log(PanicLevel, msg, nil, nil)
	if m.ShouldPanic {
		panic(msg)
	}
}

// Formatted logging methods
func (m *MockLogger) Debugf(format string, args ...interface{}) {
	m.log(DebugLevel, fmt.Sprintf(format, args...), nil, nil)
}

func (m *MockLogger) Infof(format string, args ...interface{}) {
	m.log(InfoLevel, fmt.Sprintf(format, args...), nil, nil)
}

func (m *MockLogger) Warnf(format string, args ...interface{}) {
	m.log(WarnLevel, fmt.Sprintf(format, args...), nil, nil)
}

func (m *MockLogger) Errorf(format string, args ...interface{}) {
	m.log(ErrorLevel, fmt.Sprintf(format, args...), nil, nil)
}

func (m *MockLogger) Fatalf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	m.log(FatalLevel, msg, nil, nil)
	if m.ShouldFatal {
		panic(fatalLogMessage + msg)
	}
}

func (m *MockLogger) Panicf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	m.log(PanicLevel, msg, nil, nil)
	if m.ShouldPanic {
		panic(msg)
	}
}

// Variadic logging methods
func (m *MockLogger) Debugw(msg string, keysAndValues ...interface{}) {
	m.log(DebugLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
}

func (m *MockLogger) Infow(msg string, keysAndValues ...interface{}) {
	m.log(InfoLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
}

func (m *MockLogger) Warnw(msg string, keysAndValues ...interface{}) {
	m.log(WarnLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
}

func (m *MockLogger) Errorw(msg string, keysAndValues ...interface{}) {
	m.log(ErrorLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
}

func (m *MockLogger) Fatalw(msg string, keysAndValues ...interface{}) {
	m.log(FatalLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
	if m.ShouldFatal {
		panic(fatalLogMessage + msg)
	}
}

func (m *MockLogger) Panicw(msg string, keysAndValues ...interface{}) {
	m.log(PanicLevel, msg, keysAndValuesToFields(keysAndValues...), nil)
	if m.ShouldPanic {
		panic(msg)
	}
}

// Structured logging with fields
func (m *MockLogger) WithFields(fields Fields) Logger {
	m.mu.RLock()
	newFields := make(Fields)
	for k, v := range m.fields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	m.mu.RUnlock()

	newLogger := &MockLogger{
		level:         m.level,
		componentName: m.componentName,
		serviceName:   m.serviceName,
		fields:        newFields,
		context:       m.context,
		LogEntries:    make([]LogEntry, 0),
		ShouldPanic:   m.ShouldPanic,
		ShouldFatal:   m.ShouldFatal,
	}
	return newLogger
}

func (m *MockLogger) WithField(key string, value interface{}) Logger {
	return m.WithFields(Fields{key: value})
}

func (m *MockLogger) WithError(err error) Logger {
	if err == nil {
		return m
	}

	newLogger := m.WithFields(Fields{"error": err.Error()}).(*MockLogger)

	// Store the actual error for test verification in the most recent entry
	newLogger.mu.Lock()
	if len(newLogger.LogEntries) > 0 {
		newLogger.LogEntries[len(newLogger.LogEntries)-1].Error = err
	}
	newLogger.mu.Unlock()

	return newLogger
}

// Context-aware logging
func (m *MockLogger) WithContext(ctx context.Context) Logger {
	newLogger := m.Clone().(*MockLogger)
	newLogger.context = ctx
	return newLogger
}

// Log at specific level
func (m *MockLogger) Log(level Level, msg string) {
	m.log(level, msg, nil, nil)
}

func (m *MockLogger) Logf(level Level, format string, args ...interface{}) {
	m.log(level, fmt.Sprintf(format, args...), nil, nil)
}

func (m *MockLogger) Logw(level Level, msg string, keysAndValues ...interface{}) {
	m.log(level, msg, keysAndValuesToFields(keysAndValues...), nil)
}

// Clone creates a copy of the logger
func (m *MockLogger) Clone() Logger {
	m.mu.RLock()
	defer m.mu.RUnlock()

	newFields := make(Fields)
	for k, v := range m.fields {
		newFields[k] = v
	}

	return &MockLogger{
		level:         m.level,
		componentName: m.componentName,
		serviceName:   m.serviceName,
		fields:        newFields,
		context:       m.context,
		LogEntries:    make([]LogEntry, 0),
		ShouldPanic:   m.ShouldPanic,
		ShouldFatal:   m.ShouldFatal,
	}
}

// Close closes the logger and releases any resources
func (m *MockLogger) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LogEntries = nil
	m.fields = nil
	return nil
}

// log is the internal method that captures all log entries
func (m *MockLogger) log(level Level, msg string, additionalFields Fields, err error) {
	if !m.IsLevelEnabled(level) {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Combine fields
	allFields := make(Fields)
	for k, v := range ctxutil.FieldsFromContext(m.context) {
		allFields[k] = v
	}
	for k, v := range m.fields {
		allFields[k] = v
	}
	for k, v := range additionalFields {
		allFields[k] = v
	}

	// Add component and service info
	if m.componentName != "" {
		allFields["component"] = m.componentName
	}
	if m.serviceName != "" {
		allFields["service"] = m.serviceName
	}

	entry := LogEntry{
		Level:   level,
		Message: msg,
		Fields:  allFields,
		Error:   err,
	}

	m.LogEntries = append(m.LogEntries, entry)
}

// Test helper methods for verification

// GetLogEntries returns all captured log entries (thread-safe copy)
func (m *MockLogger) GetLogEntries() []LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries := make([]LogEntry, len(m.LogEntries))
	copy(entries, m.LogEntries)
	return entries
}

// GetLogEntriesByLevel returns log entries filtered by level
func (m *MockLogger) GetLogEntriesByLevel(level Level) []LogEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var filtered []LogEntry
	for _, entry := range m.LogEntries {
		if entry.Level == level {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

// HasLogEntry checks if a log entry with the exact message exists at the given level
func (m *MockLogger) HasLogEntry(level Level, message string) bool {
	entries := m.GetLogEntriesByLevel(level)
	for _, entry := range entries {
		if entry.Message == message {
			return true
		}
	}
	return false
}

// HasLogEntryContaining checks if any log entry contains the specified text
func (m *MockLogger) HasLogEntryContaining(level Level, text string) bool {
	entries := m.GetLogEntriesByLevel(level)
	for _, entry := range entries {
		if strings.Contains(entry.Message, text) {
			return true
		}
	}
	return false
}

// HasLogEntryWithField checks if any log entry has the specified field with the given value
func (m *MockLogger) HasLogEntryWithField(level Level, fieldKey string, fieldValue interface{}) bool {
	entries := m.GetLogEntriesByLevel(level)
	for _, entry := range entries {
		if val, exists := entry.Fields[fieldKey]; exists && val == fieldValue {
			return true
		}
	}
	return false
}

// HasLogEntryWithFields checks if any log entry has all the specified fields
func (m *MockLogger) HasLogEntryWithFields(level Level, fields Fields) bool {
	entries := m.GetLogEntriesByLevel(level)
	for _, entry := range entries {
		allMatch := true
		for key, expectedValue := range fields {
			if val, exists := entry.Fields[key]; !exists || val != expectedValue {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}

// GetLogEntryWithMessage returns the first log entry with the exact message, or nil if not found
func (m *MockLogger) GetLogEntryWithMessage(level Level, message string) *LogEntry {
	entries := m.GetLogEntriesByLevel(level)
	for _, entry := range entries {
		if entry.Message == message {
			return &entry
		}
	}
	return nil
}

// GetLogEntriesContaining returns all log entries containing the specified text
func (m *MockLogger) GetLogEntriesContaining(level Level, text string) []LogEntry {
	entries := m.GetLogEntriesByLevel(level)
	var matching []LogEntry
	for _, entry := range entries {
		if strings.Contains(entry.Message, text) {
			matching = append(matching, entry)
		}
	}
	return matching
}

// ClearLogEntries clears all captured log entries
func (m *MockLogger) ClearLogEntries() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LogEntries = make([]LogEntry, 0)
}

// GetLogCount returns the total number of captured log entries
func (m *MockLogger) GetLogCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.LogEntries)
}

// GetLogCountByLevel returns the number of log entries for a specific level
func (m *MockLogger) GetLogCountByLevel(level Level) int {
	return len(m.GetLogEntriesByLevel(level))
}

// SetComponentName sets the component name for all future log entries
func (m *MockLogger) SetComponentName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.componentName = name
}

// SetServiceName sets the service name for all future log entries
func (m *MockLogger) SetServiceName(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.serviceName = name
}

// GetComponentName returns the current component name
func (m *MockLogger) GetComponentName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.componentName
}

// GetServiceName returns the current service name
func (m *MockLogger) GetServiceName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.serviceName
}

// EnablePanicOnFatal configures the mock to panic when Fatal methods are called
func (m *MockLogger) EnablePanicOnFatal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldFatal = true
}

// EnablePanicOnPanic configures the mock to panic when Panic methods are called
func (m *MockLogger) EnablePanicOnPanic() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldPanic = true
}

// DisablePanicBehavior disables panic behavior for Fatal and Panic methods
func (m *MockLogger) DisablePanicBehavior() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ShouldFatal = false
	m.ShouldPanic = false
}

// String returns a string representation of the mock logger for debugging
func (m *MockLogger) String() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return fmt.Sprintf("MockLogger{level:%s, entries:%d, component:%s, service:%s}",
		m.level.String(), len(m.LogEntries), m.componentName, m.serviceName)
}
