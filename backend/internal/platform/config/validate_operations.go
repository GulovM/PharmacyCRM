package config

import (
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/contracts"
)

const workerProcessShutdownTimeout = 20 * time.Second

func validateLogging(c LoggingConfig, environment string) error {
	if c.Level != "debug" && c.Level != "info" && c.Level != "warn" && c.Level != "error" {
		return invalid("logging level is invalid")
	}
	if c.Format != "console" && c.Format != "json" {
		return invalid("logging format is invalid")
	}
	if environment == productionEnvironment && (c.Level == "debug" || c.Format != "json") {
		return invalid("production logging must use non-debug json output")
	}
	if c.FilePath == "" || filepath.Clean(c.FilePath) == "." || strings.HasSuffix(c.FilePath, string(filepath.Separator)) {
		return invalid("logging file path is invalid")
	}
	if c.MaxSizeMB < 1 || c.MaxBackups < 1 || c.MaxAgeDays < 1 {
		return invalid("logging rotation settings must be positive")
	}
	return nil
}
func validateTelemetry(c TelemetryConfig) error {
	if _, _, err := net.SplitHostPort(c.MetricsAddress); err != nil {
		return invalid("telemetry metrics address is invalid")
	}
	if c.TracingEndpoint != "" {
		if u, err := url.ParseRequestURI(c.TracingEndpoint); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("telemetry tracing endpoint is invalid")
		}
	}
	if c.ExportTimeout <= 0 {
		return invalid("telemetry export timeout must be positive")
	}
	return nil
}
func validateWorker(c WorkerConfig, expectedProtocol int) error {
	if c.ProtocolVersion != SupportedWorkerProtocol {
		return invalid("worker protocol is unsupported")
	}
	if c.ProtocolVersion != expectedProtocol {
		return invalid("worker protocol is incompatible with application protocol")
	}
	if strings.TrimSpace(c.Owner) == "" || c.Owner != strings.TrimSpace(c.Owner) || len(c.Owner) > contracts.MaxWorkerOwnerLength {
		return invalid("worker owner is invalid")
	}
	if c.Concurrency < 1 || c.MaxClaim < 1 || c.MaxClaim > 100 || c.PollInterval <= 0 || c.LeaseDuration <= 0 || c.DrainTimeout <= 0 || c.DrainTimeout > workerProcessShutdownTimeout || c.RetentionInterval <= 0 || c.RetentionBatchSize < 1 || c.RetentionBatchSize > 1000 || c.RetentionMaxBatches < 1 || c.RetentionMaxDuration <= 0 || c.ProcessedRetention != 30*24*time.Hour || c.DeadLetterRetention != 180*24*time.Hour {
		return invalid("worker settings are invalid")
	}
	return nil
}
func validateStorage(c StorageConfig) error {
	if c.ImportRoot == "" || filepath.Clean(c.ImportRoot) == "." || c.MaxUploadBytes <= 0 || c.Retention <= 0 {
		return invalid("import storage settings are invalid")
	}
	return nil
}
