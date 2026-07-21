package httpserver

import (
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/config"
	"github.com/GulovM/PharmacyCRM/backend/internal/platform/logging"
	"github.com/GulovM/PharmacyCRM/backend/internal/shared/httpx"
	"github.com/gin-gonic/gin"
)

// MiddlewareChain is the single normative order for global HTTP middleware.
func MiddlewareChain(httpConfig config.HTTPConfig, proxyCORS config.ProxyCORSConfig, logger *logging.Logger, responder *httpx.Responder) []gin.HandlerFunc {
	return []gin.HandlerFunc{
		requestID(), recovery(logger, responder), accessLog(logger), tracingMetrics(),
		bodyLimit(httpConfig.MaxBodyBytes), corsAndSecurity(proxyCORS),
		authenticationParsing(), routePolicyAndRateLimit(),
	}
}

func tracingMetrics() gin.HandlerFunc          { return func(c *gin.Context) { c.Next() } }
func authenticationParsing() gin.HandlerFunc   { return func(c *gin.Context) { c.Next() } }
func routePolicyAndRateLimit() gin.HandlerFunc { return func(c *gin.Context) { c.Next() } }
