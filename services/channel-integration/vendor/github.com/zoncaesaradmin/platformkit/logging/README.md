# Logging Package

A comprehensive, interface-based logging package for Go applications with support for structured logging, multiple log levels, and zerolog-based implementation.

## Features

- **Zerolog-based implementation** - Fast, structured logging with excellent performance
- **Interface-based design** - Easy to swap implementations based on environment or organizational preferences
- **Multiple log levels** - Debug, Info, Warn, Error, Fatal, Panic
- **Structured logging** - Support for fields and key-value pairs
- **Context-aware** - Built-in context support for tracing and request correlation
- **Thread-safe** - Concurrent access safe
- **Pluggable implementations** - Easy to implement custom loggers (e.g., for remote logging services)
- **Explicit logger instances** - Enforces explicit logger usage for better dependency management
- **High performance** - Leverages zerolog's efficient logging capabilities

## Quick Start

### Creating and Using Logger Instances

```go
import "platformkit/logging"

func main() {
    logger, _ := logging.NewLogger(&logging.LoggerConfig{
        Level:        logging.InfoLevel,
        OutputTarget: logging.OutputTargetStdout,
        LoggerName:   "api",
        ServiceName:  "user-service",
    })
    
    logger.Info("Application starting")
    logger.Infof("Server listening on port %d", 4477)
    logger.Infow("User logged in", "user_id", 12345, "ip", "192.168.1.1")
}
```

### Using Default Configuration

```go
import "platformkit/logging"

func main() {
    logger := logging.NewLogger(logging.DefaultLoggerConfig())
    logger.Info("Using default configuration")
}
```

## Logging Levels

- `DebugLevel` - Detailed information for debugging
- `InfoLevel` - General information (default level)
- `WarnLevel` - Warning messages
- `ErrorLevel` - Error messages  
- `FatalLevel` - Fatal errors (calls os.Exit(1))
- `PanicLevel` - Panic-level errors (calls panic())

## Logging Methods

### Basic Logging
```go
logger.Debug("Debug message")
logger.Info("Info message")
logger.Warn("Warning message")
logger.Error("Error message")
```

### Formatted Logging (printf-style)
```go
logger.Debugf("Processing request %d", requestID)
logger.Infof("User %s logged in", username)
logger.Errorf("Connection failed: %v", err)
```

### Structured Logging (key-value pairs)
```go
logger.Infow("User action",
    "user_id", 12345,
    "action", "create_post",
    "duration_ms", 150,
)
```

### With Fields
```go
// Persistent fields
serviceLogger := logger.WithFields(logging.Fields{
    "service": "user-api",
    "version": "1.0.0",
})

// Single field
requestLogger := logger.WithField("request_id", "req-123")

// With error
logger.WithError(err).Error("Operation failed")
```

### Context-Aware Logging
```go
ctx := context.WithValue(context.Background(), "trace_id", "trace-123")
contextLogger := logger.WithContext(ctx)
contextLogger.Info("Request processed")
```

### Standardized Log Context
```go
import (
    "context"
    "platformkit/ctxutil"
    "platformkit/logging"
)

func main() {
    logger, _ := logging.NewLogger(&logging.LoggerConfig{
        Level:                    logging.InfoLevel,
        OutputTarget:             logging.OutputTargetStdout,
        LoggerName:               "worker",
        ServiceName:              "rule-engine",
        IncludeCallerOnError:     true,
        IncludeStackTraceOnError: true,
    })

    ctx := context.Background()
    ctx = ctxutil.WithTraceID(ctx, "trace-123")
    ctx = ctxutil.WithRequestID(ctx, "req-456")
    ctx = ctxutil.WithField(ctx, "workflowId", "wf-789")

    logger.WithContext(ctx).Info("Request processed")
    logger.WithContext(ctx).Error("Request failed")
}
```

## Configuration

### Environment-Based Configuration

```go
func setupLogger() logging.Logger {
    env := os.Getenv("ENV")
    
    var config *logging.LoggerConfig
    
    switch env {
    case "development":
        config = &logging.LoggerConfig{
            Level:        logging.DebugLevel,
            OutputTarget: logging.OutputTargetStdout,
            LoggerName:   "api",
            ServiceName:  "user-service",
        }
    case "production":
        config = &logging.LoggerConfig{
            Level:        logging.WarnLevel,
            OutputTarget: logging.OutputTargetStderr,
            LoggerName:   "api",
            ServiceName:  "user-service",
        }
    default:
        config = logging.DefaultLoggerConfig()
    }
    
    return logging.NewLogger(config)
}
```

## Implementation Details

### Zerolog Integration

The package uses [zerolog](https://github.com/rs/zerolog) as the underlying logging implementation, providing:

- **High performance** - Zero allocation logging in most cases
- **Structured output** - JSON and console-friendly formatting
- **Rich context** - Easy field addition and context propagation
- **Level-based filtering** - Efficient level checking and filtering

The zerolog implementation automatically formats output for console readability while maintaining structured data capabilities.

## Custom Implementations

The logging package is designed to support different implementations. You can implement the `Logger` interface to integrate with your organization's preferred logging solution:

```go
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

    // Formatted logging methods
    Debugf(format string, args ...interface{})
    Infof(format string, args ...interface{})
    // ... more methods

    // Structured logging
    WithFields(fields Fields) Logger
    WithField(key string, value interface{}) Logger
    WithError(err error) Logger
    WithContext(ctx context.Context) Logger

    // Clone for isolated contexts
    Clone() Logger
}
```

### Example Custom Implementation

```go
type CustomLogger struct {
    // Your custom implementation
}

func (c *CustomLogger) Info(msg string) {
    // Send to your preferred logging service
    // e.g., logrus, zap, or remote logging service
}

// Implement all other required methods...
```

## Performance Considerations

### Level Checking for Expensive Operations

```go
if logger.IsLevelEnabled(logging.DebugLevel) {
    // Only perform expensive debug operations when debug is enabled
    debugData := expensiveDebugOperation()
    logger.Debugf("Debug data: %v", debugData)
}
```

### Logger Cloning

```go
// Clone loggers for different contexts to avoid field pollution
userLogger := baseLogger.WithField("module", "user")
authLogger := userLogger.Clone().WithField("component", "auth")
```

## Integration Examples

### With HTTP Middleware

```go
func LoggingMiddleware(logger logging.Logger) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            requestLogger := logger.WithFields(logging.Fields{
                "method": r.Method,
                "path":   r.URL.Path,
                "ip":     r.RemoteAddr,
            })
            
            start := time.Now()
            next.ServeHTTP(w, r)
            duration := time.Since(start)
            
            requestLogger.Infow("Request completed",
                "duration_ms", duration.Milliseconds(),
                "status", "200", // You'd capture actual status
            )
        })
    }
}
```

### With Error Handling

```go
func processUser(logger logging.Logger, userID int) error {
    userLogger := logger.WithField("user_id", userID)
    
    user, err := fetchUser(userID)
    if err != nil {
        userLogger.WithError(err).Error("Failed to fetch user")
        return err
    }
    
    userLogger.Info("User processed successfully")
    return nil
}
```

## Thread Safety

All provided implementations are thread-safe and can be used concurrently across goroutines without additional synchronization.

## Testing

The package includes comprehensive tests demonstrating all functionality. Run tests with:

```bash
go test ./logging
```
