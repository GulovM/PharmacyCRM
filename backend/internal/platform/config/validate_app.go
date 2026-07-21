package config

func validateApp(c AppConfig) error {
	if c.Environment == "" || c.ServiceName == "" {
		return invalid("app environment and service name are required")
	}
	if c.Environment == productionEnvironment && c.Debug {
		return invalid("app debug mode is forbidden in production")
	}
	if c.MinSchemaVersion < 1 || c.MaxSchemaVersion < c.MinSchemaVersion || c.MinSchemaVersion > SupportedSchemaVersion || c.MaxSchemaVersion > SupportedSchemaVersion {
		return invalid("app schema compatibility range is invalid")
	}
	if c.WorkerProtocol != SupportedWorkerProtocol {
		return invalid("app worker protocol is unsupported")
	}
	return nil
}
