package config

// LoadAPI loads only the configuration and secrets required by the API process.
func LoadAPI() (APIConfig, error) {
	var cfg APIConfig
	if err := process(
		struct {
			prefix string
			target any
		}{"APP", &cfg.App},
		struct {
			prefix string
			target any
		}{"HTTP", &cfg.HTTP},
		struct {
			prefix string
			target any
		}{"POSTGRES", &cfg.APIPostgres},
		struct {
			prefix string
			target any
		}{"AUTH", &cfg.Auth},
		struct {
			prefix string
			target any
		}{"PROXY_CORS", &cfg.ProxyCORS},
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
		return APIConfig{}, err
	}
	if err := validateAPI(cfg); err != nil {
		return APIConfig{}, err
	}
	return cfg, nil
}
