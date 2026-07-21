package bootstrap

import (
	"fmt"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"go.uber.org/zap"
)

func RunWorker() error {
	cfg, err := config.LoadWorker()
	if err != nil {
		return err
	}
	logger, err := logging.New(cfg.Logging, cfg.App)
	if err != nil {
		return fmt.Errorf("initialize logger")
	}
	defer logger.Close()
	logger.Info("process.initialized", zap.String("component", "worker"))
	return nil
}
