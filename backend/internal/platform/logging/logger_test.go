package logging

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func testConfig(path string) (config.LoggingConfig, config.AppConfig) {
	return config.LoggingConfig{
			Level: "info", Format: "console", FilePath: path,
			MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1, Compress: true,
		}, config.AppConfig{
			Environment: "development", ServiceName: "pharmacycrm", Version: "test",
			CommitSHA: "abcdef", MinSchemaVersion: 0, MaxSchemaVersion: 0, WorkerProtocol: 1,
		}
}

func TestNewWritesStructuredRecordToTerminalAndFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "app.log")
	loggingConfig, appConfig := testConfig(path)
	var terminal bytes.Buffer
	logger, err := newLogger(loggingConfig, appConfig, &terminal, &terminal)
	if err != nil {
		t.Fatalf("newLogger() error = %v", err)
	}
	logger.Info("process.started", zap.String("component", "api"))
	if err := logger.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	fileContent, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var record map[string]any
	if err := json.Unmarshal(fileContent, &record); err != nil {
		t.Fatalf("file record is not JSON: %v", err)
	}
	for key, want := range map[string]string{"message": "process.started", "service": "pharmacycrm", "environment": "development", "event_schema_version": "1"} {
		if got := record[key]; got != want {
			t.Errorf("record[%q] = %v, want %q", key, got, want)
		}
	}
	if terminal.Len() == 0 {
		t.Fatal("terminal output is empty")
	}
}

func TestNewFailsWhenLogPathCannotBeCreated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	loggingConfig, appConfig := testConfig(filepath.Join(file, "app.log"))
	if _, err := newLogger(loggingConfig, appConfig, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("newLogger() error = nil")
	}
}

func TestResilientFileSinkFallsBackToTerminal(t *testing.T) {
	var fallback bytes.Buffer
	sink := &resilientFileSink{primary: failingSink{}, fallback: zapcore.AddSync(&fallback)}
	if _, err := sink.Write([]byte("record")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if got := sink.failures.Load(); got != 1 {
		t.Errorf("failures = %d, want 1", got)
	}
	if got := fallback.String(); !strings.Contains(got, "record") || !strings.Contains(got, "file_log_sink_failed") {
		t.Errorf("fallback = %q, want record and failure alert", got)
	}
}

type failingSink struct{}

func (failingSink) Write([]byte) (int, error) { return 0, os.ErrPermission }
func (failingSink) Sync() error               { return os.ErrPermission }
