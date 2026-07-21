// Package logging creates the backend's structured technical logger.
package logging

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"gopkg.in/natefinch/lumberjack.v2"
)

const eventSchemaVersion = "1"

var fileSinkFailureAlert = []byte("ERROR file_log_sink_failed: terminal fallback active\n")

// Logger owns the process logger and exposes file-sink health for the metrics
// and alerting integration added in a later observability slice.
type Logger struct {
	*zap.Logger
	fileSink *resilientFileSink
	file     io.Closer
}

// New validates that the configured file can be opened before process startup,
// then writes every record to both the terminal and a rotating JSON file.
func New(loggingConfig config.LoggingConfig, appConfig config.AppConfig) (*Logger, error) {
	return newLogger(loggingConfig, appConfig, os.Stdout, os.Stderr)
}

func newLogger(loggingConfig config.LoggingConfig, appConfig config.AppConfig, stdout, stderr io.Writer) (*Logger, error) {
	if err := probeFile(loggingConfig.FilePath); err != nil {
		return nil, fmt.Errorf("initialize file logger: log path is unavailable")
	}

	var level zapcore.Level
	if err := level.UnmarshalText([]byte(loggingConfig.Level)); err != nil {
		return nil, fmt.Errorf("initialize logger: invalid log level")
	}

	terminalEncoder, err := encoder(loggingConfig.Format)
	if err != nil {
		return nil, fmt.Errorf("initialize logger: invalid terminal format")
	}
	fileEncoder, err := encoder("json")
	if err != nil {
		return nil, fmt.Errorf("initialize logger: invalid file format")
	}

	terminalSink := zapcore.Lock(zapcore.AddSync(stdout))
	rotatingFile := &lumberjack.Logger{
		Filename:   loggingConfig.FilePath,
		MaxSize:    loggingConfig.MaxSizeMB,
		MaxBackups: loggingConfig.MaxBackups,
		MaxAge:     loggingConfig.MaxAgeDays,
		Compress:   loggingConfig.Compress,
	}
	fileSink := &resilientFileSink{
		primary:  zapcore.AddSync(rotatingFile),
		fallback: terminalSink,
	}

	core := zapcore.NewTee(
		zapcore.NewCore(terminalEncoder, terminalSink, level),
		zapcore.NewCore(fileEncoder, fileSink, level),
	)
	base := zap.New(core, zap.ErrorOutput(zapcore.Lock(zapcore.AddSync(stderr))))
	base = base.With(
		zap.String("service", appConfig.ServiceName),
		zap.String("environment", appConfig.Environment),
		zap.String("version", appConfig.Version),
		zap.String("commit_sha", appConfig.CommitSHA),
		zap.Int("schema_min_version", appConfig.MinSchemaVersion),
		zap.Int("schema_max_version", appConfig.MaxSchemaVersion),
		zap.Int("worker_protocol_version", appConfig.WorkerProtocol),
		zap.String("event_schema_version", eventSchemaVersion),
	)

	return &Logger{Logger: base, fileSink: fileSink, file: rotatingFile}, nil
}

// Sync flushes all logger sinks. Errors caused by unsynchronizable terminal
// descriptors are expected on Windows and Unix terminals and are ignored; all
// other failures are returned to the caller for explicit handling.
func (l *Logger) Sync() error {
	err := l.Logger.Sync()
	if isIgnorableSyncError(err) {
		return nil
	}
	return err
}

// Close flushes logger sinks and releases the rotating log file. It is the
// shutdown hook used by every process entry point.
func (l *Logger) Close() error {
	syncErr := l.Sync()
	closeErr := l.file.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

// FileSinkFailureCount is an observable counter for runtime failures of the
// rotating file sink. The terminal core remains active when this value is nonzero.
func (l *Logger) FileSinkFailureCount() uint64 {
	return l.fileSink.failures.Load()
}

func encoder(format string) (zapcore.Encoder, error) {
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.LevelKey = "level"
	encoderConfig.MessageKey = "message"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	encoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	if format == "json" {
		return zapcore.NewJSONEncoder(encoderConfig), nil
	}
	if format == "console" {
		return zapcore.NewConsoleEncoder(encoderConfig), nil
	}
	return nil, fmt.Errorf("unsupported format")
}

func probeFile(path string) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	return file.Close()
}

type resilientFileSink struct {
	primary  zapcore.WriteSyncer
	fallback zapcore.WriteSyncer
	failures atomic.Uint64
}

func (s *resilientFileSink) Write(record []byte) (int, error) {
	n, err := s.primary.Write(record)
	if err == nil {
		return n, nil
	}

	s.failures.Add(1)
	if _, fallbackErr := s.fallback.Write(record); fallbackErr == nil {
		// The terminal core has already received the record. This second write is
		// deliberate: it makes the file-sink outage visible to operators. The
		// stable alert text is also suitable for an alert rule until metrics are
		// exported by the observability slice.
		_, _ = s.fallback.Write(fileSinkFailureAlert)
		return len(record), nil
	}
	return n, err
}

func (s *resilientFileSink) Sync() error {
	if err := s.primary.Sync(); err != nil {
		s.failures.Add(1)
		return err
	}
	return nil
}

func isIgnorableSyncError(err error) bool {
	return errors.Is(err, syscall.EINVAL) ||
		errors.Is(err, syscall.ENOTTY) ||
		strings.Contains(strings.ToLower(errorString(err)), "inappropriate ioctl")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
