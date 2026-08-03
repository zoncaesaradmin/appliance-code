package logging

import (
	"context"
	"fmt"
	"io"
	"strings"
)

// Level represents the logging level
type Level int

const (
	// DebugLevel logs are typically voluminous, and are usually disabled in production
	DebugLevel Level = iota
	// InfoLevel is the default logging priority
	InfoLevel
	// WarnLevel logs are more important than Info, but don't need individual human review
	WarnLevel
	// ErrorLevel logs are high-priority. If an application is running smoothly, it shouldn't generate any error-level logs
	ErrorLevel
	// FatalLevel logs a message, then calls os.Exit(1)
	FatalLevel
	// PanicLevel logs a message, then panics
	PanicLevel
)

// String returns the string representation of the log level
func (l Level) String() string {
	switch l {
	case DebugLevel:
		return "DEBUG"
	case InfoLevel:
		return "INFO"
	case WarnLevel:
		return "WARN"
	case ErrorLevel:
		return "ERROR"
	case FatalLevel:
		return "FATAL"
	case PanicLevel:
		return "PANIC"
	default:
		return "UNKNOWN"
	}
}

// ConvertToLogLevel converts a string log level to logging.Level
func ConvertToLogLevel(levelStr string) Level {
	switch strings.ToLower(levelStr) {
	case "debug":
		return DebugLevel
	case "info":
		return InfoLevel
	case "warn":
		return WarnLevel
	case "error":
		return ErrorLevel
	case "fatal":
		return FatalLevel
	case "panic":
		return PanicLevel
	default:
		return InfoLevel // Default to info if invalid level
	}
}

// Fields represents structured logging fields
type Fields map[string]interface{}

// OutputTarget determines where log entries are written.
type OutputTarget string

const (
	OutputTargetFile   OutputTarget = "file"
	OutputTargetStdout OutputTarget = "stdout"
	OutputTargetStderr OutputTarget = "stderr"
	outputTargetWriter OutputTarget = "writer"
)

// Logger defines the interface for structured logging
type Logger interface {
	// Level control
	SetLevel(level Level)
	GetLevel() Level
	IsLevelEnabled(level Level) bool

	// Basic logging methods
	Debug(msg string)
	Info(msg string)
	Warn(msg string)
	Error(msg string)
	Fatal(msg string)
	Panic(msg string)

	// Formatted logging methods (printf-style)
	Debugf(format string, args ...interface{})
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	Panicf(format string, args ...interface{})

	// Variadic logging methods (anything that implements fmt.Stringer or has String())
	Debugw(msg string, keysAndValues ...interface{})
	Infow(msg string, keysAndValues ...interface{})
	Warnw(msg string, keysAndValues ...interface{})
	Errorw(msg string, keysAndValues ...interface{})
	Fatalw(msg string, keysAndValues ...interface{})
	Panicw(msg string, keysAndValues ...interface{})

	// Structured logging with fields
	WithFields(fields Fields) Logger
	WithField(key string, value interface{}) Logger
	WithError(err error) Logger

	// Context-aware logging
	WithContext(ctx context.Context) Logger

	// Log at specific level
	Log(level Level, msg string)
	Logf(level Level, format string, args ...interface{})
	Logw(level Level, msg string, keysAndValues ...interface{})

	// Clone creates a copy of the logger
	Clone() Logger

	// Close closes the logger and releases any resources
	Close() error
}

// Helper function to convert key-value pairs to Fields
func keysAndValuesToFields(keysAndValues ...interface{}) Fields {
	fields := make(Fields)
	for i := 0; i < len(keysAndValues); i += 2 {
		if i+1 < len(keysAndValues) {
			key := fmt.Sprintf("%v", keysAndValues[i])
			fields[key] = keysAndValues[i+1]
		}
	}
	return fields
}

// LoggerConfig holds comprehensive configuration for creating loggers
type LoggerConfig struct {
	// Basic configuration
	Level         Level        // Log level (Debug, Info, Warn, Error, Fatal, Panic)
	FilePath      string       // Complete path to log file when OutputTargetFile is used
	LoggerName    string       // Name identifier for the logger instance
	ComponentName string       // Component/module name for structured logging
	ServiceName   string       // Service name for structured logging
	OutputTarget  OutputTarget // file, stdout, or stderr. File output remains the default.
	OutputWriter  io.Writer    // Optional custom writer. When set, it takes precedence over OutputTarget/FilePath.

	// Error metadata controls
	IncludeCallerOnError     bool // Adds a caller field for Error/Fatal/Panic events.
	IncludeStackTraceOnError bool // Adds a stack field for Error/Fatal/Panic events.
}

// DefaultLoggerConfig returns the default logger configuration
func DefaultLoggerConfig() *LoggerConfig {
	return &LoggerConfig{
		Level:         InfoLevel,
		FilePath:      "/tmp/app.log", // Default file path only
		LoggerName:    "default",
		ComponentName: "application",
		ServiceName:   "service",
		OutputTarget:  OutputTargetFile,
	}
}

// Validate validates the logger configuration
func (c *LoggerConfig) Validate() error {
	if c.LoggerName == "" {
		return fmt.Errorf("logger name is required")
	}
	if c.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}

	switch c.resolveOutputTarget() {
	case OutputTargetFile:
		if c.FilePath == "" {
			return fmt.Errorf("filename is required - stdout logging is not allowed")
		}
	case OutputTargetStdout, OutputTargetStderr, outputTargetWriter:
		// valid non-file targets
	default:
		return fmt.Errorf("invalid output target %q", c.OutputTarget)
	}
	return nil
}

func (c *LoggerConfig) resolveOutputTarget() OutputTarget {
	if c == nil {
		return OutputTargetFile
	}
	if c.OutputWriter != nil {
		return outputTargetWriter
	}
	if c.OutputTarget != "" {
		return c.OutputTarget
	}
	if c.FilePath != "" {
		return OutputTargetFile
	}
	return OutputTargetFile
}

// NewLogger creates a new logger with the given configuration
// This is a factory function that creates the default implementation (zerolog)
func NewLogger(config *LoggerConfig) (Logger, error) {
	if config == nil {
		config = DefaultLoggerConfig()
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid logger configuration: %w", err)
	}

	// Create zerolog implementation
	logger, err := NewLoggerWithConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	return logger, nil
}
