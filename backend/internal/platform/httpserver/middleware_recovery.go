package httpserver

import (
	"runtime/debug"

	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/httpx"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func recovery(logger *logging.Logger, responder *httpx.Responder) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if recovered := recover(); recovered != nil {
				logger.Error("http.panic", zap.String("request_id", currentRequestID(c)), zap.ByteString("stack", debug.Stack()))
				responder.Panic(c, "http.panic")
				c.Abort()
			}
		}()
		c.Next()
	}
}
