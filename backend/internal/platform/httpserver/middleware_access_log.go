package httpserver

import (
	"time"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func accessLog(logger *logging.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		logger.Info("http.request.completed", zap.String("request_id", currentRequestID(c)), zap.String("method", c.Request.Method), zap.String("route", c.FullPath()), zap.Int("status", c.Writer.Status()), zap.Int("response_bytes", c.Writer.Size()), zap.Duration("duration", time.Since(started)))
	}
}
