package config

// LoadMigration loads only the migration database credentials and common process metadata.
func LoadMigration() (MigrationConfig, error) {
	var cfg MigrationConfig
	if err := process(
		struct {
			prefix string
			target any
		}{"APP", &cfg.App},
		struct {
			prefix string
			target any
		}{"POSTGRES", &cfg.MigrationPostgres},
		struct {
			prefix string
			target any
		}{"LOGGING", &cfg.Logging},
	); err != nil {
		return MigrationConfig{}, err
	}
	if err := validateMigration(cfg); err != nil {
		return MigrationConfig{}, err
	}
	return cfg, nil
}
