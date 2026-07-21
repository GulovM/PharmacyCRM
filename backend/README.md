# Backend runtime configuration

`internal/platform/config` is the only package that reads environment variables.
It uses `github.com/kelseyhightower/envconfig` and validates the complete process
configuration before any listener, log file, or database connection is opened.

Start local work by copying `.env.example` to `.env` and replacing the two auth
secret placeholders. A shell or Compose configuration must export the variables
before running a command; Go itself does not load `.env` files.

Configuration groups use these prefixes: `APP`, `HTTP`, `POSTGRES`, `AUTH`,
`PROXY_CORS`, `LOGGING`, `TELEMETRY`, `WORKER`, and `STORAGE`. The example file
is the versioned non-secret configuration matrix for local development.

The process rejects missing required values, invalid PostgreSQL DSNs, bad
timeouts or pool bounds, incompatible worker protocol declarations, unsafe
production TLS/cookie/debug settings, a wildcard credentialed CORS origin, and
invalid log/import paths. Error messages name a configuration category but never
echo secret values or DSNs.
