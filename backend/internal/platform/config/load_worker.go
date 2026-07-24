package config

// LoadWorker loads only the configuration and secrets required by the worker.
func LoadWorker() (WorkerProcessConfig, error) {
	var cfg WorkerProcessConfig
	if err := process(
		struct {
			prefix string
			target any
		}{"APP", &cfg.App},
		struct {
			prefix string
			target any
		}{"POSTGRES", &cfg.WorkerPostgres},
		struct {
			prefix string
			target any
		}{"LOGGING", &cfg.Logging},
		struct {
			prefix string
			target any
		}{"TELEMETRY", &cfg.Telemetry},
		struct {
			prefix string
			target any
		}{"WORKER", &cfg.Worker},
		struct {
			prefix string
			target any
		}{"STORAGE", &cfg.Storage},
	); err != nil {
		return WorkerProcessConfig{}, err
	}
	if err := validateWorkerProcess(cfg); err != nil {
		return WorkerProcessConfig{}, err
	}
	return cfg, nil
}
