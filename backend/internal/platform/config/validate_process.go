package config

func validateAPI(c APIConfig) error {
	if err := validateApp(c.App); err != nil {
		return err
	}
	if err := validateHTTP(c.HTTP, c.App.Environment); err != nil {
		return err
	}
	if err := validateAPIPostgres(c.APIPostgres); err != nil {
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
	return validateStorage(c.Storage)
}

func validateWorkerProcess(c WorkerProcessConfig) error {
	if err := validateApp(c.App); err != nil {
		return err
	}
	if err := validateWorkerPostgres(c.WorkerPostgres); err != nil {
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

func validateMigration(c MigrationConfig) error {
	if err := validateApp(c.App); err != nil {
		return err
	}
	if err := validateMigrationPostgres(c.MigrationPostgres); err != nil {
		return err
	}
	return validateLogging(c.Logging, c.App.Environment)
}
