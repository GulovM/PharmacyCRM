// Package config loads and validates process-specific configuration contracts.
package config

import (
	"fmt"
	"strings"
	"time"
)

const (
	productionEnvironment   = "production"
	approvedJWTAlgorithm    = "EdDSA"
	SupportedSchemaVersion  = 14
	SupportedWorkerProtocol = 1
)

type AppConfig struct {
	Environment      string `envconfig:"ENVIRONMENT" default:"development"`
	ServiceName      string `envconfig:"SERVICE_NAME" default:"pharmacycrm"`
	Version          string `default:"dev"`
	CommitSHA        string `envconfig:"COMMIT_SHA" default:"unknown"`
	Debug            bool   `default:"false"`
	MinSchemaVersion int    `envconfig:"MIN_SCHEMA_VERSION" default:"14"`
	MaxSchemaVersion int    `envconfig:"MAX_SCHEMA_VERSION" default:"14"`
	WorkerProtocol   int    `envconfig:"WORKER_PROTOCOL" default:"1"`
}

type HTTPConfig struct {
	Address           string        `default:":8080"`
	TLSMode           string        `envconfig:"TLS_MODE" default:"disabled"`
	ReadHeaderTimeout time.Duration `envconfig:"READ_HEADER_TIMEOUT" default:"5s"`
	ReadTimeout       time.Duration `envconfig:"READ_TIMEOUT" default:"15s"`
	WriteTimeout      time.Duration `envconfig:"WRITE_TIMEOUT" default:"30s"`
	IdleTimeout       time.Duration `envconfig:"IDLE_TIMEOUT" default:"60s"`
	ShutdownTimeout   time.Duration `envconfig:"SHUTDOWN_TIMEOUT" default:"20s"`
	MaxHeaderBytes    int           `envconfig:"MAX_HEADER_BYTES" default:"1048576"`
	MaxBodyBytes      int64         `envconfig:"MAX_BODY_BYTES" default:"1048576"`
}

type PoolConfig struct {
	MinConnections     int32         `envconfig:"MIN_CONNECTIONS" default:"1"`
	MaxConnections     int32         `envconfig:"MAX_CONNECTIONS" default:"10"`
	AcquireTimeout     time.Duration `envconfig:"ACQUIRE_TIMEOUT" default:"5s"`
	MaxConnectionLife  time.Duration `envconfig:"MAX_CONNECTION_LIFETIME" default:"30m"`
	MaxConnectionIdle  time.Duration `envconfig:"MAX_CONNECTION_IDLE_TIME" default:"5m"`
	HealthCheckPeriod  time.Duration `envconfig:"HEALTH_CHECK_PERIOD" default:"30s"`
	ConnectionCapacity int32         `envconfig:"CONNECTION_CAPACITY" default:"20"`
}

type RuntimePostgresConfig struct {
	DSN string `envconfig:"RUNTIME_DSN" required:"true"`
	PoolConfig
}

type MigrationPostgresConfig struct {
	DSN string `envconfig:"MIGRATION_DSN" required:"true"`
	PoolConfig
}

type AuthConfig struct {
	JWTIssuer          string        `envconfig:"JWT_ISSUER" required:"true"`
	JWTAudience        string        `envconfig:"JWT_AUDIENCE" required:"true"`
	JWTAlgorithm       string        `envconfig:"JWT_ALGORITHM" default:"EdDSA"`
	JWTPrivateKey      string        `envconfig:"JWT_PRIVATE_KEY" required:"true"`
	RefreshTokenPepper string        `envconfig:"REFRESH_TOKEN_PEPPER" required:"true"`
	CookieSecure       bool          `envconfig:"COOKIE_SECURE" default:"false"`
	CookieSameSite     string        `envconfig:"COOKIE_SAME_SITE" default:"strict"`
	AccessTokenTTL     time.Duration `envconfig:"ACCESS_TOKEN_TTL" default:"10m"`
	RefreshAbsoluteTTL time.Duration `envconfig:"REFRESH_ABSOLUTE_TTL" default:"720h"`
	RefreshIdleTTL     time.Duration `envconfig:"REFRESH_IDLE_TTL" default:"168h"`
	ClockSkew          time.Duration `envconfig:"CLOCK_SKEW" default:"30s"`
}

type ProxyCORSConfig struct {
	TrustForwardedHeaders bool `envconfig:"TRUST_FORWARDED_HEADERS" default:"false"`
	TrustedProxyCIDRs     CSV  `envconfig:"TRUSTED_PROXY_CIDRS"`
	AllowedOrigins        CSV  `envconfig:"ALLOWED_ORIGINS"`
	AllowCredentials      bool `envconfig:"ALLOW_CREDENTIALS" default:"true"`
}

type LoggingConfig struct {
	Level      string `default:"info"`
	Format     string `default:"console"`
	FilePath   string `envconfig:"FILE_PATH" default:"var/log/pharmacycrm/app.log"`
	MaxSizeMB  int    `envconfig:"MAX_SIZE_MB" default:"100"`
	MaxBackups int    `envconfig:"MAX_BACKUPS" default:"10"`
	MaxAgeDays int    `envconfig:"MAX_AGE_DAYS" default:"30"`
	Compress   bool   `default:"true"`
}

type TelemetryConfig struct {
	TracingEndpoint string        `envconfig:"TRACING_ENDPOINT"`
	MetricsAddress  string        `envconfig:"METRICS_ADDRESS" default:":9090"`
	ExportTimeout   time.Duration `envconfig:"EXPORT_TIMEOUT" default:"5s"`
}

type WorkerConfig struct {
	ProtocolVersion int           `envconfig:"PROTOCOL_VERSION" default:"1"`
	Owner           string        `required:"true"`
	Concurrency     int           `default:"1"`
	PollInterval    time.Duration `envconfig:"POLL_INTERVAL" default:"1s"`
	LeaseDuration   time.Duration `envconfig:"LEASE_DURATION" default:"30s"`
	MaxClaim        int           `envconfig:"MAX_CLAIM" default:"100"`
	DrainTimeout    time.Duration `envconfig:"DRAIN_TIMEOUT" default:"20s"`
}

type StorageConfig struct {
	ImportRoot     string        `envconfig:"IMPORT_ROOT" default:"var/imports"`
	MaxUploadBytes int64         `envconfig:"MAX_UPLOAD_BYTES" default:"10485760"`
	Retention      time.Duration `envconfig:"RETENTION" default:"720h"`
}

// APIConfig deliberately has no migration credentials.
type APIConfig struct {
	App             AppConfig
	HTTP            HTTPConfig
	RuntimePostgres RuntimePostgresConfig
	Auth            AuthConfig
	ProxyCORS       ProxyCORSConfig
	Logging         LoggingConfig
	Telemetry       TelemetryConfig
	Worker          WorkerConfig
	Storage         StorageConfig
}

// WorkerProcessConfig deliberately has neither migration nor auth credentials.
type WorkerProcessConfig struct {
	App             AppConfig
	RuntimePostgres RuntimePostgresConfig
	Logging         LoggingConfig
	Telemetry       TelemetryConfig
	Worker          WorkerConfig
	Storage         StorageConfig
}

// MigrationConfig deliberately has neither runtime database nor auth credentials.
type MigrationConfig struct {
	App               AppConfig
	MigrationPostgres MigrationPostgresConfig
	Logging           LoggingConfig
}

type CSV []string

func (v *CSV) Decode(value string) error {
	if strings.TrimSpace(value) == "" {
		*v = nil
		return nil
	}
	parts := strings.Split(value, ",")
	parsed := make(CSV, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("contains an empty entry")
		}
		parsed = append(parsed, part)
	}
	*v = parsed
	return nil
}
