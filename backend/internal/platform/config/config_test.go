package config

import (
	"strings"
	"testing"
	"time"
)

func setRuntimeEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_RUNTIME_DSN", "postgres://user:password@localhost:5432/pharmacy")
	t.Setenv("WORKER_OWNER", "worker-test-1")
}

func setMigrationEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("POSTGRES_MIGRATION_DSN", "postgres://migrator:password@localhost:5432/pharmacy")
}

func TestLoadAPILoadsOnlyAPICredentials(t *testing.T) {
	setRuntimeEnvironment(t)
	t.Setenv("POSTGRES_MIGRATION_DSN", "not-a-migration-dsn")
	t.Setenv("AUTH_JWT_ISSUER", "pharmacycrm")
	t.Setenv("AUTH_JWT_AUDIENCE", "pharmacycrm-api")
	t.Setenv("AUTH_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("AUTH_REFRESH_TOKEN_PEPPER", "pepper")
	t.Setenv("PROXY_CORS_ALLOWED_ORIGINS", "http://localhost:5173")
	if _, err := LoadAPI(); err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
}

func TestLoadWorkerDoesNotRequireAuthOrMigrationCredentials(t *testing.T) {
	setRuntimeEnvironment(t)
	t.Setenv("POSTGRES_MIGRATION_DSN", "not-a-migration-dsn")
	t.Setenv("POSTGRES_MAX_CONNECTIONS", "12")
	cfg, err := LoadWorker()
	if err != nil {
		t.Fatalf("LoadWorker() error = %v", err)
	}
	if cfg.RuntimePostgres.DSN == "" {
		t.Fatal("runtime DSN was not loaded")
	}
	if cfg.RuntimePostgres.MaxConnections != 12 {
		t.Fatalf("runtime pool configuration was not loaded: %d", cfg.RuntimePostgres.MaxConnections)
	}
}

func TestLoadMigrationDoesNotRequireRuntimeOrAuthCredentials(t *testing.T) {
	setMigrationEnvironment(t)
	t.Setenv("POSTGRES_RUNTIME_DSN", "not-a-runtime-dsn")
	cfg, err := LoadMigration()
	if err != nil {
		t.Fatalf("LoadMigration() error = %v", err)
	}
	if cfg.MigrationPostgres.DSN == "" {
		t.Fatal("migration DSN was not loaded")
	}
}

func TestLoadAPIAndWorkerRejectUnsupportedWorkerProtocols(t *testing.T) {
	tests := []struct {
		name, appProtocol, workerProtocol string
	}{
		{"both unsupported", "999", "999"},
		{"app unsupported", "999", "1"},
		{"worker unsupported", "1", "999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setRuntimeEnvironment(t)
			t.Setenv("APP_WORKER_PROTOCOL", tt.appProtocol)
			t.Setenv("WORKER_PROTOCOL_VERSION", tt.workerProtocol)
			t.Setenv("AUTH_JWT_ISSUER", "pharmacycrm")
			t.Setenv("AUTH_JWT_AUDIENCE", "pharmacycrm-api")
			t.Setenv("AUTH_JWT_PRIVATE_KEY", "private-key")
			t.Setenv("AUTH_REFRESH_TOKEN_PEPPER", "pepper")
			t.Setenv("PROXY_CORS_ALLOWED_ORIGINS", "http://localhost:5173")
			for _, load := range []struct {
				name string
				run  func() error
			}{
				{"api", func() error { _, err := LoadAPI(); return err }},
				{"worker", func() error { _, err := LoadWorker(); return err }},
			} {
				err := load.run()
				if err == nil {
					t.Fatalf("Load%s() error = nil", load.name)
				}
				if strings.Contains(err.Error(), "999") || strings.Contains(err.Error(), "private-key") {
					t.Fatalf("validation error exposed configuration: %v", err)
				}
			}
		})
	}
}

func validAPIConfig() APIConfig {
	pool := PoolConfig{MaxConnections: 1, AcquireTimeout: time.Second, MaxConnectionLife: time.Second, MaxConnectionIdle: time.Second, HealthCheckPeriod: time.Second, ConnectionCapacity: 1}
	return APIConfig{
		App:             AppConfig{Environment: "development", ServiceName: "pharmacycrm", MinSchemaVersion: 1, MaxSchemaVersion: 1, WorkerProtocol: 1},
		HTTP:            HTTPConfig{Address: ":8080", TLSMode: "disabled", ReadHeaderTimeout: time.Second, ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1, MaxBodyBytes: 1},
		RuntimePostgres: RuntimePostgresConfig{DSN: "postgres://user:password@localhost:5432/pharmacy", PoolConfig: pool},
		Auth:            AuthConfig{JWTIssuer: "pharmacycrm", JWTAudience: "pharmacycrm-api", JWTAlgorithm: "EdDSA", JWTPrivateKey: "private-key", RefreshTokenPepper: "pepper", CookieSameSite: "strict", AccessTokenTTL: time.Second, RefreshAbsoluteTTL: 2 * time.Second, RefreshIdleTTL: time.Second},
		Logging:         LoggingConfig{Level: "info", Format: "console", FilePath: "var/log/pharmacycrm/app.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		Telemetry:       TelemetryConfig{MetricsAddress: ":9090", ExportTimeout: time.Second},
		Worker:          WorkerConfig{ProtocolVersion: 1, Owner: "worker-test-1", Concurrency: 1, PollInterval: time.Second, LeaseDuration: time.Second, MaxClaim: 1, DrainTimeout: time.Second},
		Storage:         StorageConfig{ImportRoot: "var/imports", MaxUploadBytes: 1, Retention: time.Second},
	}
}

func TestValidateAPIRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name   string
		change func(*APIConfig)
	}{
		{"schema fail open", func(c *APIConfig) { c.App.MinSchemaVersion, c.App.MaxSchemaVersion = 0, 0 }},
		{"invalid dsn", func(c *APIConfig) { c.RuntimePostgres.DSN = "not-a-dsn" }},
		{"unsupported direct tls", func(c *APIConfig) { c.HTTP.TLSMode = "direct" }},
		{"cors wildcard credentials", func(c *APIConfig) { c.ProxyCORS.AllowedOrigins = CSV{"*"}; c.ProxyCORS.AllowCredentials = true }},
		{"invalid pool", func(c *APIConfig) { c.RuntimePostgres.MinConnections = 2; c.RuntimePostgres.MaxConnections = 1 }},
		{"empty worker owner", func(c *APIConfig) { c.Worker.Owner = "" }},
		{"oversized claim", func(c *APIConfig) { c.Worker.MaxClaim = 101 }},
		{"drain exceeds process shutdown", func(c *APIConfig) { c.Worker.DrainTimeout = 21 * time.Second }},
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

func TestValidationDoesNotExposeSecrets(t *testing.T) {
	cfg := validAPIConfig()
	cfg.RuntimePostgres.DSN = "postgres://user:super-secret@localhost:5432/pharmacy"
	cfg.RuntimePostgres.MaxConnections = 0
	err := validateAPI(cfg)
	if err == nil {
		t.Fatal("validateAPI() error = nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
}
