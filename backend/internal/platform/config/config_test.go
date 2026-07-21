package config

import (
	"strings"
	"testing"
)

func TestLoadReadsEnvironmentConfiguration(t *testing.T) {
	t.Setenv("POSTGRES_RUNTIME_DSN", "postgres://user:password@localhost:5432/pharmacy")
	t.Setenv("POSTGRES_MIGRATION_DSN", "postgres://migrator:password@localhost:5432/pharmacy")
	t.Setenv("AUTH_JWT_ISSUER", "pharmacycrm")
	t.Setenv("AUTH_JWT_AUDIENCE", "pharmacycrm-api")
	t.Setenv("AUTH_JWT_PRIVATE_KEY", "private-key")
	t.Setenv("AUTH_REFRESH_TOKEN_PEPPER", "pepper")
	t.Setenv("PROXY_CORS_ALLOWED_ORIGINS", "http://localhost:5173")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.App.Environment != "development" || cfg.Postgres.MaxConnections != 10 {
		t.Fatalf("Load() returned unexpected defaults: %#v", cfg)
	}
}

func validConfig() Config {
	return Config{
		App:       AppConfig{Environment: "development", ServiceName: "pharmacycrm", MinSchemaVersion: 0, MaxSchemaVersion: 0, WorkerProtocol: 1},
		HTTP:      HTTPConfig{Address: ":8080", TLSMode: "disabled", ReadHeaderTimeout: 1, ReadTimeout: 1, WriteTimeout: 1, IdleTimeout: 1, ShutdownTimeout: 1, MaxHeaderBytes: 1, MaxBodyBytes: 1},
		Postgres:  PostgresConfig{RuntimeDSN: "postgres://user:password@localhost:5432/pharmacy", MigrationDSN: "postgres://migrator:password@localhost:5432/pharmacy", MaxConnections: 1, AcquireTimeout: 1, MaxConnectionLife: 1, MaxConnectionIdle: 1, HealthCheckPeriod: 1, ConnectionCapacity: 1},
		Auth:      AuthConfig{JWTIssuer: "pharmacycrm", JWTAudience: "pharmacycrm-api", JWTAlgorithm: "EdDSA", JWTPrivateKey: "private-key", RefreshTokenPepper: "pepper", CookieSameSite: "strict", AccessTokenTTL: 1, RefreshAbsoluteTTL: 2, RefreshIdleTTL: 1},
		Logging:   LoggingConfig{Level: "info", Format: "console", FilePath: "var/log/pharmacycrm/app.log", MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1},
		Telemetry: TelemetryConfig{MetricsAddress: ":9090", ExportTimeout: 1},
		Worker:    WorkerConfig{ProtocolVersion: 1, Concurrency: 1, PollInterval: 1, LeaseDuration: 1, MaxClaim: 1},
		Storage:   StorageConfig{ImportRoot: "var/imports", MaxUploadBytes: 1, Retention: 1},
	}
}

func TestValidateAcceptsSafeDevelopmentConfiguration(t *testing.T) {
	if err := validConfig().Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateRejectsUnsafeConfigurations(t *testing.T) {
	tests := []struct {
		name   string
		change func(*Config)
	}{
		{"production debug", func(c *Config) {
			c.App.Environment = "production"
			c.App.Debug = true
			c.HTTP.TLSMode = "terminated"
			c.Auth.CookieSecure = true
			c.Logging.Format = "json"
		}},
		{"invalid dsn", func(c *Config) { c.Postgres.RuntimeDSN = "not-a-dsn" }},
		{"unsupported direct tls", func(c *Config) { c.HTTP.TLSMode = "direct" }},
		{"unsafe production cookie", func(c *Config) {
			c.App.Environment = "production"
			c.HTTP.TLSMode = "terminated"
			c.Logging.Format = "json"
		}},
		{"cors wildcard credentials", func(c *Config) { c.ProxyCORS.AllowedOrigins = CSV{"*"}; c.ProxyCORS.AllowCredentials = true }},
		{"invalid pool", func(c *Config) { c.Postgres.MinConnections = 2; c.Postgres.MaxConnections = 1 }},
		{"incompatible protocol", func(c *Config) { c.Worker.ProtocolVersion = 2 }},
		{"invalid log path", func(c *Config) { c.Logging.FilePath = "." }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			tt.change(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil")
			}
		})
	}
}

func TestValidationDoesNotExposeSecrets(t *testing.T) {
	cfg := validConfig()
	cfg.Postgres.RuntimeDSN = "postgres://user:super-secret@localhost:5432/pharmacy"
	cfg.Postgres.MaxConnections = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked secret: %v", err)
	}
}
