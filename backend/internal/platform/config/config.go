// Package config loads and validates process configuration.
package config

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

const (
	productionEnvironment = "production"
	approvedJWTAlgorithm  = "EdDSA"
)

// Config is the complete, immutable process configuration. Values are loaded
// only in the composition root; domain and application packages never read the
// environment directly.
type Config struct {
	App       AppConfig
	HTTP      HTTPConfig
	Postgres  PostgresConfig
	Auth      AuthConfig
	ProxyCORS ProxyCORSConfig
	Logging   LoggingConfig
	Telemetry TelemetryConfig
	Worker    WorkerConfig
	Storage   StorageConfig
}

type AppConfig struct {
	Environment      string `envconfig:"ENVIRONMENT" default:"development"`
	ServiceName      string `envconfig:"SERVICE_NAME" default:"pharmacycrm"`
	Version          string `default:"dev"`
	CommitSHA        string `envconfig:"COMMIT_SHA" default:"unknown"`
	Debug            bool   `default:"false"`
	MinSchemaVersion int    `envconfig:"MIN_SCHEMA_VERSION" default:"0"`
	MaxSchemaVersion int    `envconfig:"MAX_SCHEMA_VERSION" default:"0"`
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

type PostgresConfig struct {
	RuntimeDSN         string        `envconfig:"RUNTIME_DSN" required:"true"`
	MigrationDSN       string        `envconfig:"MIGRATION_DSN" required:"true"`
	MinConnections     int32         `envconfig:"MIN_CONNECTIONS" default:"1"`
	MaxConnections     int32         `envconfig:"MAX_CONNECTIONS" default:"10"`
	AcquireTimeout     time.Duration `envconfig:"ACQUIRE_TIMEOUT" default:"5s"`
	MaxConnectionLife  time.Duration `envconfig:"MAX_CONNECTION_LIFETIME" default:"30m"`
	MaxConnectionIdle  time.Duration `envconfig:"MAX_CONNECTION_IDLE_TIME" default:"5m"`
	HealthCheckPeriod  time.Duration `envconfig:"HEALTH_CHECK_PERIOD" default:"30s"`
	ConnectionCapacity int32         `envconfig:"CONNECTION_CAPACITY" default:"20"`
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
	Level    string `default:"info"`
	Format   string `default:"console"`
	FilePath string `envconfig:"FILE_PATH" default:"var/log/pharmacycrm/app.log"`
}

type TelemetryConfig struct {
	TracingEndpoint string        `envconfig:"TRACING_ENDPOINT"`
	MetricsAddress  string        `envconfig:"METRICS_ADDRESS" default:":9090"`
	ExportTimeout   time.Duration `envconfig:"EXPORT_TIMEOUT" default:"5s"`
}

type WorkerConfig struct {
	ProtocolVersion int           `envconfig:"PROTOCOL_VERSION" default:"1"`
	Concurrency     int           `default:"1"`
	PollInterval    time.Duration `envconfig:"POLL_INTERVAL" default:"1s"`
	LeaseDuration   time.Duration `envconfig:"LEASE_DURATION" default:"30s"`
	MaxClaim        int           `envconfig:"MAX_CLAIM" default:"100"`
}

type StorageConfig struct {
	ImportRoot     string        `envconfig:"IMPORT_ROOT" default:"var/imports"`
	MaxUploadBytes int64         `envconfig:"MAX_UPLOAD_BYTES" default:"10485760"`
	Retention      time.Duration `envconfig:"RETENTION" default:"720h"`
}

// CSV parses comma-separated environment values without silently retaining
// whitespace or empty entries.
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

// Load reads configuration from environment variables and validates it before
// a process opens listeners, files, or database connections.
func Load() (Config, error) {
	var cfg Config
	for _, group := range []struct {
		prefix string
		target any
	}{
		{"APP", &cfg.App},
		{"HTTP", &cfg.HTTP},
		{"POSTGRES", &cfg.Postgres},
		{"AUTH", &cfg.Auth},
		{"PROXY_CORS", &cfg.ProxyCORS},
		{"LOGGING", &cfg.Logging},
		{"TELEMETRY", &cfg.Telemetry},
		{"WORKER", &cfg.Worker},
		{"STORAGE", &cfg.Storage},
	} {
		if err := envconfig.Process(group.prefix, group.target); err != nil {
			// envconfig errors can include the raw value. Do not let a malformed
			// secret be copied to stderr or a central log collector.
			return Config{}, fmt.Errorf("load %s configuration: invalid or missing environment variable", strings.ToLower(group.prefix))
		}
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Validate checks local and cross-group invariants. It does not contact the
// database; schema compatibility with the deployed database is checked by the
// readiness path added in E1-FND-006/007.
func (c Config) Validate() error {
	if err := validateApp(c.App); err != nil {
		return err
	}
	if err := validateHTTP(c.HTTP, c.App.Environment); err != nil {
		return err
	}
	if err := validatePostgres(c.Postgres); err != nil {
		return err
	}
	if err := validateAuth(c.Auth, c.App.Environment, c.HTTP.TLSMode); err != nil {
		return err
	}
	if err := validateProxyCORS(c.ProxyCORS); err != nil {
		return err
	}
	if err := validateLogging(c.Logging, c.App.Environment); err != nil {
		return err
	}
	if err := validateTelemetry(c.Telemetry); err != nil {
		return err
	}
	if err := validateWorker(c.Worker, c.App.WorkerProtocol); err != nil {
		return err
	}
	return validateStorage(c.Storage)
}

func validateApp(c AppConfig) error {
	if c.Environment == "" || c.ServiceName == "" {
		return invalid("app environment and service name are required")
	}
	if c.Environment == productionEnvironment && c.Debug {
		return invalid("app debug mode is forbidden in production")
	}
	if c.MinSchemaVersion < 0 || c.MaxSchemaVersion < c.MinSchemaVersion {
		return invalid("app schema compatibility range is invalid")
	}
	if c.WorkerProtocol < 1 {
		return invalid("app worker protocol must be positive")
	}
	return nil
}

func validateHTTP(c HTTPConfig, environment string) error {
	if _, _, err := net.SplitHostPort(c.Address); err != nil {
		return invalid("http address is invalid")
	}
	if c.TLSMode != "disabled" && c.TLSMode != "terminated" && c.TLSMode != "direct" {
		return invalid("http tls mode is invalid")
	}
	if environment == productionEnvironment && c.TLSMode == "disabled" {
		return invalid("http tls must be enabled in production")
	}
	if c.ReadHeaderTimeout <= 0 || c.ReadTimeout <= 0 || c.WriteTimeout <= 0 || c.IdleTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return invalid("http timeouts must be positive")
	}
	if c.MaxHeaderBytes <= 0 || c.MaxBodyBytes <= 0 {
		return invalid("http limits must be positive")
	}
	return nil
}

func validatePostgres(c PostgresConfig) error {
	if err := validatePostgresDSN(c.RuntimeDSN); err != nil {
		return invalid("postgres runtime dsn is invalid")
	}
	if err := validatePostgresDSN(c.MigrationDSN); err != nil {
		return invalid("postgres migration dsn is invalid")
	}
	if c.MinConnections < 0 || c.MaxConnections < 1 || c.MinConnections > c.MaxConnections {
		return invalid("postgres pool bounds are invalid")
	}
	if c.ConnectionCapacity < c.MaxConnections {
		return invalid("postgres pool exceeds configured connection capacity")
	}
	if c.AcquireTimeout <= 0 || c.MaxConnectionLife <= 0 || c.MaxConnectionIdle <= 0 || c.HealthCheckPeriod <= 0 {
		return invalid("postgres timeouts must be positive")
	}
	return nil
}

func validatePostgresDSN(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" || u.User == nil || strings.TrimPrefix(u.Path, "/") == "" {
		return fmt.Errorf("invalid")
	}
	return nil
}

func validateAuth(c AuthConfig, environment, tlsMode string) error {
	if c.JWTIssuer == "" || c.JWTAudience == "" || c.JWTPrivateKey == "" || c.RefreshTokenPepper == "" {
		return invalid("auth required secret or identifier is missing")
	}
	if c.JWTAlgorithm != approvedJWTAlgorithm {
		return invalid("auth jwt algorithm must be EdDSA")
	}
	if c.CookieSameSite != "strict" {
		return invalid("auth cookie same-site policy must be strict")
	}
	if environment == productionEnvironment && (!c.CookieSecure || tlsMode == "disabled") {
		return invalid("auth cookie or tls settings are unsafe for production")
	}
	if c.AccessTokenTTL <= 0 || c.RefreshAbsoluteTTL <= 0 || c.RefreshIdleTTL <= 0 || c.ClockSkew < 0 || c.RefreshIdleTTL > c.RefreshAbsoluteTTL {
		return invalid("auth ttl settings are invalid")
	}
	return nil
}

func validateProxyCORS(c ProxyCORSConfig) error {
	if c.TrustForwardedHeaders && len(c.TrustedProxyCIDRs) == 0 {
		return invalid("trusted proxy allowlist is required when forwarded headers are trusted")
	}
	for _, cidr := range c.TrustedProxyCIDRs {
		if _, _, err := net.ParseCIDR(cidr); err != nil {
			return invalid("trusted proxy cidr is invalid")
		}
	}
	for _, origin := range c.AllowedOrigins {
		if origin == "*" && c.AllowCredentials {
			return invalid("cors wildcard origin cannot be used with credentials")
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Path != "" {
			return invalid("cors allowed origin is invalid")
		}
	}
	return nil
}

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
	if c.ProtocolVersion != expectedProtocol {
		return invalid("worker protocol is incompatible with application protocol")
	}
	if c.Concurrency < 1 || c.MaxClaim < 1 || c.PollInterval <= 0 || c.LeaseDuration <= 0 {
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

func invalid(message string) error { return fmt.Errorf("invalid configuration: %s", message) }
