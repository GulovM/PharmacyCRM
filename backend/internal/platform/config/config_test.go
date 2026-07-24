package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/shared/contracts"
)

var workerEnvironmentKeys = []string{
	"WORKER_OWNER",
	"WORKER_CONCURRENCY",
	"WORKER_POLL_INTERVAL",
	"WORKER_LEASE_DURATION",
	"WORKER_MAX_CLAIM",
	"WORKER_DRAIN_TIMEOUT",
	"WORKER_RETENTION_INTERVAL",
	"WORKER_RETENTION_BATCH_SIZE",
	"WORKER_RETENTION_MAX_BATCHES_PER_CYCLE",
	"WORKER_RETENTION_MAX_CYCLE_DURATION",
	"WORKER_PROCESSED_RETENTION",
	"WORKER_DEAD_LETTER_RETENTION",
	"WORKER_PROTOCOL_VERSION",
}

func unsetEnvironment(t testing.TB, key string) {
	t.Helper()
	value, exists := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if exists {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func setAPIEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_API_RUNTIME_DSN", "postgres://api:password@localhost:5432/pharmacy")
	t.Setenv("AUTH_JWT_ISSUER", "pharmacycrm")
	t.Setenv("AUTH_JWT_AUDIENCE", "pharmacycrm-api")
	t.Setenv("AUTH_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("AUTH_REFRESH_TOKEN_PEPPER", "pepper")
	t.Setenv("PROXY_CORS_ALLOWED_ORIGINS", "http://localhost:5173")
}

func setWorkerEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_WORKER_RUNTIME_DSN", "postgres://worker:password@localhost:5432/pharmacy")
	t.Setenv("WORKER_OWNER", "worker-test-1")
}

func setRuntimeEnvironment(t *testing.T) {
	t.Helper()
	setAPIEnvironment(t)
	setWorkerEnvironment(t)
}

func setMigrationEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_MIGRATION_DSN", "postgres://migrator:password@localhost:5432/pharmacy")
}

func TestLoadAPILoadsOnlyAPICredentials(t *testing.T) {
	setAPIEnvironment(t)
	t.Setenv("POSTGRES_WORKER_RUNTIME_DSN", "not-a-worker-runtime-dsn")
	t.Setenv("POSTGRES_MIGRATION_DSN", "not-a-migration-dsn")
	if _, err := LoadAPI(); err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestLoadAPIDoesNotRequireWorkerConfiguration(t *testing.T) {
	setAPIEnvironment(t)
	for _, key := range workerEnvironmentKeys {
		unsetEnvironment(t, key)
	}
	if _, err := LoadAPI(); err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestInvalidWorkerConfigurationDoesNotAffectAPI(t *testing.T) {
	setAPIEnvironment(t)
	t.Setenv("WORKER_OWNER", "")
	t.Setenv("WORKER_MAX_CLAIM", "99999")
	t.Setenv("WORKER_DRAIN_TIMEOUT", "-1s")
	t.Setenv("WORKER_RETENTION_INTERVAL", "0s")
	if _, err := LoadAPI(); err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestLoadWorkerDoesNotRequireAuthOrMigrationCredentials(t *testing.T) {
	setWorkerEnvironment(t)
	t.Setenv("POSTGRES_MIGRATION_DSN", "not-a-migration-dsn")
	t.Setenv("POSTGRES_MAX_CONNECTIONS", "12")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker() error = %v", err)
	}
	if cfg.WorkerPostgres.DSN == "" {
		t.Fatal("worker runtime DSN was not loaded")
	}
	if cfg.WorkerPostgres.MaxConnections != 12 {
		t.Fatalf("worker pool configuration was not loaded: %d", cfg.WorkerPostgres.MaxConnections)
	}
}

func TestWorkerOwnerLengthMatchesDatabaseContract(t *testing.T) {
	setWorkerEnvironment(t)
	t.Setenv("WORKER_OWNER", strings.Repeat("w", contracts.MaxWorkerOwnerLength))
	if _, err := LoadWorker(); err != nil {
		t.Fatalf("maximum worker owner length rejected: %v", err)
	}
	t.Setenv("WORKER_OWNER", strings.Repeat("w", contracts.MaxWorkerOwnerLength+1))
	if _, err := LoadWorker(); err == nil {
		t.Fatal("oversized worker owner was accepted")
	}
}

func TestLoadWorkerStillRequiresWorkerConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T)
	}{
		{"empty owner", func(t *testing.T) { t.Setenv("WORKER_OWNER", "") }},
		{"invalid batch size", func(t *testing.T) { t.Setenv("WORKER_MAX_CLAIM", "101") }},
		{"invalid drain timeout", func(t *testing.T) { t.Setenv("WORKER_DRAIN_TIMEOUT", "-1s") }},
		{"invalid retention interval", func(t *testing.T) { t.Setenv("WORKER_RETENTION_INTERVAL", "0s") }},
		{"invalid processed retention", func(t *testing.T) { t.Setenv("WORKER_PROCESSED_RETENTION", "1h") }},
		{"unsupported protocol", func(t *testing.T) { t.Setenv("WORKER_PROTOCOL_VERSION", "999") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWorkerEnvironment(t)
			tt.change(t)
			if _, err := LoadWorker(); err == nil {
				t.Fatal("LoadWorker() error = nil")
			}
		})
	}
}

func TestLoadMigrationDoesNotRequireRuntimeOrAuthCredentials(t *testing.T) {
	setMigrationEnvironment(t)
	t.Setenv("POSTGRES_API_RUNTIME_DSN", "not-an-api-runtime-dsn")
	t.Setenv("POSTGRES_WORKER_RUNTIME_DSN", "not-a-worker-runtime-dsn")
	cfg, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration() error = %v", err)
	}
	if cfg.MigrationPostgres.DSN == "" {
		t.Fatal("migration DSN was not loaded")
	}
}

func TestSchemaDefaultsMatchSupportedVersion(t *testing.T) {
	defaults := AppConfig{MinSchemaVersion: 25, MaxSchemaVersion: 25}
	if defaults.MinSchemaVersion != SupportedSchemaVersion || defaults.MaxSchemaVersion != SupportedSchemaVersion {
		t.Fatalf("schema defaults must match supported version %d", SupportedSchemaVersion)
	}
}

func TestLoadAPIDefaultsToSupportedSchemaVersion(t *testing.T) {
	setAPIEnvironment(t)
	unsetEnvironment(t, "APP_MIN_SCHEMA_VERSION")
	unsetEnvironment(t, "APP_MAX_SCHEMA_VERSION")
	cfg, err := LoadAPI()
	if err != nil || cfg.App.MinSchemaVersion != SupportedSchemaVersion || cfg.App.MaxSchemaVersion != SupportedSchemaVersion {
		t.Fatalf("defaults=%d..%d err=%v", cfg.App.MinSchemaVersion, cfg.App.MaxSchemaVersion, err)
	}
}

func TestLoadWorkerDefaultsToSupportedSchemaVersion(t *testing.T) {
	setWorkerEnvironment(t)
	unsetEnvironment(t, "APP_MIN_SCHEMA_VERSION")
	unsetEnvironment(t, "APP_MAX_SCHEMA_VERSION")
	cfg, err := LoadWorker()
	if err != nil || cfg.App.MinSchemaVersion != SupportedSchemaVersion || cfg.App.MaxSchemaVersion != SupportedSchemaVersion {
		t.Fatalf("defaults=%d..%d err=%v", cfg.App.MinSchemaVersion, cfg.App.MaxSchemaVersion, err)
	}
}

func TestLoadMigrationDefaultsToSupportedSchemaVersion(t *testing.T) {
	setMigrationEnvironment(t)
	unsetEnvironment(t, "APP_MIN_SCHEMA_VERSION")
	unsetEnvironment(t, "APP_MAX_SCHEMA_VERSION")
	cfg, err := LoadMigration()
	if err != nil || cfg.App.MinSchemaVersion != SupportedSchemaVersion || cfg.App.MaxSchemaVersion != SupportedSchemaVersion {
		t.Fatalf("defaults=%d..%d err=%v", cfg.App.MinSchemaVersion, cfg.App.MaxSchemaVersion, err)
	}
}

func TestLoadAPIRejectsUnsupportedApplicationWorkerProtocol(t *testing.T) {
	setAPIEnvironment(t)
	t.Setenv("APP_WORKER_PROTOCOL", "999")
	if _, err := LoadAPI(); err == nil {
		t.Fatal("LoadAPI() error = nil")
	}
}

func TestLoadWorkerRejectsUnsupportedWorkerProtocols(t *testing.T) {
	tests := []struct {
		name, appProtocol, workerProtocol string
	}{
		{"both unsupported", "999", "999"},
		{"app unsupported", "999", "1"},
		{"worker unsupported", "1", "999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setWorkerEnvironment(t)
			t.Setenv("APP_WORKER_PROTOCOL", tt.appProtocol)
			t.Setenv("WORKER_PROTOCOL_VERSION", tt.workerProtocol)
			err := func() error { _, err := LoadWorker(); return err }()
			if err == nil {
				t.Fatal("LoadWorker() error = nil")
			}
		})
	}
}

func validPoolConfig() PoolConfig {
	return PoolConfig{
		MaxConnections: 1, AcquireTimeout: time.Second, MaxConnectionLife: time.Second,
		MaxConnectionIdle: time.Second, HealthCheckPeriod: time.Second, ConnectionCapacity: 1,
	}
}

func validAPIConfig() APIConfig {
	return APIConfig{
		App:         AppConfig{Environment: "development", ServiceName: "pharmacycrm", MinSchemaVersion: 1, MaxSchemaVersion: 1, WorkerProtocol: 1},
		HTTP:        HTTPConfig{Address: ":8080", TLSMode: "disabled", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1, MaxBodyBytes: 1},
		APIPostgres: APIPostgresConfig{DSN: "postgres://user:password@localhost:5432/pharmacy", PoolConfig: validPoolConfig()},
		Auth:        AuthConfig{JWTIssuer: "pharmacycrm", JWTAudience: "pharmacycrm-api", JWTAlgorithm: "EdDSA", JWTPrivateKey: "private-key", RefreshTokenPepper: "pepper", CookieSameSite: "strict", AccessTokenTTL: time.Second, RefreshAbsoluteTTL: 2 * time.Second, RefreshIdleTTL: time.Second},
		Logging:     LoggingConfig{Level: "info", Format: "console", FilePath: "var/log/pharmacycrm/app.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		Telemetry:   TelemetryConfig{MetricsAddress: ":9090", ExportTimeout: time.Second},
		Storage:     StorageConfig{ImportRoot: "var/imports", MaxUploadBytes: 1, Retention: time.Second},
	}
}

func validWorkerProcessConfig() WorkerProcessConfig {
	return WorkerProcessConfig{
		App:            AppConfig{Environment: "development", ServiceName: "pharmacycrm", MinSchemaVersion: 1, MaxSchemaVersion: 1, WorkerProtocol: 1},
		WorkerPostgres: WorkerPostgresConfig{DSN: "postgres://worker:password@localhost:5432/pharmacy", PoolConfig: validPoolConfig()},
		Logging:        LoggingConfig{Level: "info", Format: "console", FilePath: "var/log/pharmacycrm/worker.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		Telemetry:      TelemetryConfig{MetricsAddress: ":9091", ExportTimeout: time.Second},
		Worker: WorkerConfig{
			ProtocolVersion: 1, Owner: "worker-test-1", Concurrency: 1, PollInterval: time.Second,
			LeaseDuration: time.Second, MaxClaim: 1, DrainTimeout: time.Second,
			RetentionInterval: time.Hour, RetentionBatchSize: 100, RetentionMaxBatches: 10,
			RetentionMaxDuration: 30 * time.Second, ProcessedRetention: 30 * 24 * time.Hour,
			DeadLetterRetention: 180 * 24 * time.Hour,
		},
		Storage: StorageConfig{ImportRoot: "var/imports", MaxUploadBytes: 1, Retention: time.Second},
	}
}

func TestValidateAPIRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*APIConfig)
	}{
		{"schema fail open", func(c *APIConfig) { c.App.MinSchemaVersion, c.App.MaxSchemaVersion = 0, 0 }},
		{"invalid dsn", func(c *APIConfig) { c.APIPostgres.DSN = "not-a-dsn" }},
		{"unsupported direct tls", func(c *APIConfig) { c.HTTP.TLSMode = "direct" }},
		{"cors wildcard credentials", func(c *APIConfig) { c.ProxyCORS.AllowedOrigins = CSV{"*"}; c.ProxyCORS.AllowCredentials = true }},
		{"invalid pool", func(c *APIConfig) { c.APIPostgres.MinConnections = 2; c.APIPostgres.MaxConnections = 1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validAPIConfig()
			tt.change(&cfg)
			if err := validateAPI(cfg); err == nil {
				t.Fatal("validateAPI() error = nil")
			}
		})
	}
}

func TestValidateWorkerProcessRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*WorkerProcessConfig)
	}{
		{"empty worker owner", func(c *WorkerProcessConfig) { c.Worker.Owner = "" }},
		{"oversized claim", func(c *WorkerProcessConfig) { c.Worker.MaxClaim = 101 }},
		{"invalid drain timeout", func(c *WorkerProcessConfig) { c.Worker.DrainTimeout = 0 }},
		{"oversized retention batch", func(c *WorkerProcessConfig) { c.Worker.RetentionBatchSize = 1001 }},
		{"short processed retention", func(c *WorkerProcessConfig) { c.Worker.ProcessedRetention = 29 * 24 * time.Hour }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validWorkerProcessConfig()
			tt.change(&cfg)
			if err := validateWorkerProcess(cfg); err == nil {
				t.Fatal("validateWorkerProcess() error = nil")
			}
		})
	}
}

func TestValidationDoesNotExposeSecrets(t *testing.T) {
	cfg := validAPIConfig()
	cfg.APIPostgres.DSN = "postgres://user:super-secret@localhost:5432/pharmacy"
	cfg.APIPostgres.MaxConnections = 0
	err := validateAPI(cfg)
	if err == nil {
		t.Fatal("validateAPI() error = nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
}
